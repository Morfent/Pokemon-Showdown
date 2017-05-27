/**
 * Connections
 * Pokemon Showdown - http://pokemonshowdown.com/
 *
 * Abstraction layer for multi-process SockJS connections.
 *
 * This file handles all the communications between the users' browsers and
 * the main process.
 *
 * @license MIT license
 */

'use strict';

const cluster = require('cluster');
const fs = require('fs');
const sockjs = require('sockjs');
const StaticServer = require('node-static').Server;

if (!global.Config) global.Config = require('./config/config');
if (!global.Dnsbl) global.Dnsbl = require('./dnsbl');
if (!global.Monitor) global.Monitor = require('./monitor');

// IPC command tokens
const EVAL = '$';
const SOCKET_CONNECT = '*';
const SOCKET_DISCONNECT = '!';
const SOCKET_RECEIVE = '<';
const SOCKET_SEND = '>';
const CHANNEL_ADD = '+';
const CHANNEL_REMOVE = '-';
const CHANNEL_BROADCAST = '#';
const SUBCHANNEL_MOVE = '.';
const SUBCHANNEL_BROADCAST = ':';

// Subchannel IDs
const DEFAULT_SUBCHANNEL = '0';
const P1_SUBCHANNEL = '1';
const P2_SUBCHANNEL = '2';

// Regex for splitting subchannel broadcasts between subchannels.
const SUBCHANNEL_MESSAGE_REGEX = /\n\/split(\n[^\n]*)(\n[^\n]*)(\n[^\n]*)\n[^\n]*/g;

/*
 * FIXME: this also belongs in dev-tools/globals.ts...
 * @typedef {Map<string, string>} Channel
 * @typedef {Map<string, Socket>} Sockets
 * @typedef {Map<string, Channel>} Channels
 */

/**
 * @class Multiplexer
 * @description Manages the worker's state for sockets, channels, and
 * subchannels. This is responsible for parsing all outgoing and incoming
 * messages.
 */
class Multiplexer {
	/**
	 * @param {number} socketCounter
	 * @param {Sockets} sockets
	 * @param {Channels} channels
	 * @param {NodeJS.Timer | null} cleanupInterval
	 */
	constructor() {
		this.socketCounter = 0;
		this.sockets = new Map();
		this.channels = new Map();
		this.cleanupInterval = setInterval(() => this.sweepClosedSockets(), 10 * 60 * 1000);
	}

	/**
	 * @description Mitigates a potential bug in SockJS or Faye-Websocket where
	 * sockets fail to emit a 'close' event after having disconnected.
	 * @returns {void}
	 */
	sweepClosedSockets() {
		this.sockets.forEach(socket => {
			if (socket.protocol === 'xhr-streaming' &&
					socket._session &&
					socket._session.recv) {
				socket._session.recv.didClose();
			}

			// A ghost connection's `_session.to_tref._idlePrev` (and `_idleNext`) property is `null` while
			// it is an object for normal users. Under normal circumstances, those properties should only be
			// `null` when the timeout has already been called, but somehow it's not happening for some connections.
			// Simply calling `_session.timeout_cb` (the function bound to the aformentioned timeout) manually
			// on those connections kills those connections. For a bit of background, this timeout is the timeout
			// that sockjs sets to wait for users to reconnect within that time to continue their session.
			if (socket._session &&
					socket._session.to_tref &&
					!socket._session.to_tref._idlePrev) {
				socket._session.timeout_cb();
			}
		});

		// Don't bother deleting the sockets from our map; their close event
		// handler will deal with it.
	}

	/**
	 * @description Sends an IPC message to the parent process.
	 * @param {string} token
	 * @param {string[]} params
	 * @returns {void}
	 */
	sendUpstream(token, ...params) {
		let message = `${token}${params.join('\n')}`;
		process.send(message);
	}

	/**
	 * @description Parses the params in a downstream message sent as a
	 * command.
	 * @param {string} params
	 * @param {number} count
	 * @returns {string[]}
	 */
	parseParams(params, count) {
		let i = 0;
		let idx = 0;
		let ret = [];
		while (i++ < count) {
			let newIdx = params.indexOf('\n', idx);
			if (newIdx < 0) {
				// No remaining newlines; just use the rest of the string as
				// the last parametre.
				ret.push(params.slice(idx));
				break;
			}

			let param = params.slice(idx, newIdx);
			if (i === count) {
				// We reached the number of parametres needed, but there is
				// still some remaining string left. Glue it to the last one.
				param += `\n${params.slice(newIdx + 1)}`;
			} else {
				idx = newIdx + 1;
			}

			ret.push(param);
		}

		return ret;
	}

	/**
	 * @description Parses downstream messages.
	 * @param {string} data
	 * @returns {boolean}
	 */
	receiveDownstream(data) {
		let command = data.charAt(0);
		let params = data.substr(1);
		let socketid;
		let channelid;
		let subchannelid;
		let message;
		switch (command) {
		case EVAL:
			return this.onEval(params);
		case SOCKET_DISCONNECT:
			return this.onSocketDisconnect(params);
		case SOCKET_SEND:
			[socketid, message] = this.parseParams(params, 2);
			return this.onSocketSend(socketid, message);
		case CHANNEL_ADD:
			[channelid, socketid] = this.parseParams(params, 2);
			return this.onChannelAdd(channelid, socketid);
		case CHANNEL_REMOVE:
			[channelid, socketid] = this.parseParams(params, 2);
			return this.onChannelRemove(channelid, socketid);
		case CHANNEL_BROADCAST:
			[channelid, message] = this.parseParams(params, 2);
			return this.onChannelBroadcast(channelid, message);
		case SUBCHANNEL_MOVE:
			[channelid, subchannelid, socketid] = this.parseParams(params, 3);
			return this.onSubchannelMove(channelid, subchannelid, socketid);
		case SUBCHANNEL_BROADCAST:
			[channelid, message] = this.parseParams(params, 2);
			return this.onSubchannelBroadcast(channelid, message);
		default:
			Monitor.debug(`Sockets worker IPC error: unknown command type in downstream message: ${data}`);
			return false;
		}
	}

	/**
	 * @description Safely tries to destroy a socket's connection.
	 * @param {any} socket
	 * @returns {void}
	 */
	tryDestroySocket(socket) {
		try {
			socket.end();
			socket.destroy();
		} catch (e) {}
	}

	/**
	 * @description Eval handler for downstream messages.
	 * @param {string} expr
	 * @returns {boolean}
	 */
	onEval(expr) {
		try {
			eval(expr);
			return true;
		} catch (e) {}
		return false;
	}

	/**
	 * @description Sockets.socketConnect message handler.
	 * @param {any} socket
	 * @returns {boolean}
	 */
	onSocketConnect(socket) {
		if (!socket) return false;
		if (!socket.remoteAddress) {
			this.tryDestroySocket(socket);
			return false;
		}

		let socketid = '' + this.socketCounter++;
		let ip = socket.remoteAddress;
		let ips = socket.headers['x-forwarded-for'] || '';
		this.sockets.set(socketid, socket);
		this.sendUpstream(SOCKET_CONNECT, socketid, ip, ips, socket.protocol);

		socket.on('data', /** @param {string} message */ message => {
			this.onSocketReceive(socketid, message);
		});

		socket.on('close', () => {
			this.sendUpstream(SOCKET_DISCONNECT, socketid);
			this.sockets.delete(socketid);
			this.channels.forEach((channel, channelid) => {
				if (!channel.has(socketid)) return;
				channel.delete(socketid);
				if (!channel.size) this.channels.delete(channelid);
			});
		});

		return true;
	}

	/**
	 * @description Sockets.socketDisconnect message handler.
	 * @param {string} socketid
	 * @returns {boolean}
	 */
	onSocketDisconnect(socketid) {
		let socket = this.sockets.get(socketid);
		if (!socket) return false;

		this.tryDestroySocket(socket);
		return true;
	}

	/**
	 * @description Sockets.socketSend message handler.
	 * @param {string} socketid
	 * @param {string} message
	 * @returns {boolean}
	 */
	onSocketSend(socketid, message) {
		let socket = this.sockets.get(socketid);
		if (!socket) return false;

		socket.write(message);
		return true;
	}

	/**
	 * @description onmessage event handler for sockets. Passes the message
	 * upstream.
	 * @param {string} socketid
	 * @param {string} message
	 * @returns {boolean}
	 */
	onSocketReceive(socketid, message) {
		// Drop empty messages (DDOS?).
		if (!message) return false;

		// Drop >100KB messages.
		if (message.length > (1000 * 1024)) {
			console.log(`Dropping client message ${message.length / 1024} KB...`);
			console.log(message.slice(0, 160));
			return false;
		}

		// Drop legacy JSON messages.
		if ((typeof message !== 'string') || message.startsWith('{')) return false;

		// Drop invalid messages (again, DDOS?).
		if (!message.includes('|') || message.endsWith('|')) return false;

		this.sendUpstream(SOCKET_RECEIVE, socketid, message);
		return true;
	}

	/**
	 * @description Sockets.channelAdd message handler.
	 * @param {string} channelid
	 * @param {string} socketid
	 * @returns {boolean}
	 */
	onChannelAdd(channelid, socketid) {
		if (!this.sockets.has(socketid)) return false;

		if (this.channels.has(channelid)) {
			let channel = this.channels.get(channelid);
			if (channel.has(socketid)) return false;
			channel.set(socketid, DEFAULT_SUBCHANNEL);
		} else {
			let channel = new Map();
			channel.set(socketid, DEFAULT_SUBCHANNEL);
			this.channels.set(channelid, channel);
		}

		return true;
	}

	/**
	 * @description Sockets.channelRemove message handler.
	 * @param {string} channelid
	 * @param {string} socketid
	 * @returns {boolean}
	 */
	onChannelRemove(channelid, socketid) {
		let channel = this.channels.get(channelid);
		if (!channel) return false;

		channel.delete(socketid);
		if (!channel.size) this.channels.delete(channelid);

		return true;
	}

	/**
	 * @description Sockets.channelSend and Sockets.channelBroadcast message
	 * handler.
	 * @param {string} channelid
	 * @param {string} message
	 * @returns {boolean}
	 */
	onChannelBroadcast(channelid, message) {
		let channel = this.channels.get(channelid);
		if (!channel) return false;

		channel.forEach(
			/** @param {string} subchannelid */
			/** @param {string} socketid */
			(subchannelid, socketid) => {
				let socket = this.sockets.get(socketid);
				socket.write(message);
			}
		);

		return true;
	}

	/**
	 * @description Sockets.subchannelMove message handler.
	 * @param {string} channelid
	 * @param {string} subchannelid
	 * @param {string} socketid
	 * @returns {boolean}
	 */
	onSubchannelMove(channelid, subchannelid, socketid) {
		if (!this.sockets.has(socketid)) return false;

		if (this.channels.has(channelid)) {
			let channel = new Map([[socketid, subchannelid]]);
			this.channels.set(channelid, channel);
		} else {
			let channel = this.channels.get(channelid);
			channel.set(socketid, subchannelid);
		}

		return true;
	}

	/**
	 * @description Sockets.subchannelBroadcast message handler.
	 * @param {string} channelid
	 * @param {string} message
	 * @returns {boolean}
	 */
	onSubchannelBroadcast(channelid, message) {
		let channel = this.channels.get(channelid);
		if (!channel) return false;

		/** @type {RegExpExecArray | null} */
		let matches = SUBCHANNEL_MESSAGE_REGEX.exec(message);
		if (!matches) return false;

		let [match, msg1, msg2, msg3] = matches.splice(0);
		channel.forEach(
			/** @param {string} subchannelid */
			/** @param {string} socketid */
			(subchannelid, socketid) => {
				let socket = this.sockets.get(socketid);
				if (!socket) return;

				switch (subchannelid) {
				case DEFAULT_SUBCHANNEL:
					socket.write(msg1);
					break;
				case P1_SUBCHANNEL:
					socket.write(msg2);
					break;
				case P2_SUBCHANNEL:
					socket.write(msg3);
					break;
				default:
					Monitor.debug(`Sockets worker ${cluster.worker.id} received a message targeted at an unknown subchannel: ${match}`);
					break;
				}
			}
		);

		return true;
	}
}

if (cluster.isWorker) {
	if (process.env.PSPORT) Config.port = +process.env.PSPORT;
	if (process.env.PSBINDADDR) Config.bindaddress = process.env.PSBINDADDR;
	if (+process.env.PSNOSSL) Config.ssl = null;
	if (Config.crashguard) {
		// Graceful crash.
		process.on('uncaughtException', /** @param {Error} err */ err => {
			require('./crashlogger')(err, `Socket process ${cluster.worker.id} (${process.pid})`, true);
		});
	}

	// This is optional. If ofe is installed, it will take a heapdump if the
	// process runs out of memory.
	try {
		require('ofe').call();
	} catch (e) {}

	let app = require('http').createServer();
	let appssl = null;
	if (Config.ssl) {
		let key;
		let cert;
		try {
			key = fs.readFileSync(Config.ssl.options.key);
			cert = fs.readFileSync(Config.ssl.options.cert);
			Config.ssl.options.key = key;
			Config.ssl.options.cert = cert;
		} catch (e) {
			console.error('The configured SSL key and cert must be the filenames of their according files now in order for Go processes to be able to host over HTTPS.');
		} finally {
			appssl = require('https').createServer(Config.ssl.options);
		}
	}

	// Launch the static server.
	try {
		let cssserver = new StaticServer('./config');
		let avatarserver = new StaticServer('./config/avatars');
		let staticserver = new StaticServer('./static');
		/** @param {any} request */
		/** @param {any} response */
		let staticRequestHandler = (request, response) => {
			// console.log("static rq: " + request.socket.remoteAddress + ":" + request.socket.remotePort + " -> " + request.socket.localAddress + ":" + request.socket.localPort + " - " + request.method + " " + request.url + " " + request.httpVersion + " - " + request.rawHeaders.join('|'));
			request.resume();
			request.addListener('end', () => {
				if (Config.customhttpresponse &&
						Config.customhttpresponse(request, response)) {
					return;
				}
				let server;
				if (request.url === '/custom.css') {
					server = cssserver;
				} else if (request.url.substr(0, 9) === '/avatars/') {
					request.url = request.url.substr(8);
					server = avatarserver;
				} else {
					if (/^\/([A-Za-z0-9][A-Za-z0-9-]*)\/?$/.test(request.url)) {
						request.url = '/';
					}
					server = staticserver;
				}

				server.serve(request, response,
					/** @param {any} e */
					/** @param {any} res */
					(e, res) => {
						if (e && (e.status === 404)) {
							staticserver.serveFile('404.html', 404, {}, request, response);
						}
					}
				);
			});
		};
		app.on('request', staticRequestHandler);
		if (appssl) appssl.on('request', staticRequestHandler);
	} catch (e) {}

	// Launch the SockJS server.
	/** @type {any} */
	const server = sockjs.createServer({
		sockjs_url: '//play.pokemonshowdown.com/js/lib/sockjs-1.1.1-nwjsfix.min.js',
		/** @param {string} severity */
		/** @param {string} message */
		log: (severity, message) => {
			if (severity === 'error') Monitor.debug(`Sockets worker SockJS error: ${message}`);
		},
		prefix: '/showdown',
	});

	// Instantiate SockJS' multiplexer. This takes messages received downstream
	// from the parent process and distributes them across the sockets they are
	// targeting, as well as handling user disconnects and passing user
	// messages upstream.
	const multiplexer = new Multiplexer();

	process.on('message', /** @param {string} data */ data => {
		// console.log('worker received: ' + data);
		let ret = multiplexer.receiveDownstream(data);
		if (!ret) {
			Monitor.debug(`Sockets worker IPC error: failed to parse downstream message: ${data}`);
		}
	});

	process.on('disconnect', () => {
		process.exit(0);
	});

	server.on('connection', /** @param {any} socket */ socket => {
		multiplexer.onSocketConnect(socket);
	});

	server.installHandlers(app, {});
	if (!Config.bindaddress) Config.bindaddress = '0.0.0.0';
	app.listen(Config.port, Config.bindaddress);
	console.log(`Worker ${cluster.worker.id} now listening on ${Config.bindaddress}:${Config.port}`);

	if (appssl) {
		server.installHandlers(appssl, {});
		appssl.listen(Config.ssl.port, Config.bindaddress);
		console.log(`Worker ${cluster.worker.id} now listening for SSL on port ${Config.ssl.port}`);
	}

	console.log(`Test your server at http://${Config.bindaddress === '0.0.0.0' ? 'localhost' : Config.bindaddress}:${Config.port}`);

	require('./repl').start('sockets-', `${cluster.worker.id}-${process.pid}`, /** @param {string} cmd */ cmd => eval(cmd));
}

module.exports = {
	TOKENS: {
		EVAL,
		SOCKET_CONNECT,
		SOCKET_DISCONNECT,
		SOCKET_SEND,
		SOCKET_RECEIVE,
		CHANNEL_ADD,
		CHANNEL_REMOVE,
		CHANNEL_BROADCAST,
		SUBCHANNEL_MOVE,
		SUBCHANNEL_BROADCAST,
	},

	SUBCHANNEL_IDS: {
		DEFAULT_SUBCHANNEL,
		P1_SUBCHANNEL,
		P2_SUBCHANNEL,
	},

	Multiplexer,
};

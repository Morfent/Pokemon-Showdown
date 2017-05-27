/**
 * Connections
 * Pokemon Showdown - http://pokemonshowdown.com/
 *
 * Abstraction layer for multi-process SockJS connections.
 *
 * This file handles all the communications between the networking processes
 * and users.js.
 *
 * @license MIT license
 */

'use strict';

const cluster = require('cluster');
const EventEmitter = require('events');

if (!global.Config) global.Config = require('./config/config');

if (cluster.isMaster) {
	cluster.setupMaster({
		exec: require('path').resolve(__dirname, 'sockets-workers'),
	});
}

// FIXME: this needs to go to dev-tools/globals.ts, but @types/node and
// @types/sockjs-node must be installed before that can be done.
/** @typedef {any} NodeJSWorker */
cluster.Worker; // eslint-disable-line no-unused-expressions
/** @typedef {any} Socket */
require('net').Socket; // eslint-disable-line no-unused-expressions
/** @typedef {NodeJSWorker | GoWorker} Worker */

/**
 * @description IPC delimiter byte. This byte must stringify as either a
 * hexadecimal or a Unicode escape code when stringified as JSON to prevent
 * messages from being able to contain the byte itself.
 * @type {string}
 */
const DELIM = '\u0003';

/**
 * @class WorkerWrapper
 * @implements NodeJS.Cluster.Worker
 * @description A wrapper class for native Node.js Worker and GoWorker
 * instances. This parses upstream messages from both types of workers and
 * cleans up when workers crash or get killed before respawning them. In other
 * words this listens for events emitted from both types of workers and handles
 * them accordingly.
 */
class WorkerWrapper {
	/**
	 * @param {Worker} worker
	 * @prop {number} id;
	 * @prop {Worker} worker
	 * @prop {NodeJS.ChildProcess | null} process
	 * @prop {boolean | undefined} exitedAfterDisconnect
	 * @prop {(ip: string) => boolean} isTrustedProxyIp
	 */
	constructor(worker) {
		this.id = worker.id;
		this.worker = worker;
		this.process = worker.process;
		this.exitedAfterDisconnect = worker.exitedAfterDisconnect;
		this.isTrustedProxyIp = Dnsbl.checker(Config.proxyip);

		worker.on('message',
			/** @param {string} data */
			data => this.onMessage(data)
		);
		worker.on('error', () => {
			// Ignore. Neither kind of child process ever prints to stderr
			// without throwing/panicking and emitting the diconnect/exit
			// events.
		});
		worker.once('disconnect',
			/** @param {string} data */
			data => {
				if (this.exitedAfterDisconnect !== undefined) return;
				this.exitedAfterDisconnect = true;
				process.nextTick(() => this.onDisconnect(data));
			}
		);
		worker.once('exit',
			/** @param {number} code */
			/** @param {string} signal */
			(code, signal) => {
				if (this.exitedAfterDisconnect !== undefined) return;
				this.exitedAfterDisconnect = false;
				process.nextTick(() => this.onExit(code, signal));
			}
		);
	}

	/**
	 * @description Worker#suicide getter wrapper
	 * @returns {boolean | undefined}
	 */
	get suicide() {
		return this.exitedAfterDisconnect;
	}

	/**
	 * @description Worker#suicide setter wrapper
	 * @param {boolean} val
	 * @returns {void}
	 */
	set suicide(val) {
		this.exitedAfterDisconnect = val;
		this.worker.exitedAfterDisconnect = val;
	}

	/**
	 * @description Worker#kill wrapper
	 * @param {string} signal
	 * @returns {void}
	 */
	kill(signal = 'SIGTERM') {
		return this.worker.kill(signal);
	}

	/**
	 * @description Worker#destroy wrapper
	 * @param {string} signal
	 * @returns {void}
	 */
	destroy(signal) {
		return this.kill(signal);
	}

	/**
	 * @description Worker#send wrapper
	 * @param {string} message
	 * @param {any?} sendHandle
	 * @returns {void}
	 */
	send(message, sendHandle) {
		return this.worker.send(message, sendHandle);
	}

	/**
	 * @description Worker#isConnected wrapper
	 * @returns {boolean}
	 */
	isConnected() {
		return this.worker.isConnected();
	}

	/**
	 * @description Worker#isDead wrapper
	 * @returns {boolean}
	 */
	isDead() {
		return this.worker.isDead();
	}

	/**
	 * @description Splits the parametres of incoming IPC messages from the
	 * worker's child process for the 'message' event handler.
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
	 * @description Picks the last known IP for a new connection that's found
	 * to not be trusted as per the DNSBL.
	 * @param {string} ip
	 * @param {string} header
	 * @returns {string}
	 */
	pluckUntrustedIp(ip, header = '') {
		if (!this.isTrustedProxyIp(ip)) {
			return ip;
		}

		let ips = header.split(',');
		let ret;
		for (let i = ips.length; i--;) {
			ret = ips[i].trim();
			if (ret && !this.isTrustedProxyIp(ret)) {
				return ret;
			}
		}

		return ip;
	}

	/**
	 * @description 'message' event handler for the worker. Parses which type
	 * of command the incoming IPC message uses, then parses its parametres and
	 * calls the appropriate Users method.
	 * @param {string} data
	 * @returns {boolean}
	 */
	onMessage(data) {
		// console.log('master received: ' + data);
		let command = data.charAt(0);
		let params = data.substr(1);
		switch (command) {
		case '*':
			let [socketid, ip, header, protocol] = this.parseParams(params, 4);
			ip = this.pluckUntrustedIp(ip, header);
			Users.socketConnect(this.worker, this.id, socketid, ip, protocol);
			break;
		case '!':
			Users.socketDisconnect(this.worker, this.id, params);
			break;
		case '<':
			Users.socketReceive(this.worker, this.id, ...this.parseParams(params, 2));
			break;
		default:
			Monitor.debug(`Sockets: master received unknown IPC command type: ${data}`);
			break;
		}
	}

	/**
	 * @description 'disconnect' event handler for the worker. Cleans up any
	 * remaining users whose sockets were contained by the worker's child
	 * process, then attempts to respawn it..
	 * @param {string} data
	 * @returns {void}
	 */
	onDisconnect(data) {
		require('./crashlogger')(new Error(`Worker ${this.id} abruptly died with the following stack trace: ${data}`), 'The main process');
		console.error(`${Users.socketDisconnectAll(this.worker)} connections were lost.`);
		spawnWorker();
	}

	/**
	 * @description 'exit' event handler for the worker. Only used by GoWorker
	 * instances, since the 'disconnect' event is only available for Node.js
	 * workers.
	 * @param {number} code
	 * @param {string?} signal
	 * @returns {void}
	 */
	onExit(code, signal) {
		require('./crashlogger')(new Error(`Worker ${this.id} abruptly died with code ${code} and signal ${signal}`), 'The main process');
		console.error(`${Users.socketDisconnectAll(this.worker)} connections were lost.`);
		spawnWorker();
	}
}

/**
 * @class GoWorker
 * @extends NodeJS.EventEmitter
 * @description A mock Worker class for Go child processes. Similarly to
 * Node.js workers, it uses a TCP net server to perform IPC. After launching
 * the server, it will spawn the Go child process and wait for it to make a
 * connection to the worker's server before performing IPC with it.
 */
class GoWorker extends EventEmitter {
	/**
	 * @param {number} id
	 * @prop {number} id
	 * @prop {NodeJS.ChildProcess | null} process
	 * @prop {boolean | undefined} exitedAfterDisconnect
	 * @prop {NodeJS.net.Server | null} server
	 * @prop {NodeJS.net.NodeSocket | null} connection
	 * @prop {string[]} buffer
	 */
	constructor(id) {
		super();

		this.id = id;
		this.process = null;
		this.exitedAfterDisconnect = undefined;

		this.server = null;
		this.connection = null;
		/** @type {string[]} */
		this.buffer = [];

		process.nextTick(() => this.spawnServer());
	}

	/**
	 * @description Worker#kill mock
	 * @param {string} signal
	 * @returns {void}
	 */
	kill(signal = 'SIGTERM') {
		if (this.isConnected()) this.connection.end();
		if (!this.isDead() && this.process) this.process.kill(signal);
		if (this.server) this.server.close();
		this.exitedAfterDisconnect = false;
	}

	/**
	 * @description Worker#destroy mock
	 * @param {string=} signal
	 * @returns {void}
	 */
	destroy(signal) {
		return this.kill(signal);
	}

	/**
	 * @description Worker#send mock
	 * @param {string} message
	 * @param {any?} sendHandle
	 * @returns {void}
	 */
	send(message, sendHandle) { // eslint-disable-line no-unused-vars
		if (!this.isConnected()) {
			this.buffer.push(message);
			return;
		}

		if (this.buffer.length) {
			this.buffer.splice(0).forEach(msg => {
				this.connection.write(JSON.stringify(msg) + DELIM);
			});
		}

		return this.connection.write(JSON.stringify(message) + DELIM);
	}

	/**
	 * @description Worker#isConnected mock
	 * @returns {boolean}
	 */
	isConnected() {
		return this.connection && !this.connection.destroyed;
	}

	/**
	 * @description Worker#isDead mock
	 * @returns {boolean}
	 */
	isDead() {
		return !this.process || this.connection.exitCode !== null || this.connection.statusCode !== null;
	}

	/**
	 * @description Spawns the TCP server through which IPC with the child
	 * process is handled.
	 * @returns {boolean}
	 */
	spawnServer() {
		if (!this.isDead()) return false;

		this.server = require('net').createServer();
		this.server.on('error', console.error);
		this.server.once('listening', () => {
			// Spawn the child process after the TCP server has finished
			// launching to allow it to connect to it for IPC.
			process.nextTick(() => this.spawnChild());
		});
		// When the child process finally connects to the TCP server we can
		// begin communicating with it using a random port.
		this.server.listen(() => {
			if (!this.server) return;
			this.server.once('connection', connection => {
				process.nextTick(() => this.bootstrapChild(connection));
			});
		});
	}

	/**
	 * @description Spawns the Go child process. Once the process has started,
	 * it will make a connection to the worker's TCP server.
	 * @returns {void}
	 */
	spawnChild() {
		if (!this.server) return this.spawnServer();
		this.process = require('child_process').spawn(
			`${process.env.GOPATH}/bin/sockets`, [], {
				env: {
					GOPATH: process.env.GOPATH || '',
					GOROOT: process.env.GOROOT || '',
					PS_IPC_PORT: `:${this.server.address().port}`,
					PS_CONFIG: JSON.stringify({
						workers: Config.workers || 1,
						port: `:${Config.port || 8000}`,
						bindAddress: Config.bindaddress || '0.0.0.0',
						ssl: Config.ssl || null,
					}),
				},
				stdio: ['inherit', 'inherit', 'pipe'],
				shell: true,
			}
		);

		this.process.once('exit', (code, signal) => {
			process.nextTick(() => this.emit('exit', code, signal));
		});

		this.process.stderr.setEncoding('utf8');
		this.process.stderr.once('data', data => {
			process.nextTick(() => this.emit('error', data));
		});
	}

	/**
	 * @description 'connection' event handler for the TCP server. Begins
	 * the parsing of incoming IPC messages.
	 * @param {Socket} connection
	 * @returns {void}
	 */
	bootstrapChild(connection) {
		this.connection = connection;
		this.connection.setEncoding('utf8');
		this.connection.on('data',
			/** @param {string} data */
			data => {
				let messages = data.slice(0, -1).split(DELIM);
				messages.forEach(message => {
					this.emit('message', JSON.parse(message));
				});
			}
		);

		// Leave the error handling to the process, not the connection.
		this.connection.on('error', () => {});
	}
}

/**
 * @description Map of worker IDs to worker processes.
 * @type {Map<number, Worker>}
 */
const workers = new Map();

/**
 * @description Worker ID counter used for Go workers.
 * @type {number}
 */
let nextWorkerid = 0;

/**
 * @description Spawns a new worker process.
 * @returns {Worker}
 */
function spawnWorker() {
	let worker;
	if (Config.golang) {
		worker = new GoWorker(nextWorkerid);
	} else {
		worker = cluster.fork({
			PSPORT: Config.port,
			PSBINDADDR: Config.bindaddress || '0.0.0.0',
			PSNOSSL: Config.ssl ? 0 : 1,
		});
	}

	let wrapper = new WorkerWrapper(worker);
	workers.set(wrapper.id, wrapper);
	nextWorkerid++;
	return wrapper;
}

/**
 * @description Initializes the configured number of worker processes.
 * @param {any} port
 * @param {any} bindAddress
 * @param {any} workerCount
 * @returns {void}
 */
function listen(port, bindAddress, workerCount) {
	if (port !== undefined && !isNaN(port)) {
		Config.port = port;
		Config.ssl = null;
	} else {
		port = Config.port;
		// Autoconfigure the app when running in cloud hosting environments:
		try {
			const cloudenv = require('cloud-env');
			bindAddress = cloudenv.get('IP', bindAddress);
			port = cloudenv.get('PORT', port);
		} catch (e) {}
	}
	if (bindAddress !== undefined) {
		Config.bindaddress = bindAddress;
	}

	// Go only uses one child process since it does not share FD handles for
	// serving like Node.js workers do. Workers are instead used to limit the
	// number of concurrent requests that can be handled at once in the child
	// process.
	if (Config.golang) {
		spawnWorker();
		return;
	}

	if (workerCount === undefined) {
		workerCount = (Config.workers !== undefined ? Config.workers : 1);
	}
	for (let i = 0; i < workerCount; i++) {
		spawnWorker();
	}
}

/**
 * @description Kills a worker process using the given worker object.
 * @param {Worker} worker
 * @returns {number}
 */
function killWorker(worker) {
	let count = Users.socketDisconnectAll(worker);
	try {
		worker.kill();
	} catch (e) {}
	workers.delete(worker.id);
	return count;
}

/**
 * @description Kills a worker process using the given worker PID.
 * @param {number} pid
 * @returns {number | false}
 */
function killPid(pid) {
	workers.forEach(worker => {
		if (pid === worker.process.pid) {
			return this.killWorker(worker);
		}
	});
	return false;
}

/**
 * @description Sends a message to a socket in a given worker by ID.
 * @param {Worker} worker
 * @param {string} socketid
 * @param {string} message
 * @returns {void}
 */
function socketSend(worker, socketid, message) {
	worker.send(`>${socketid}\n${message}`);
}

/**
 * @description Forcefully disconnects a socket in a given worker by ID.
 * @param {Worker} worker
 * @param {string} socketid
 * @returns {void}
 */
function socketDisconnect(worker, socketid) {
	worker.send(`!${socketid}`);
}

/**
 * @description Broadcasts a message to all sockets in a given channel across
 * all workers.
 * @param {string} channelid
 * @param {string} message
 * @returns {void}
 */
function channelBroadcast(channelid, message) {
	workers.forEach(worker => {
		worker.send(`#${channelid}\n${message}`);
	});
}

/**
 * @description Broadcasts a message to all sockets in a given channel and a
 * given worker.
 * @param {Worker} worker
 * @param {string} channelid
 * @param {string} message
 * @returns {void}
 */
function channelSend(worker, channelid, message) {
	worker.send(`#${channelid}\n${message}`);
}

/**
 * @description Adds a socket to a given channel in a given worker by ID.
 * @param {Worker} worker
 * @param {string} channelid
 * @param {string} socketid
 * @returns {void}
 */
function channelAdd(worker, channelid, socketid) {
	worker.send(`+${channelid}\n${socketid}`);
}

/**
 * @description Removes a socket from a given channel in a given worker by ID.
 * @param {Worker} worker
 * @param {string} channelid
 * @param {string} socketid
 * @returns {void}
 */
function channelRemove(worker, channelid, socketid) {
	worker.send(`-${channelid}\n${socketid}`);
}

/**
 * @description Broadcasts a message to be demuxed into three separate messages
 * across three subchannels in a given channel across all workers.
 * @param {string} channelid
 * @param {string} message
 * @returns {void}
 */
function subchannelBroadcast(channelid, message) {
	workers.forEach(worker => {
		worker.send(`:${channelid}\n${message}`);
	});
}

/**
 * @description Moves a given socket to a different subchannel in a channel by
 * ID in the given worker.
 * @param {Worker} worker
 * @param {string} channelid
 * @param {string} subchannelid
 * @param {string} socketid
 */
function subchannelMove(worker, channelid, subchannelid, socketid) {
	worker.send(`.${channelid}\n${subchannelid}\n${socketid}`);
}

module.exports = {
	WorkerWrapper,
	GoWorker,

	workers,
	spawnWorker,
	listen,
	killWorker,
	killPid,

	socketSend,
	socketDisconnect,
	channelBroadcast,
	channelSend,
	channelAdd,
	channelRemove,
	subchannelBroadcast,
	subchannelMove,
};

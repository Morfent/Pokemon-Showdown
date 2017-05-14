'use strict';

const assert = require('assert');

const {createSocket} = require('../../dev-tools/sockets');

describe('Sockets workers', function () {
	before(function () {
		this.mux = new (require('../../sockets-workers')).Multiplexer();
		clearInterval(this.mux.cleanupInterval);
		this.mux.cleanupInterval = null;
		this.mux.sendUpstream = () => {};
	});

	beforeEach(function () {
		this.socket = createSocket();
	});

	afterEach(function () {
		this.mux.tryDestroySocket(this.socket);
		this.mux.channels.clear();
	});

	after(function () {
		this.socket = null;
		this.mux.sockets.clear();
		this.mux = null;
	});

	it('should parse more than two params', function () {
		let params = '1\n1\n0\n';
		let ret = this.mux.parseParams(params, 4);
		assert.deepStrictEqual(ret, ['1', '1', '0', '']);
	});

	it('should parse params with multiple newlines', function () {
		let params = '0\n|1\n|2';
		let ret = this.mux.parseParams(params, 2);
		assert.deepStrictEqual(ret, ['0', '|1\n|2']);
	});

	it('should add sockets on connect', function () {
		let res = this.mux.onSocketConnect(this.socket);
		assert.ok(res);
	});

	it('should remove sockets on disconnect', function () {
		this.mux.onSocketConnect(this.socket);
		let res = this.mux.onSocketDisconnect('0', this.socket);
		assert.ok(res);
	});

	it('should add sockets to channels', function () {
		this.mux.onSocketConnect(this.socket);
		let res = this.mux.onChannelAdd('global', '0');
		assert.ok(res);
		res = this.mux.onChannelAdd('global', '0');
		assert.ok(!res);
		this.mux.channels.set('lobby', new Map());
		res = this.mux.onChannelAdd('lobby', '0');
		assert.ok(res);
	});

	it('should remove sockets from channels', function () {
		this.mux.onSocketConnect(this.socket);
		this.mux.onChannelAdd('global', '0');
		let res = this.mux.onChannelRemove('global', '0');
		assert.ok(res);
		res = this.mux.onChannelRemove('global', '0');
		assert.ok(!res);
	});

	it('should move sockets to subchannels', function () {
		this.mux.onSocketConnect(this.socket);
		this.mux.onChannelAdd('global', '0');
		let res = this.mux.onSubchannelMove('global', '1', '0');
		assert.ok(res);
	});
});

'use strict';

// TODO: finish Windows installer.

// Verify that Node.js is compatible with this script.
try {
	eval('{ let [a] = [1]; }');
} catch (e) {
	console.error("We require Node.js v6 or later; you're using " + process.version);
	console.error("Update Node.js, then run:");
	console.error("\t# node post-install.js");
	console.error("Or run npm install again.");
	process.exit(1);
}

const child_process = require('child_process');
const fs = require('fs');
const http = require('http');
const https = require('https');
const os = require('os');
const path = require('path');

const panic = (...errstr) => {
	console.error(errstr.join('\n'));
	console.error("\t# node post-install.js");
	console.error("Or run npm install again.");
	process.exit(1);
};

const IS_WIN32 = (os.platform() === 'win32');
if (!IS_WIN32) {
	// Ensure that Redis isn't installed, that we're running this as root, and
	// that gmake is installed.
	let res;
	let arg = ['benchmark', 'check-aof', 'check-rdb', 'cli', 'sentinel', 'server']
		.map(bin => `which redis-${bin}`)
		.join(' && ');
	try {
		res = child_process.execSync(arg);
	} catch (e) {}

	if (res) {
		console.log("Redis is already installed!");
		process.exit(0);
	}

	if (os.userInfo().uid !== 0) panic("Redis must be installed as root! Run:");

	try {
		child_process.execSync("which gmake");
	} catch (e) {
		panic("gmake is required to install Redis! Install it, then run:");
	}
}

const REDIS_URL = IS_WIN32 ?
	'github.com/MSOpenTech/redis/releases/download/win-3.2.100/Redis-x64-3.2.100.msi' :
	'download.redis.io/releases/redis-3.2.1.tar.gz';
const FILENAME = path.resolve(__dirname, IS_WIN32 ? 'redis.msi' : 'redis.tar.gz');
const DIRNAME = path.resolve(__dirname, 'redis');

const fileExists = () => {
	let ret;
	try {
		ret = fs.lstatSync(FILENAME);
	} catch (e) {}
	return !!ret && ret.isFile();
};

const dirExists = () => {
	let ret;
	try {
		ret = fs.lstatSync(DIRNAME);
	} catch (e) {}
	return !!ret && ret.isDirectory();
};

// Kick off the installation process.
(function fetchRedis(usingSSL) {
	if (fileExists()) {
		if (dirExists()) return installRedis();
		return extractRedis();
	}

	let _http;
	let url;
	let pathIdx = REDIS_URL.indexOf('/');
	let opts = {
		hostname: REDIS_URL.substr(0, pathIdx),
		method: 'GET',
		path: REDIS_URL.substr(pathIdx),
		agent: false,
	};
	if (usingSSL) {
		_http = https;
		url = `https://${REDIS_URL}`;
		opts.protocol = 'https:';
		opts.port = 443;
	} else {
		_http = http;
		url = `http://${REDIS_URL}`;
		opts.protocol = 'http:';
		opts.port = 80;
	}

	console.log(`Fetching Redis installer from ${url}...`);
	let req = _http.request(opts, res => {
		if (res.statusCode !== 200) {
			if (usingSSL && IS_WIN32) {
				console.error(`Failed to fetch the Redis installer from ${url}. Trying again over HTTP.`);
				res.resume();
				fetchRedis(false);
				return;
			}

			res.resume();
			panic(`Failed to fetch the Redis installer from ${url}. Try again by running:`);
		}

		let data = new Buffer([]);
		res.on('data', chunk => {
			data = Buffer.concat([data, chunk]);
		});
		res.on('end', () => writeRedis(data));
	});

	req.on('error', () => panic(`Failed to fetch the Redis installer from ${url}. Try again by running:`));

	req.end();
})(IS_WIN32);

function writeRedis(data) {
	console.log(`Writing Redis installer to ${FILENAME} ...`);
	fs.writeFile(FILENAME, data, {}, (err, res) => {
		if (err) panic(`Successfully fetched the Redis installer, but failed to write to ${FILENAME}. Try again by running:`);
		extractRedis();
	});
}

function extractRedis() {
	if (IS_WIN32) return installRedis();

	console.log(`Extracting Redis installer tarball to ${DIRNAME}...`);
	child_process.exec(`mkdir ${DIRNAME} && tar xzf ${FILENAME} -C ${DIRNAME} --strip-components\=1`, err => {
		if (err) {
			panic(
				`Failed to extract ${FILENAME} for install. Try doing it yourself by running:`,
				`\t# tar xzf ${FILENAME} -C ${DIRNAME} --strip-components\=1`,
				"Then run:"
			);
		}
		installRedis();
	});
}

function installRedis() {
	if (IS_WIN32) {
		// TODO: install Redis MSI.
		return cleanUpRedis();
	}

	console.log("Installing Redis...");
	child_process.exec(`cd ${DIRNAME} && gmake ${os.arch() === 'x32' ? '32bit' : ''} && gmake install`, err => {
		if (err) {
			panic(
				"Failed to compile Redis. To compile it manually, run:",
				`\t# cd ${DIRNAME} && gmake && gmake install`,
				"Or, if you're running a 32 bit OS:",
				`\t# cd ${DIRNAME} && gmake 32bit && gmake install`,
				"Then run:"
			);
		}
		cleanUpRedis();
	});
}

function cleanUpRedis() {
	let arg = IS_WIN32 ? `del ${FILENAME}` : `rm -r ${FILENAME} ${DIRNAME}`;
	console.log("Performing housekeeping...");
	child_process.exec(arg, err => {
		if (err) {
			console.error("Failed to clean up. To clean up manually, run:");
			console.error(`\t# ${arg}`);
		}
		console.log("Redis was installed successfully!");
	});
}

const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const { EventEmitter } = require('node:events');
const { DEFAULT_SETTINGS } = require('../dist/main/settings');

function harness(portCheck = async () => true) {
  const app = new EventEmitter();
  app.getPath = () => '/mock-profile';
  app.requestSingleInstanceLock = () => true;
  app.whenReady = () => Promise.resolve();
  app.quit = () => {};
  const handlers = new Map(), sent = [], children = [], timers = [], writes = [];
  class Window {
    constructor() { this.webContents = { send: (...args) => sent.push(args) }; }
    isDestroyed() { return false; }
    setMenuBarVisibility() {}
    loadFile() {}
  }
  const sandbox = {
    exports: {}, __dirname: path.resolve(__dirname, '../dist/main'),
    process: { platform: 'win32', arch: 'x64', resourcesPath: '/mock-bin' },
    setTimeout: callback => { timers.push(callback); return callback; },
    clearTimeout: callback => { const i = timers.indexOf(callback); if (i >= 0) timers.splice(i, 1); },
    require: name => {
      if (name === 'electron') return { app, BrowserWindow: Window, ipcMain: { handle: (key, fn) => handlers.set(key, fn) } };
      if (name === 'node:fs') return {
        existsSync: () => true, mkdirSync() {}, renameSync() {},
        readFileSync: () => { throw new Error('No saved settings'); },
        writeFileSync: (_p, value) => writes.push(value),
      };
      if (name === './port') return { portIsAvailable: portCheck };
      if (name === 'node:child_process') return { spawn: () => {
        const child = new EventEmitter();
        child.stdout = new EventEmitter(); child.stderr = new EventEmitter();
        child.stdin = { write() {}, end() {} }; child.kill = () => {};
        children.push(child); return child;
      } };
      if (name.startsWith('.')) return require(path.resolve(__dirname, '../dist/main', name));
      return require(name);
    },
  };
  vm.runInNewContext(fs.readFileSync(path.resolve(__dirname, '../dist/main/index.js'), 'utf8'), sandbox);
  return { app, sent, children, timers, writes, call: (key, settings) => handlers.get('joiner:' + key)(null, settings) };
}
const settings = { ...DEFAULT_SETTINGS, link: 'https://telemost.yandex.ru/j/example', socksPass: 'private-test-password' };

test('busy port refuses startup without spawning or persisting settings', async () => {
  const h = harness(async () => false);
  assert.equal((await h.call('start', settings)).ok, false);
  assert.equal(h.children.length, 0);
  assert.equal(h.writes.length, 0);
});
test('Stop during asynchronous port check cancels pending startup', async () => {
  let resolve;
  const h = harness(() => new Promise(r => { resolve = r; }));
  const pending = h.call('start', settings);
  await h.call('stop'); resolve(true);
  assert.equal((await pending).ok, false);
  assert.equal(h.children.length, 0);
});
test('connected status recognizes underscore, persists no password and closing cancels reconnect', async () => {
  const h = harness();
  assert.equal((await h.call('start', settings)).ok, true);
  assert.equal(JSON.parse(h.writes[0]).socksPass, '');
  h.children[0].stdout.emit('data', Buffer.from('TUNNEL_CONNECTED\n'));
  assert.ok(h.sent.some(([channel, value]) => channel === 'joiner:status' && value === 'connected'));
  assert.ok(!h.sent.filter(([channel]) => channel === 'joiner:log').some(([, value]) => value.includes(settings.link) || value.includes(settings.socksPass)));
  h.children[0].emit('close', 1, null);
  assert.equal(h.timers.length, 1);
  h.app.emit('window-all-closed');
  assert.equal(h.timers.length, 0);
});

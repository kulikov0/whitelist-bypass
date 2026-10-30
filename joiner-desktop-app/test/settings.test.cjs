const test = require('node:test');
const assert = require('node:assert/strict');
const net = require('node:net');
const { DEFAULT_SETTINGS, buildJoinerArgs, redactLog } = require('../dist/main/settings');
const { portIsAvailable } = require('../dist/main/port');
const base = { ...DEFAULT_SETTINGS, socksPort: 10809, vp8Batch: 300, dns: '1.1.1.1', noTun: true, reliable: true, link: 'https://telemost.yandex.ru/j/test-placeholder' };

test('Telemost GUI launches the agreed SOCKS-only reliable video configuration', () => {
  const args = buildJoinerArgs(base);
  for (const [key, value] of Object.entries({
    '--platform': 'telemost', '--socks-port': '10809', '--vp8-fps': '24',
    '--vp8-batch': '300', '--tunnel-mode': 'video', '--dns': '1.1.1.1',
  })) assert.equal(args[args.indexOf(key) + 1], value);
  assert.ok(args.includes('--reliable'));
  assert.ok(args.includes('--no-tun'));
  assert.ok(!args.includes('--debug'));
});
test('KCP checkbox controls the Telemost flag and WB Stream keeps its flag', () => {
  assert.ok(!buildJoinerArgs({ ...base, reliable: false }).includes('--reliable'));
  assert.ok(buildJoinerArgs({ ...base, platform: 'wbstream' }).includes('--reliable'));
  const bitrix = buildJoinerArgs({ ...base, platform: 'bitrix', dualTrack: true });
  assert.ok(bitrix.includes('--reliable'));
  assert.ok(bitrix.includes('--dual-track'));
});
test('invalid ports and malformed settings fail before spawning a child', () => {
  for (const socksPort of [0, 65536, 10809.5, NaN]) assert.throws(() => buildJoinerArgs({ ...base, socksPort }));
  assert.throws(() => buildJoinerArgs({ ...base, noTun: 'true' }));
  assert.throws(() => buildJoinerArgs({ ...base, link: '' }));
});
test('log output does not expose the conference link or SOCKS password', () => {
  const settings = { ...base, socksPass: 'test-secret' };
  const result = redactLog('run --link ' + settings.link + ' --socks-pass test-secret', settings);
  assert.ok(!result.includes(settings.link));
  assert.ok(!result.includes('test-secret'));
});
test('a live listener is rejected without interrupting it, then the released port is usable', async () => {
  const server = net.createServer((socket) => socket.end('still-alive'));
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  try {
    assert.equal(await portIsAvailable(port), false);
    const data = await new Promise((resolve, reject) => {
      const socket = net.connect(port, '127.0.0.1');
      socket.on('data', (chunk) => resolve(chunk.toString()));
      socket.on('error', reject);
    });
    assert.equal(data, 'still-alive');
  } finally { await new Promise((resolve) => server.close(resolve)); }
  assert.equal(await portIsAvailable(port), true);
});

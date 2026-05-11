type JoinerPlatform = 'wbstream' | 'telemost' | 'vk';

function detectPlatform(url: string): JoinerPlatform | null {
  const u = url.toLowerCase();
  if (!u) return null;
  if (u.includes('wbstream://') || u.includes('stream.wb.ru')) return 'wbstream';
  if (u.includes('telemost.yandex')) return 'telemost';
  return 'vk';
}

function platformLabel(p: JoinerPlatform | null): string {
  switch (p) {
    case 'wbstream': return 'WB Stream';
    case 'telemost': return 'Telemost';
    case 'vk': return 'VK';
    default: return '-';
  }
}

interface Bridge {
  start(settings: any): Promise<{ ok: boolean; error?: string }>;
  stop(): Promise<{ ok: boolean }>;
  onLog(cb: (text: string) => void): void;
  onStatus(cb: (status: string) => void): void;
  onRunning(cb: (running: boolean) => void): void;
}
declare const bridge: Bridge;

const $ = (id: string) => document.getElementById(id) as HTMLElement;
const input = (id: string) => document.getElementById(id) as HTMLInputElement;
const select = (id: string) => document.getElementById(id) as HTMLSelectElement;

const logEl = $('log') as HTMLPreElement;
const statusEl = $('status');
const startBtn = $('start') as HTMLButtonElement;
const stopBtn = $('stop') as HTMLButtonElement;
const platformHint = $('platformHint');
const linkInput = input('link');

stopBtn.disabled = true;

function refreshPlatformHint() {
  const p = detectPlatform(linkInput.value.trim());
  platformHint.textContent = `Detected platform: ${platformLabel(p)}`;
  platformHint.dataset.detected = p ?? '';
}
linkInput.addEventListener('input', refreshPlatformHint);
refreshPlatformHint();

function appendLog(text: string) {
  logEl.textContent += text;
  logEl.scrollTop = logEl.scrollHeight;
}

bridge.onLog((text) => appendLog(text));
bridge.onStatus((s) => {
  statusEl.textContent = s;
  statusEl.dataset.state = s;
});
bridge.onRunning((running) => {
  startBtn.disabled = running;
  stopBtn.disabled = !running;
});

startBtn.addEventListener('click', async () => {
  appendLog('\n[ui] starting joiner...\n');
  const link = linkInput.value.trim();
  if (!link) {
    appendLog('[ui] link is required\n');
    return;
  }
  const platform = detectPlatform(link);
  if (!platform) {
    appendLog('[ui] link does not look like a WB Stream or Telemost call\n');
    return;
  }
  const settings = {
    platform,
    link,
    displayName: input('name').value.trim() || 'Joiner',
    socksPort: parseInt(input('socksPort').value, 10) || 1080,
    socksUser: input('socksUser').value,
    socksPass: input('socksPass').value,
    tunnelMode: select('tunnelMode').value,
    vp8Fps: parseInt(input('vp8Fps').value, 10) || 24,
    vp8Batch: parseInt(input('vp8Batch').value, 10) || 30,
    resources: select('resources').value,
    dns: input('dns').value.trim() || '1.1.1.1,8.8.8.8',
    noTun: input('noTun').checked,
  };
  const r = await bridge.start(settings);
  if (!r.ok) appendLog(`[ui] start failed: ${r.error}\n`);
});

stopBtn.addEventListener('click', async () => {
  appendLog('\n[ui] stopping joiner...\n');
  await bridge.stop();
});

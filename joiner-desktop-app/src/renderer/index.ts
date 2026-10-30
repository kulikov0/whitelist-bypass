type JoinerPlatform = 'wbstream' | 'telemost' | 'vk' | 'dion' | 'bitrix';

function detectPlatform(url: string): JoinerPlatform | null {
  const u = url.toLowerCase();
  if (!u) return null;
  if (u.includes('wbstream://') || u.includes('stream.wb.ru')) return 'wbstream';
  if (u.includes('telemost.yandex')) return 'telemost';
  if (u.includes('dion://') || u.includes('dion.vc')) return 'dion';
  if (u.includes('bitrix24') && u.includes('/video/')) return 'bitrix';
  return 'vk';
}

function platformLabel(p: JoinerPlatform | null): string {
  switch (p) {
    case 'wbstream': return 'WB Stream';
    case 'telemost': return 'Telemost';
    case 'vk': return 'VK';
    case 'dion': return 'DION';
    case 'bitrix': return 'Bitrix';
    default: return '-';
  }
}

interface Bridge {
  state(): Promise<{ settings: any; running: boolean; status: string; log: string }>;
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
const downloadLogsBtn = $('downloadLogs') as HTMLImageElement;
const platformHint = $('platformHint');
const linkInput = input('link');

stopBtn.disabled = true;
startBtn.disabled = true;

downloadLogsBtn.addEventListener('click', () => {
  const blob = new Blob([logEl.textContent || ''], { type: 'text/plain' });
  const anchor = document.createElement('a');
  anchor.href = URL.createObjectURL(blob);
  anchor.download = 'joiner-logs-' + new Date().toISOString().replace(/[:.]/g, '-') + '.txt';
  anchor.click();
  URL.revokeObjectURL(anchor.href);
});

function refreshPlatformHint() {
  const p = detectPlatform(linkInput.value.trim());
  platformHint.textContent = `Detected platform: ${platformLabel(p)}`;
  platformHint.dataset.detected = p ?? '';
}
linkInput.addEventListener('input', refreshPlatformHint);
refreshPlatformHint();

const MAX_LOG_CHARS = 400000;
const LOG_TRIM_KEEP_CHARS = 200000;

function trimLogText(text: string): string {
  if (text.length <= MAX_LOG_CHARS) return text;
  const cut = text.length - LOG_TRIM_KEEP_CHARS;
  const newline = text.indexOf('\n', cut);
  return text.slice(newline === -1 ? cut : newline + 1);
}

function appendLog(text: string) {
  logEl.textContent = trimLogText((logEl.textContent || '') + text);
  logEl.scrollTop = logEl.scrollHeight;
}

bridge.onLog((text) => appendLog(text));
bridge.onStatus((s) => {
  statusEl.textContent = s;
  statusEl.dataset.state = s;
});
function setRunning(running: boolean) {
  startBtn.disabled = running;
  stopBtn.disabled = !running;
  document.querySelectorAll<HTMLInputElement | HTMLSelectElement>('.form input, .form select')
    .forEach((element) => { element.disabled = running; });
}
bridge.onRunning(setRunning);
bridge.state().then((state) => {
  const settings = state.settings;
  const fields: Record<string, string> = {
    link: 'link', name: 'displayName', socksPort: 'socksPort',
    socksUser: 'socksUser', socksPass: 'socksPass', tunnelMode: 'tunnelMode',
    vp8Fps: 'vp8Fps', vp8Batch: 'vp8Batch', resources: 'resources', dns: 'dns',
  };
  for (const [id, key] of Object.entries(fields)) {
    (document.getElementById(id) as HTMLInputElement).value = String(settings[key] ?? '');
  }
  for (const key of ['noTun', 'dualTrack', 'reliable']) input(key).checked = Boolean(settings[key]);
  refreshPlatformHint();
  statusEl.textContent = state.status;
  statusEl.dataset.state = state.status;
  appendLog(state.log);
  setRunning(state.running);
}).catch(() => {
  appendLog('[ui] Could not load saved settings.\n');
  startBtn.disabled = false;
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
    dualTrack: input('dualTrack').checked,
    reliable: input('reliable').checked,
  };
  startBtn.disabled = true;
  try {
    const r = await bridge.start(settings);
    if (!r.ok) { appendLog(`[ui] start failed: ${r.error}\n`); setRunning(false); }
  } catch {
    appendLog('[ui] Could not start the client.\n');
    setRunning(false);
  }
});

stopBtn.addEventListener('click', async () => {
  appendLog('\n[ui] stopping joiner...\n');
  await bridge.stop();
});

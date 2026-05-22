type JoinerPlatform = 'wbstream' | 'telemost' | 'vk' | 'dion';

function detectPlatform(url: string): JoinerPlatform | null {
  const u = url.toLowerCase();
  if (!u) return null;
  if (u.includes('wbstream://') || u.includes('stream.wb.ru')) return 'wbstream';
  if (u.includes('telemost.yandex')) return 'telemost';
  if (u.includes('dion://') || u.includes('dion.vc')) return 'dion';
  return 'vk';
}

function platformLabel(p: JoinerPlatform | null): string {
  switch (p) {
    case 'wbstream': return 'WB Stream';
    case 'telemost': return 'Telemost';
    case 'vk': return 'VK';
    case 'dion': return 'DION';
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
const downloadLogsBtn = $('downloadLogs') as HTMLImageElement;
const platformHint = $('platformHint');
const linkInput = input('link');

stopBtn.disabled = true;

downloadLogsBtn.addEventListener('click', () => {
  const blob = new Blob([logEl.textContent || ''], { type: 'text/plain' });
  const anchor = document.createElement('a');
  anchor.href = URL.createObjectURL(blob);
  anchor.download = 'joiner-logs-' + new Date().toISOString().replace(/[:.]/g, '-') + '.txt';
  anchor.click();
  URL.revokeObjectURL(anchor.href);
});

const profileSelect = select('profile');

const DEFAULT_PROFILE = {
  link: '',
  name: 'Joiner',
  socksPort: '1080',
  dns: '1.1.1.1,8.8.8.8',
  socksUser: '',
  socksPass: '',
  tunnelMode: 'video',
  resources: 'default',
  vp8Fps: '24',
  vp8Batch: '30',
  noTun: false
};

function loadProfile(id: string) {
  try {
    const raw = localStorage.getItem('profile_' + id);
    const p = raw ? { ...DEFAULT_PROFILE, ...JSON.parse(raw) } : DEFAULT_PROFILE;
    linkInput.value = p.link || '';
    input('name').value = p.name || 'Joiner';
    input('socksPort').value = p.socksPort || '1080';
    input('dns').value = p.dns || '1.1.1.1,8.8.8.8';
    input('socksUser').value = p.socksUser || '';
    input('socksPass').value = p.socksPass || '';
    select('tunnelMode').value = p.tunnelMode || 'video';
    select('resources').value = p.resources || 'default';
    input('vp8Fps').value = p.vp8Fps || '24';
    input('vp8Batch').value = p.vp8Batch || '30';
    input('noTun').checked = !!p.noTun;
    refreshPlatformHint();
  } catch (e) {
    console.error('Failed to load profile', e);
  }
}

function saveProfile() {
  const id = profileSelect.value;
  const p = {
    link: linkInput.value,
    name: input('name').value,
    socksPort: input('socksPort').value,
    dns: input('dns').value,
    socksUser: input('socksUser').value,
    socksPass: input('socksPass').value,
    tunnelMode: select('tunnelMode').value,
    resources: select('resources').value,
    vp8Fps: input('vp8Fps').value,
    vp8Batch: input('vp8Batch').value,
    noTun: input('noTun').checked
  };
  localStorage.setItem('profile_' + id, JSON.stringify(p));
}

profileSelect.addEventListener('change', () => loadProfile(profileSelect.value));

document.querySelectorAll('input, select').forEach(el => {
  if (el.id !== 'profile') {
    el.addEventListener('change', saveProfile);
    el.addEventListener('input', saveProfile);
  }
});

const lastProfile = localStorage.getItem('last_profile') || '1';
profileSelect.value = lastProfile;
profileSelect.addEventListener('change', () => localStorage.setItem('last_profile', profileSelect.value));
loadProfile(lastProfile);

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

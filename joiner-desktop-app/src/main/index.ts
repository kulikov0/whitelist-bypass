import { app, BrowserWindow, ipcMain } from 'electron';
import { spawn, ChildProcess } from 'node:child_process';
import { existsSync, readFileSync, writeFileSync, mkdirSync, renameSync } from 'node:fs';
import { join } from 'node:path';
import { IPC, JoinerSettings } from '../constants';
import { DEFAULT_SETTINGS, buildJoinerArgs, redactLog, validateSettings } from './settings';
import { portIsAvailable } from './port';

// Single global joiner process. We never run two tunnels at once: the
// wintun adapter and the route table are exclusive resources.
let joinerProcess: ChildProcess | null = null;
let mainWindow: BrowserWindow | null = null;
let captchaWindow: BrowserWindow | null = null;
let userRequestedStop = false;
let reconnectTimer: NodeJS.Timeout | null = null;
let retryCount = 0;
let lastSettings: JoinerSettings | null = null;
const MAX_RETRIES = 8;
let startInProgress = false;
let currentStatus = 'idle';
let running = false;
let logBuffer = '';
const profileDir = app.getPath('userData');
mkdirSync(profileDir, { recursive: true });
const settingsFile = join(profileDir, 'settings.json');

function readSettings(): JoinerSettings {
  try {
    const settings = { ...DEFAULT_SETTINGS, ...JSON.parse(readFileSync(settingsFile, 'utf8')), socksPass: '' };
    validateSettings(settings);
    return settings;
  }
  catch { return { ...DEFAULT_SETTINGS }; }
}
function saveSettings(settings: JoinerSettings) {
  const tempFile = settingsFile + '.tmp';
  writeFileSync(tempFile, JSON.stringify({ ...settings, socksPass: '' }, null, 2), { mode: 0o600 });
  renameSync(tempFile, settingsFile);
}

const gotLock = app.requestSingleInstanceLock();
if (!gotLock) app.quit();
app.on('second-instance', () => {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.show();
    mainWindow.focus();
  }
});

function openCaptchaWindow(url: string) {
  if (captchaWindow && !captchaWindow.isDestroyed()) {
    captchaWindow.loadURL(url);
    captchaWindow.focus();
    return;
  }
  captchaWindow = new BrowserWindow({
    width: 520,
    height: 640,
    title: 'Solve the captcha',
    parent: mainWindow ?? undefined,
    autoHideMenuBar: true,
    webPreferences: { contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  captchaWindow.loadURL(url);
  captchaWindow.on('closed', () => { captchaWindow = null; });
}

function closeCaptchaWindow() {
  if (captchaWindow && !captchaWindow.isDestroyed()) {
    captchaWindow.close();
  }
  captchaWindow = null;
}

function resolveJoinerExe(): string {
  // When packaged, electron-builder copies the backend binary into
  // resources/ under the OS-appropriate name. In dev, fall back to
  // the per-arch artifact next to the Go source.
  const exeName = process.platform === 'win32' ? 'desktop-joiner.exe' : 'desktop-joiner';
  const packaged = join(process.resourcesPath || '', exeName);
  if (existsSync(packaged)) return packaged;

  const baseDir = join(__dirname, '..', '..', 'desktop-joiner');
  if (process.platform === 'darwin') {
    return join(baseDir, 'desktop-joiner-darwin');
  }
  const archMap: Record<string, string> = { x64: 'x64', arm64: 'arm64', ia32: 'ia32' };
  const archTag = archMap[process.arch] ?? 'x64';
  const suffix = process.platform === 'win32' ? '.exe' : '';
  const platTag = process.platform === 'win32' ? 'windows' : 'linux';
  return join(baseDir, `desktop-joiner-${platTag}-${archTag}${suffix}`);
}

function send(channel: string, payload: unknown) {
  if (channel === IPC.LOG) {
    payload = redactLog(String(payload), lastSettings);
    logBuffer = (logBuffer + payload).slice(-200000);
  }
  if (channel === IPC.STATUS) currentStatus = String(payload);
  if (channel === IPC.RUNNING) running = Boolean(payload);
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send(channel, payload);
  }
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1060,
    height: 780,
    minWidth: 850,
    minHeight: 650,
    title: 'WhitelistBypass Joiner',
    icon: join(__dirname, '..', '..', 'resources', 'icon.png'),
    webPreferences: {
      preload: join(__dirname, '..', 'preload', 'index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });
  mainWindow.setMenuBarVisibility(false);
  mainWindow.loadFile(join(__dirname, '..', '..', 'index.html'));
}

app.whenReady().then(() => {
  if (!gotLock) return;
  createWindow();
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  userRequestedStop = true;
  if (reconnectTimer) clearTimeout(reconnectTimer);
  stopJoiner();
  if (process.platform !== 'darwin') app.quit();
});

async function spawnJoiner(settings: JoinerSettings): Promise<{ ok: boolean; error?: string }> {
  const exe = resolveJoinerExe();
  if (!existsSync(exe)) {
    return { ok: false, error: `desktop-joiner binary not found at ${exe}` };
  }
  const tunSupported =
    process.platform === 'win32' || process.platform === 'linux' || process.platform === 'darwin';
  const noTun = tunSupported ? settings.noTun : true;
  if (process.platform !== 'win32' && !noTun && process.getuid && process.getuid() !== 0) {
    send(IPC.LOG, `[main] WARNING: ${process.platform} TUN routing needs root; relaunch with sudo or untick the TUN option\n`);
  }
  let args: string[];
  try { args = buildJoinerArgs({ ...settings, noTun }); }
  catch (err) { return { ok: false, error: (err as Error).message }; }
  if (!(await portIsAvailable(settings.socksPort))) {
    return { ok: false, error: 'SOCKS port is already in use. Stop the other client first; no process was changed.' };
  }
  if (userRequestedStop) return { ok: false, error: 'Start cancelled' };
  try { saveSettings(settings); }
  catch { return { ok: false, error: 'Cannot save GUI settings' }; }

  const elevateOnLinux =
    process.platform === 'linux' && !noTun &&
    process.getuid && process.getuid() !== 0;
  const spawnCmd = elevateOnLinux ? 'pkexec' : exe;
  const spawnArgs = elevateOnLinux ? [exe, ...args] : args;
  const commandLine = [spawnCmd, ...spawnArgs].map((s) => (/\s/.test(s) ? `"${s}"` : s)).join(' ');
  send(IPC.LOG, `[main] spawning: ${commandLine}\n`);
  try {
    joinerProcess = spawn(spawnCmd, spawnArgs, { windowsHide: true });
  } catch (err) {
    return { ok: false, error: `spawn failed: ${(err as Error).message}` };
  }
  send(IPC.RUNNING, true);
  send(IPC.STATUS, 'starting');

  joinerProcess.on('error', (err) => {
    send(IPC.LOG, `[main] spawn error: ${err.message}\n`);
    send(IPC.STATUS, 'stopped');
    send(IPC.RUNNING, false);
    // Keep ownership until close so an old child cannot clear a newer start.
  });
  const handleOutput = (text: string) => {
    send(IPC.LOG, text);
    if (/TUNNEL[ _]ACTIVE/.test(text)) send(IPC.STATUS, 'active');
    if (/TUNNEL[ _]CONNECTED/.test(text)) {
      send(IPC.STATUS, 'connected');
      retryCount = 0;
    }
    const captchaMatch = text.match(/STATUS:CAPTCHA:(\S+)/);
    if (captchaMatch) {
      openCaptchaWindow(captchaMatch[1]);
    } else if (captchaWindow && /captcha solved|Auth complete|TUNNEL/i.test(text)) {
      closeCaptchaWindow();
    }
  };
  joinerProcess.stdout?.on('data', (b: Buffer) => handleOutput(b.toString()));
  joinerProcess.stderr?.on('data', (b: Buffer) => handleOutput(b.toString()));
  joinerProcess.on('close', (code, signal) => {
    closeCaptchaWindow();
    send(IPC.LOG, `\n[main] joiner exited code=${code} signal=${signal}\n`);
    send(IPC.STATUS, 'stopped');
    send(IPC.RUNNING, false);
    joinerProcess = null;

    if (userRequestedStop || !lastSettings) return;
    if (retryCount >= MAX_RETRIES) {
      send(IPC.LOG, `[main] auto-reconnect: giving up after ${MAX_RETRIES} attempts\n`);
      return;
    }
    retryCount++;
    const delayMs = Math.min(30_000, 2_000 * 2 ** (retryCount - 1));
    send(IPC.LOG, `[main] auto-reconnect attempt ${retryCount}/${MAX_RETRIES} in ${Math.round(delayMs / 1000)}s\n`);
    send(IPC.STATUS, 'reconnecting');
    reconnectTimer = setTimeout(async () => {
      reconnectTimer = null;
      if (userRequestedStop || !lastSettings) return;
      startInProgress = true;
      const r = await spawnJoiner(lastSettings).finally(() => { startInProgress = false; });
      if (!r.ok) {
        send(IPC.STATUS, 'stopped');
        send(IPC.LOG, `[main] auto-reconnect spawn failed: ${r.error}\n`);
      }
    }, delayMs);
  });
  return { ok: true };
}

ipcMain.handle(IPC.START, async (_e, settings: JoinerSettings) => {
  if (joinerProcess || startInProgress) {
    return { ok: false, error: 'joiner already running' };
  }
  userRequestedStop = false;
  retryCount = 0;
  lastSettings = settings;
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  startInProgress = true;
  try {
    const result = await spawnJoiner(settings);
    if (!result.ok) send(IPC.STATUS, 'stopped');
    return result;
  } finally {
    startInProgress = false;
  }
});

ipcMain.handle(IPC.STATE, async () => {
  const settings = readSettings();
  const occupied = !running && !startInProgress && !(await portIsAvailable(settings.socksPort));
  return { settings, running, status: occupied ? 'port in use' : currentStatus, log: logBuffer };
});

ipcMain.handle(IPC.STOP, async () => {
  userRequestedStop = true;
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  stopJoiner();
  return { ok: true };
});

function stopJoiner() {
  closeCaptchaWindow();
  if (!joinerProcess) return;
  // On Linux when the Go binary was spawned via pkexec, it runs as
  // root and we (the user) cannot SIGTERM it. The binary watches
  // stdin: writing "QUIT\n" and closing the pipe triggers the same
  // shutdown path as SIGTERM.
  try { joinerProcess.stdin?.write('QUIT\n'); } catch {}
  try { joinerProcess.stdin?.end(); } catch {}
  try {
    joinerProcess.kill('SIGTERM');
  } catch {}
}

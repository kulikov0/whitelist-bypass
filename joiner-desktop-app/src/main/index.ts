import { app, BrowserWindow, ipcMain } from 'electron';
import { spawn, ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { IPC, JoinerSettings } from '../constants';

// Single global joiner process. We never run two tunnels at once: the
// wintun adapter and the route table are exclusive resources.
let joinerProcess: ChildProcess | null = null;
let mainWindow: BrowserWindow | null = null;
let captchaWindow: BrowserWindow | null = null;

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
  // When packaged, electron-builder copies windows-joiner.exe into
  // resources/. In dev, look next to the source binary.
  const packaged = join(process.resourcesPath || '', 'windows-joiner.exe');
  if (existsSync(packaged)) return packaged;
  const fallback = join(__dirname, '..', '..', '..', 'prebuilts', 'windows-joiner-x64.exe');
  return fallback;
}

function send(channel: string, payload: unknown) {
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send(channel, payload);
  }
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 900,
    height: 600,
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
  createWindow();
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  stopJoiner();
  if (process.platform !== 'darwin') app.quit();
});

ipcMain.handle(IPC.START, async (_e, settings: JoinerSettings) => {
  if (joinerProcess) {
    return { ok: false, error: 'joiner already running' };
  }
  const exe = resolveJoinerExe();
  if (!existsSync(exe)) {
    return { ok: false, error: `windows-joiner.exe not found at ${exe}` };
  }
  const args = [
    '--platform', settings.platform,
    '--link', settings.link,
    '--name', settings.displayName,
    '--socks-port', String(settings.socksPort),
    '--tunnel-mode', settings.tunnelMode,
    '--vp8-fps', String(settings.vp8Fps),
    '--vp8-batch', String(settings.vp8Batch),
    '--resources', settings.resources,
    '--dns', settings.dns,
  ];
  if (settings.socksUser) args.push('--socks-user', settings.socksUser);
  if (settings.socksPass) args.push('--socks-pass', settings.socksPass);
  if (settings.noTun) args.push('--no-tun');

  const commandLine = [exe, ...args].map((s) => (/\s/.test(s) ? `"${s}"` : s)).join(' ');
  send(IPC.LOG, `[main] spawning: ${commandLine}\n`);
  try {
    joinerProcess = spawn(exe, args, { windowsHide: true });
  } catch (err) {
    return { ok: false, error: `spawn failed: ${(err as Error).message}` };
  }
  send(IPC.RUNNING, true);
  send(IPC.STATUS, 'starting');

  joinerProcess.on('error', (err) => {
    send(IPC.LOG, `[main] spawn error: ${err.message}\n`);
    send(IPC.STATUS, 'stopped');
    send(IPC.RUNNING, false);
    joinerProcess = null;
  });
  const handleOutput = (text: string) => {
    send(IPC.LOG, text);
    if (text.includes('TUNNEL ACTIVE')) send(IPC.STATUS, 'active');
    if (text.includes('TUNNEL CONNECTED')) send(IPC.STATUS, 'connected');
    const captchaMatch = text.match(/STATUS:CAPTCHA:(\S+)/);
    if (captchaMatch) {
      openCaptchaWindow(captchaMatch[1]);
    } else if (captchaWindow && /captcha solved|Auth complete|TUNNEL/i.test(text)) {
      closeCaptchaWindow();
    }
  };
  joinerProcess.stdout?.on('data', (b: Buffer) => handleOutput(b.toString()));
  joinerProcess.stderr?.on('data', (b: Buffer) => handleOutput(b.toString()));
  joinerProcess.on('exit', (code, signal) => {
    closeCaptchaWindow();
    send(IPC.LOG, `\n[main] joiner exited code=${code} signal=${signal}\n`);
    send(IPC.STATUS, 'stopped');
    send(IPC.RUNNING, false);
    joinerProcess = null;
  });
  return { ok: true };
});

ipcMain.handle(IPC.STOP, async () => {
  stopJoiner();
  return { ok: true };
});

function stopJoiner() {
  closeCaptchaWindow();
  if (!joinerProcess) return;
  try {
    // SIGTERM on Windows ends up as TerminateProcess for the child.
    // The Go binary registers a signal handler for SIGTERM which
    // triggers Tunnel.Stop and tears down routes cleanly.
    joinerProcess.kill('SIGTERM');
  } catch {}
}

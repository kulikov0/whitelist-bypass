import { app, BrowserWindow, ipcMain } from 'electron';
import { spawn, ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { IPC, JoinerSettings } from '../constants';

// Single global joiner process. We never run two tunnels at once: the
// wintun adapter and the route table are exclusive resources.
let joinerProcess: ChildProcess | null = null;
let mainWindow: BrowserWindow | null = null;

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
    webPreferences: {
      preload: join(__dirname, '..', 'preload', 'index.js'),
      contextIsolation: true,
      nodeIntegration: false,
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

  try {
    joinerProcess = spawn(exe, args, { windowsHide: true });
  } catch (err) {
    return { ok: false, error: `spawn failed: ${(err as Error).message}` };
  }
  send(IPC.RUNNING, true);
  send(IPC.STATUS, 'starting');

  joinerProcess.stdout?.on('data', (b: Buffer) => send(IPC.LOG, b.toString()));
  joinerProcess.stderr?.on('data', (b: Buffer) => {
    const text = b.toString();
    send(IPC.LOG, text);
    if (text.includes('TUNNEL ACTIVE')) send(IPC.STATUS, 'active');
    if (text.includes('TUNNEL CONNECTED')) send(IPC.STATUS, 'connected');
  });
  joinerProcess.on('exit', (code, signal) => {
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
  if (!joinerProcess) return;
  try {
    // SIGTERM on Windows ends up as TerminateProcess for the child.
    // The Go binary registers a signal handler for SIGTERM which
    // triggers Tunnel.Stop and tears down routes cleanly.
    joinerProcess.kill('SIGTERM');
  } catch {}
}

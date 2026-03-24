const { app, BrowserWindow, session, ipcMain } = require('electron');
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const TelemostAutoclick = require('./telemost-autoclick');
const VkAutoclick = require('./vk-autoclick');

var hooksDir = app.isPackaged
  ? path.join(process.resourcesPath, 'hooks')
  : path.join(__dirname, '..', 'hooks');
var logCapture = "window.__hookLogs=window.__hookLogs||[];var _ol=console.log;console.log=function(){_ol.apply(console,arguments);var m=Array.prototype.slice.call(arguments).join(' ');if(m.indexOf('[HOOK]')!==-1)window.__hookLogs.push(m)};";

var tunnelMode = 'dc';
var currentPlatform = 'vk';

function loadHook(url) {
  var isTelemost = url.includes('telemost.yandex');
  var newPlatform = isTelemost ? 'telemost' : 'vk';
  if (newPlatform !== currentPlatform && tunnelMode.startsWith('pion')) {
    currentPlatform = newPlatform;
    killRelay();
    setTimeout(startRelay, 500);
  } else {
    currentPlatform = newPlatform;
  }
  if (tunnelMode === 'pion-video' || tunnelMode === 'pion-dc') {
    var hookFile = isTelemost ? 'video-joiner-telemost.js' : 'video-joiner-vk.js';
    var hook = fs.readFileSync(path.join(hooksDir, hookFile), 'utf8');
    return logCapture + 'window.PION_PORT=9002;' + hook;
  }
  var hook = isTelemost
    ? fs.readFileSync(path.join(hooksDir, 'dc-creator-telemost.js'), 'utf8')
    : fs.readFileSync(path.join(hooksDir, 'dc-creator-vk.js'), 'utf8');
  return logCapture + hook;
}

let mainWindow;
let relayProcess;

function startRelay() {
  var port = tunnelMode.startsWith('pion') ? 9002 : 9000;
  const net = require('net');
  const sock = new net.Socket();
  sock.setTimeout(1000);
  sock.on('connect', () => {
    sock.destroy();
    console.log('[relay] already running on :' + port);
    if (mainWindow) mainWindow.webContents.send('relay-log', 'Using existing relay on :' + port);
  });
  sock.on('error', () => {
    sock.destroy();
    spawnRelay();
  });
  sock.on('timeout', () => {
    sock.destroy();
    spawnRelay();
  });
  sock.connect(port, '127.0.0.1');
}

function spawnRelay() {
  var relayName = process.platform === 'win32' ? 'relay.exe' : 'relay';
  var relayPath = app.isPackaged
    ? path.join(process.resourcesPath, relayName)
    : path.join(__dirname, '..', 'relay', relayName);
  var relayMode = 'dc-creator';
  if (tunnelMode === 'pion-video') relayMode = currentPlatform === 'telemost' ? 'telemost-video-creator' : 'vk-video-creator';
  var relayArgs = ['--mode', relayMode];
  if (tunnelMode.startsWith('pion')) relayArgs.push('--ws-port', '9002');
  relayProcess = spawn(relayPath, relayArgs, {
    stdio: ['ignore', 'pipe', 'pipe']
  });
  relayProcess.stdout.on('data', (data) => {
    data.toString().trim().split('\n').forEach((msg) => {
      if (!msg) return;
      console.log('[relay]', msg);
      if (mainWindow) mainWindow.webContents.send('relay-log', msg);
    });
  });
  relayProcess.stderr.on('data', (data) => {
    data.toString().trim().split('\n').forEach((msg) => {
      if (!msg) return;
      console.log('[relay]', msg);
      if (mainWindow) mainWindow.webContents.send('relay-log', msg);
    });
  });
  relayProcess.on('close', (code) => {
    if (mainWindow) mainWindow.webContents.send('relay-log', 'Relay exited with code ' + code);
  });
}

function stripCSP(ses) {
  ses.webRequest.onHeadersReceived((details, callback) => {
    var headers = { ...details.responseHeaders };
    delete headers['content-security-policy'];
    delete headers['Content-Security-Policy'];
    delete headers['content-security-policy-report-only'];
    delete headers['Content-Security-Policy-Report-Only'];
    callback({ responseHeaders: headers });
  });
}

function createWindow() {
  const ses = session.fromPartition('persist:creator');
  stripCSP(ses);
  ses.setPermissionRequestHandler((webContents, permission, callback) => {
    callback(true);
  });
  ses.setPermissionCheckHandler(() => true);

  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    icon: path.join(__dirname, 'resources', 'icon.png'),
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true,
      webviewTag: true
    }
  });

  ses.setUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36');

  app.on('session-created', stripCSP);

  mainWindow.loadFile('index.html');
  mainWindow.on('closed', () => { mainWindow = null; });

  var telemostAutoclick = new TelemostAutoclick();
  var vkAutoclick = new VkAutoclick();
  mainWindow.webContents.on('did-attach-webview', (e, wvContents) => {
    wvContents.on('did-navigate', (e, url) => {
      if (url.includes('telemost.yandex')) {
        vkAutoclick.stop();
        telemostAutoclick.attach(wvContents);
      } else if (url.includes('vk.com')) {
        telemostAutoclick.stop();
        vkAutoclick.attach(wvContents);
      } else {
        telemostAutoclick.stop();
        vkAutoclick.stop();
      }
    });
    wvContents.on('console-message', (e, level, msg) => {
      if (msg.indexOf('Connection state: disconnected') !== -1 || msg.indexOf('Connection state: failed') !== -1) {
        vkAutoclick.kickDisconnected();
      }
    });
  });
}

function killRelay() {
  if (relayProcess) {
    relayProcess.kill();
    relayProcess = null;
  }
}


ipcMain.handle('get-hook-code', (e, url) => {
  return loadHook(url);
});

ipcMain.handle('set-tunnel-mode', (e, mode) => {
  if (['dc', 'pion-video'].indexOf(mode) !== -1) {
    tunnelMode = mode;
    killRelay();
    setTimeout(function() {
      startRelay();
      console.log('[main] tunnel mode:', tunnelMode);
    }, 500);
  }
});

app.whenReady().then(() => {
  startRelay();
  createWindow();
});

app.on('window-all-closed', () => {
  killRelay();
  app.quit();
});

app.on('before-quit', killRelay);
process.on('exit', killRelay);
process.on('SIGINT', () => { killRelay(); process.exit(); });
process.on('SIGTERM', () => { killRelay(); process.exit(); });

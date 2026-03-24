const { app, BrowserWindow, session, ipcMain } = require('electron');
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const TelemostAutoclick = require('./telemost-autoclick');
const VkAutoclick = require('./vk-autoclick');

var hooksDir = app.isPackaged
  ? path.join(process.resourcesPath, 'hooks')
  : path.join(__dirname, '..', 'hooks');
var logCapture = "if(!window.__logCaptureInstalled){window.__logCaptureInstalled=true;window.__hookLogs=[];var _ol=console.log.bind(console);console.log=function(){_ol.apply(null,arguments);var m=Array.prototype.slice.call(arguments).join(' ');if(m.indexOf('[HOOK]')!==-1)window.__hookLogs.push(m)}}";
var relayPath = app.isPackaged
  ? path.join(process.resourcesPath, process.platform === 'win32' ? 'relay.exe' : 'relay')
  : path.join(__dirname, '..', 'relay', process.platform === 'win32' ? 'relay.exe' : 'relay');

var tabs = new Map(); // tabId -> { relay, tunnelMode, platform, dcPort, pionPort }
var nextPortBase = 10000;
var mainWindow = null;

function allocPorts() {
  var dc = nextPortBase;
  var pion = nextPortBase + 1;
  nextPortBase += 2;
  return { dc: dc, pion: pion };
}

function getTab(tabId) {
  if (!tabs.has(tabId)) {
    var ports = allocPorts();
    tabs.set(tabId, { relay: null, tunnelMode: 'dc', platform: 'vk', dcPort: ports.dc, pionPort: ports.pion });
  }
  return tabs.get(tabId);
}

function loadHook(url, tab) {
  var isTelemost = url.includes('telemost.yandex');
  var newPlatform = isTelemost ? 'telemost' : 'vk';
  if (newPlatform !== tab.platform && tab.tunnelMode.startsWith('pion')) {
    tab.platform = newPlatform;
    killRelay(tab);
    setTimeout(function() { startRelay(tab); }, 500);
  } else {
    tab.platform = newPlatform;
  }
  if (tab.tunnelMode === 'pion-video') {
    var hookFile = isTelemost ? 'video-telemost.js' : 'video-vk.js';
    var hook = fs.readFileSync(path.join(hooksDir, hookFile), 'utf8');
    return logCapture + 'window.PION_PORT=' + tab.pionPort + ';window.IS_CREATOR=true;' + hook;
  }
  var hookFile = isTelemost ? 'dc-creator-telemost.js' : 'dc-creator-vk.js';
  var hook = fs.readFileSync(path.join(hooksDir, hookFile), 'utf8');
  return logCapture + 'window.WS_PORT=' + tab.dcPort + ';' + hook;
}

function startRelay(tab) {
  killRelay(tab);
  var port = tab.tunnelMode.startsWith('pion') ? tab.pionPort : tab.dcPort;
  var relayMode = 'dc-creator';
  if (tab.tunnelMode === 'pion-video') {
    relayMode = tab.platform === 'telemost' ? 'telemost-video-creator' : 'vk-video-creator';
  }
  var proc = spawn(relayPath, ['--mode', relayMode, '--ws-port', String(port)], {
    stdio: ['ignore', 'pipe', 'pipe']
  });
  tab.relay = proc;
  var tabId = null;
  tabs.forEach(function(t, id) { if (t === tab) tabId = id; });
  var onData = function(data) {
    data.toString().trim().split('\n').forEach(function(msg) {
      if (!msg) return;
      console.log('[relay:' + tabId + ']', msg);
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send('relay-log', { tabId: tabId, msg: msg });
      }
    });
  };
  proc.stdout.on('data', onData);
  proc.stderr.on('data', onData);
  proc.on('close', function(code) {
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send('relay-log', { tabId: tabId, msg: 'Relay exited with code ' + code });
    }
  });
}

function killRelay(tab) {
  if (tab.relay) {
    tab.relay.kill();
    tab.relay = null;
  }
}

function killAllRelays() {
  tabs.forEach(function(tab) { killRelay(tab); });
}

function stripCSP(ses) {
  ses.webRequest.onHeadersReceived(function(details, callback) {
    var headers = Object.assign({}, details.responseHeaders);
    delete headers['content-security-policy'];
    delete headers['Content-Security-Policy'];
    delete headers['content-security-policy-report-only'];
    delete headers['Content-Security-Policy-Report-Only'];
    callback({ responseHeaders: headers });
  });
}

function createWindow() {
  var ses = session.fromPartition('persist:creator');
  stripCSP(ses);
  ses.setPermissionRequestHandler(function(wc, perm, cb) { cb(true); });
  ses.setPermissionCheckHandler(function() { return true; });
  ses.setUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36');
  app.on('session-created', stripCSP);

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

  mainWindow.loadFile('index.html');
  mainWindow.on('closed', function() { mainWindow = null; });

  var autoclickers = new Map(); // tabId -> { telemost, vk }

  mainWindow.webContents.on('did-attach-webview', function(e, wvContents) {
    wvContents.on('before-input-event', function(e, input) {
      if (input.key === 'F12') wvContents.openDevTools();
    });
    wvContents.on('did-navigate', function(e, url) {
      var tabId = wvContents.id;
      if (!autoclickers.has(tabId)) {
        autoclickers.set(tabId, { telemost: new TelemostAutoclick(), vk: new VkAutoclick() });
      }
      var ac = autoclickers.get(tabId);
      if (url.includes('telemost.yandex')) {
        ac.vk.stop();
        ac.telemost.attach(wvContents);
      } else if (url.includes('vk.com')) {
        ac.telemost.stop();
        ac.vk.attach(wvContents);
      } else {
        ac.telemost.stop();
        ac.vk.stop();
      }
    });
    wvContents.on('console-message', function(e, level, msg) {
      if (msg.indexOf('state: disconnected') !== -1 || msg.indexOf('state: failed') !== -1) {
        var ac = autoclickers.get(wvContents.id);
        if (ac) ac.vk.kickDisconnected();
      }
    });
    wvContents.on('destroyed', function() {
      var ac = autoclickers.get(wvContents.id);
      if (ac) { ac.telemost.stop(); ac.vk.stop(); autoclickers.delete(wvContents.id); }
    });
  });
}

ipcMain.handle('get-hook-code', function(e, tabId, url) {
  var tab = getTab(tabId);
  return loadHook(url, tab);
});

ipcMain.handle('set-tunnel-mode', function(e, tabId, mode) {
  var tab = tabs.get(tabId);
  if (!tab) return;
  if (['dc', 'pion-video'].indexOf(mode) === -1) return;
  tab.tunnelMode = mode;
  killRelay(tab);
  setTimeout(function() { startRelay(tab); }, 500);
});

ipcMain.handle('start-relay', function(e, tabId) {
  var tab = getTab(tabId);
  startRelay(tab);
});

ipcMain.handle('close-tab', function(e, tabId) {
  var tab = tabs.get(tabId);
  if (tab) {
    killRelay(tab);
    tabs.delete(tabId);
  }
});

app.whenReady().then(createWindow);

app.on('window-all-closed', function() { killAllRelays(); app.quit(); });
app.on('before-quit', killAllRelays);
process.on('exit', killAllRelays);
process.on('SIGINT', function() { killAllRelays(); process.exit(); });
process.on('SIGTERM', function() { killAllRelays(); process.exit(); });

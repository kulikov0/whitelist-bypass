const { app, BrowserWindow, session, ipcMain } = require('electron');
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const TelemostAutoclick = require('./telemost-autoclick');
const VkAutoclick = require('./vk-autoclick');
const BotManager = require('./bot-manager');

var hooksDir = app.isPackaged
  ? path.join(process.resourcesPath, 'hooks')
  : path.join(__dirname, '..', 'hooks');
var logCapture = "if(!window.__logCaptureInstalled){window.__logCaptureInstalled=true;window.__hookLogs=[];var _ol=console.log.bind(console);console.log=function(){_ol.apply(null,arguments);var m=Array.prototype.slice.call(arguments).join(' ');if(m.indexOf('[HOOK]')!==-1)window.__hookLogs.push(m)}}";
var relayPath = app.isPackaged
  ? path.join(process.resourcesPath, process.platform === 'win32' ? 'relay.exe' : 'relay')
  : path.join(__dirname, '..', 'relay', process.platform === 'win32' ? 'relay.exe' : 'relay');

var tabs = new Map(); // tabId -> { relay, tunnelMode, platform, dcPort, pionPort, isBot }
var callStatusCache = new Map();
var nextPortBase = 10000;
var mainWindow = null;
var botManager = null;
var botTabs = new Set();

function isPortFree(port) {
  return new Promise(function(resolve) {
    var server = require('net').createServer();
    server.once('error', function() { resolve(false); });
    server.once('listening', function() { server.close(function() { resolve(true); }); });
    server.listen(port, '127.0.0.1');
  });
}

async function allocPorts() {
  while (true) {
    var dc = nextPortBase;
    var pion = nextPortBase + 1;
    nextPortBase += 2;
    var dcFree = await isPortFree(dc);
    var pionFree = await isPortFree(pion);
    if (dcFree && pionFree) return { dc: dc, pion: pion };
  }
}

async function getTab(tabId) {
  if (!tabs.has(tabId)) {
    var ports = await allocPorts();
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
      if (msg.indexOf('[BOT] VKCalls: call link:') !== -1) {
        var link = msg.split('[BOT] VKCalls: call link:')[1].trim();
        console.log('[MAIN] VK call link captured:', link);

        var foundPeerId = null;
        tabs.forEach(function(t, id) {
          if (botTabs.has(id) && t.platform === 'vk') {
            foundPeerId = t.peerId;
          }
        });

        if (foundPeerId && botManager) {
          console.log('[MAIN] Sending VK link to peer:', foundPeerId);
          botManager.sendMessage(foundPeerId, 'Call created!\n ' + link);
        }
      }

      if (msg.indexOf('[BOT] Telemost: call link:') !== -1) {
        var link = msg.split('[BOT] Telemost: call link:')[1].trim();
        console.log('[MAIN] Telemost call link captured:', link);

        var foundPeerId = null;
        tabs.forEach(function(t, id) {
          if (botTabs.has(id) && t.platform === 'telemost') {
            foundPeerId = t.peerId;
          }
        });

        if (foundPeerId && botManager) {
          console.log('[MAIN] Sending Telemost link to peer:', foundPeerId);
          botManager.sendMessage(foundPeerId, 'Call created!\n ' + link);
        }
      }

      if (msg.indexOf('[CALL_STATUS]') !== -1) {
        console.log('[MAIN] Received call status:', msg);
        if (msg.indexOf(':') !== -1) {
          var parts = msg.split('[CALL_STATUS] ')[1];
          var colonIndex = parts.indexOf(':');
          if (colonIndex !== -1) {
            var tabId = parts.substring(0, colonIndex);
            var status = parts.substring(colonIndex + 1);
            callStatusCache.set(tabId, status);
            console.log('[MAIN] Cached status for', tabId, ':', status);
          }
        }
      }
    });
    wvContents.on('destroyed', function() {
      var ac = autoclickers.get(wvContents.id);
      if (ac) { ac.telemost.stop(); ac.vk.stop(); autoclickers.delete(wvContents.id); }
      callStatusCache.forEach(function(status, tabId) {
      });
    });
  });
}

ipcMain.handle('get-hook-code', async function(e, tabId, url) {
  var tab = await getTab(tabId);
  return loadHook(url, tab);
});

ipcMain.handle('get-call-creator-code', function(e, scriptFile) {
  var filePath = path.join(__dirname, scriptFile || 'vk-call-creator.js');
  return fs.readFileSync(filePath, 'utf8');
});

ipcMain.handle('set-tunnel-mode', function(e, tabId, mode) {
  var tab = tabs.get(tabId);
  if (!tab) return;
  if (['dc', 'pion-video'].indexOf(mode) === -1) return;
  tab.tunnelMode = mode;
  if (tab.relay) killRelay(tab);
  setTimeout(function() { startRelay(tab); }, 500);
});

ipcMain.handle('start-relay', async function(e, tabId) {
  var tab = await getTab(tabId);
  startRelay(tab);
});

ipcMain.handle('close-tab', function(e, tabId) {
  var tab = tabs.get(tabId);
  if (tab) {
    killRelay(tab);
    tabs.delete(tabId);
  }
  botTabs.delete(tabId);
});

// Bot IPC
ipcMain.handle('start-bot', function(e, settings) {
  if (botManager) {
    botManager.stop();
  }
  botManager = new BotManager(settings, async function(tabConfig) {
    if (!mainWindow || mainWindow.isDestroyed()) return;

    var tabId = 'bot-tab-' + Date.now();
    var ports = await allocPorts();
    tabs.set(tabId, { relay: null, tunnelMode: tabConfig.mode, platform: tabConfig.platform || 'vk', dcPort: ports.dc, pionPort: ports.pion, peerId: tabConfig.peerId, isBot: true });
    botTabs.add(tabId);

    mainWindow.webContents.send('create-bot-tab', { tabId: tabId, mode: tabConfig.mode, peerId: tabConfig.peerId, platform: tabConfig.platform || 'vk' });
    console.log('[BOT] Created tab:', tabId, 'mode:', tabConfig.mode, 'platform:', tabConfig.platform, 'peerId:', tabConfig.peerId);
  }, function() {
    var result = [];
    tabs.forEach(function(tab, tabId) {
      result.push({ 
        id: tabId, 
        platform: tab.platform, 
        mode: tab.tunnelMode, 
        isBot: tab.isBot === true,
        callStatus: callStatusCache.get(tabId) || 'inactive'
      });
    });
    return result;
  }, function(tabId) {
    var tab = tabs.get(tabId);
    if (tab) {
      killRelay(tab);
      tabs.delete(tabId);
      botTabs.delete(tabId);
      callStatusCache.delete(tabId);
      console.log('[BOT] Closed tab:', tabId);
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send('close-bot-tab', { tabId: tabId });
      }
    }
  });
  botManager.onError = function(msg) {
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send('bot-error', msg);
    }
  };
  botManager.start();
  return { success: true };
});

ipcMain.handle('stop-bot', function() {
  if (botManager) {
    botManager.stop();
    botManager = null;
  }
  return { success: true };
});

app.whenReady().then(createWindow);

app.on('window-all-closed', function() { killAllRelays(); app.quit(); });
app.on('before-quit', killAllRelays);
process.on('exit', killAllRelays);
process.on('SIGINT', function() { killAllRelays(); process.exit(); });
process.on('SIGTERM', function() { killAllRelays(); process.exit(); });

const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('bridge', {
  onRelayLog: function(cb) { ipcRenderer.on('relay-log', function(e, data) { cb(data.tabId, data.msg); }); },
  getHookCode: function(tabId, url) { return ipcRenderer.invoke('get-hook-code', tabId, url); },
  setTunnelMode: function(tabId, mode) { return ipcRenderer.invoke('set-tunnel-mode', tabId, mode); },
  startRelay: function(tabId) { return ipcRenderer.invoke('start-relay', tabId); },
  closeTab: function(tabId) { return ipcRenderer.invoke('close-tab', tabId); },
  startBot: function(settings) { return ipcRenderer.invoke('start-bot', settings); },
  stopBot: function() { return ipcRenderer.invoke('stop-bot'); },
  onCreateBotTab: function(cb) { ipcRenderer.on('create-bot-tab', function(e, data) { cb(data); }); },
  getCallCreatorCode: function(scriptFile) { return ipcRenderer.invoke('get-call-creator-code', scriptFile); },
  onBotError: function(cb) { ipcRenderer.on('bot-error', function(e, msg) { cb(msg); }); },
  getCookies: function(domain) { return ipcRenderer.invoke('get-cookies', domain); },
  onCloseBotTab: function(cb) { ipcRenderer.on('close-bot-tab', function(e, data) { cb(data); }); }
});

const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('bridge', {
  onRelayLog: function(cb) { ipcRenderer.on('relay-log', function(e, data) { cb(data.tabId, data.msg); }); },
  getHookCode: function(tabId, url) { return ipcRenderer.invoke('get-hook-code', tabId, url); },
  setTunnelMode: function(tabId, mode) { return ipcRenderer.invoke('set-tunnel-mode', tabId, mode); },
  startRelay: function(tabId) { return ipcRenderer.invoke('start-relay', tabId); },
  closeTab: function(tabId) { return ipcRenderer.invoke('close-tab', tabId); }
});

(() => {
  'use strict';
  if (window.__hookInstalled) return;
  window.__hookInstalled = true;

  const WS_URL = 'ws://127.0.0.1:' + (window.WS_PORT || 9000) + '/ws';
  const log = (...args) => console.log('[HOOK]', ...args);
  const peers = [];
  let activeWS = null;
  let activeDC = null;
  let dcOpen = false;
  let wsOpen = false;

  const OrigPC = window.RTCPeerConnection;
  window.RTCPeerConnection = function (config) {
    log('New PeerConnection created');
    const pc = new OrigPC(config);
    peers.push(pc);

    pc.addEventListener('connectionstatechange', () => {
      log('Connection state:', pc.connectionState);
      if (pc.connectionState === 'connected') {
        log('=== CALL CONNECTED ===');
        setupDC(pc);
      }
    });

    return pc;
  };

  Object.keys(OrigPC).forEach((key) => {
    window.RTCPeerConnection[key] = OrigPC[key];
  });
  window.RTCPeerConnection.prototype = OrigPC.prototype;

  function setupDC(pc) {
    const dc = pc.createDataChannel('tunnel', { negotiated: true, id: 2 });
    dc.binaryType = 'arraybuffer';
    bindDC(dc);
  }

  function bindDC(dc) {
    if (activeDC && activeDC !== dc && activeDC.readyState === 'open') {
      activeDC.close();
    }
    activeDC = dc;
    dcOpen = false;

    dc.onopen = () => {
      if (dc !== activeDC) return;
      log('DataChannel open');
      dcOpen = true;
      connectWS();
    };

    dc.onclose = () => {
      if (dc !== activeDC) return;
      log('DataChannel closed');
      dcOpen = false;
      activeDC = null;
    };

    dc.onmessage = (e) => {
      if (dc !== activeDC) return;
      if (activeWS && wsOpen) {
        activeWS.send(e.data);
      }
    };
  }

  function connectWS() {
    if (activeWS && activeWS.readyState === WebSocket.OPEN) {
      activeWS.close();
    }
    var ws = new WebSocket(WS_URL);
    ws.binaryType = 'arraybuffer';
    activeWS = ws;

    ws.onopen = () => {
      if (ws !== activeWS) return;
      log('WebSocket connected to Go relay');
      wsOpen = true;
    };

    ws.onclose = () => {
      if (ws !== activeWS) return;
      wsOpen = false;
      if (dcOpen) {
        log('WebSocket disconnected, reconnecting in 2s...');
        setTimeout(() => {
          if (dcOpen && ws === activeWS) connectWS();
        }, 2000);
      }
    };

    ws.onerror = () => {
      if (ws !== activeWS) return;
      log('WebSocket error');
    };

    ws.onmessage = (e) => {
      if (activeDC && dcOpen) {
        activeDC.send(e.data);
      }
    };
  }

  window.__hook = { peers: peers, log: log };
  log('Hook installed');
})();

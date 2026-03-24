(() => {
  'use strict';

  const WS_URL = 'ws://127.0.0.1:9000/ws';
  const log = (...args) => console.log('[HOOK]', ...args);
  const peers = [];
  let activeWS = null;
  let activeDC = null;
  let dcOpen = false;
  let wsOpen = false;
  let dcSetupDone = false; // prevent double-DC from multiple PeerConnections

  // Guard against double injection
  if (window.__hookInstalled) { log('Hook already installed, skipping'); return; }
  window.__hookInstalled = true;

  const OrigPC = window.RTCPeerConnection;
  window.RTCPeerConnection = function (config) {
    log('New PeerConnection created');
    const pc = new OrigPC(config);
    peers.push(pc);

    pc.addEventListener('connectionstatechange', () => {
      log('Connection state:', pc.connectionState);
      if (pc.connectionState === 'connected' && !dcSetupDone) {
        dcSetupDone = true;
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
    };

    let bwRecvBytes = 0;
    let bwRecvStart = 0;
    let bwMode = false;

    dc.onmessage = (e) => {
      if (dc !== activeDC) return;
      if (bwMode) {
        if (e.data instanceof ArrayBuffer) {
          if (bwRecvStart === 0) bwRecvStart = performance.now();
          bwRecvBytes += e.data.byteLength;
          return;
        }
        if (typeof e.data === 'string' && e.data === 'bw:done') {
          var elapsed = (performance.now() - bwRecvStart) / 1000;
          var kbps = (bwRecvBytes * 8 / 1024 / elapsed).toFixed(1);
          log('=== RECV COMPLETE: ' + (bwRecvBytes/1024).toFixed(1) + ' KB in ' + elapsed.toFixed(2) + 's = ' + kbps + ' kbps ===');
          bwRecvBytes = 0;
          bwRecvStart = 0;
          bwMode = false;
          return;
        }
      }
      if (typeof e.data === 'string' && e.data === 'bw:start') {
        bwMode = true;
        bwRecvBytes = 0;
        bwRecvStart = 0;
        return;
      }
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
      if (typeof AndroidBridge !== 'undefined' && AndroidBridge.onTunnelReady) {
        AndroidBridge.onTunnelReady();
      }
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
  window.__hook.runBandwidthTest = function(totalMB) {
    totalMB = totalMB || 1;
    if (!dcOpen || !activeDC) { log('DC not open'); return; }
    var chunkSize = 16384;
    var chunk = new ArrayBuffer(chunkSize);
    var totalBytes = totalMB * 1024 * 1024;
    var sent = 0;
    var start = performance.now();
    activeDC.send('bw:start');
    log('Starting bandwidth test: ' + totalMB + ' MB...');
    var sendBatch = function() {
      while (sent < totalBytes) {
        if (activeDC.bufferedAmount > 512 * 1024) {
          setTimeout(sendBatch, 5);
          return;
        }
        activeDC.send(chunk);
        sent += chunkSize;
      }
      activeDC.send('bw:done');
      var elapsed = (performance.now() - start) / 1000;
      var kbps = (totalBytes * 8 / 1024 / elapsed).toFixed(1);
      log('=== SEND COMPLETE: ' + (totalBytes/1024).toFixed(1) + ' KB in ' + elapsed.toFixed(2) + 's = ' + kbps + ' kbps ===');
    };
    sendBatch();
  };

  log('Hook installed');
})();

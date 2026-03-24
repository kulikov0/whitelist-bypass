(() => {
  'use strict';
  if (window.__hookInstalled) return;
  window.__hookInstalled = true;

  const WS_URL = 'ws://127.0.0.1:9000/ws';
  const log = (...args) => console.log('[HOOK]', ...args);
  const peers = [];
  let activeWS = null;
  let outboundDC = null;
  let dcCreating = false;
  let tunnelReady = false;
  let wsOpen = false;

  var signalingWS = null;
  var lastPcSeq = 0;
  var OrigWebSocket = window.WebSocket;
  window.WebSocket = function(url, protocols) {
    var ws = protocols ? new OrigWebSocket(url, protocols) : new OrigWebSocket(url);
    if (url && (url.indexOf('strm.yandex') !== -1 || url.indexOf('jvb.telemost') !== -1)) {
      log('Signaling WS found');
      signalingWS = ws;
      var origSend = ws.send.bind(ws);
      ws.send = function(data) {
        try {
          var msg = JSON.parse(data);
          if (msg.type === 'publisherSdpOffer' && msg.payload && msg.payload.pcSeq) {
            lastPcSeq = msg.payload.pcSeq;
          }
        } catch(e) {}
        return origSend(data);
      };
      ws.addEventListener('message', function(e) {
        try {
          var msg = JSON.parse(e.data);
          if (msg.type === 'publisherSdpAnswer' && msg.payload && msg.payload.sdp) {
            var pc0 = peers[0];
            if (pc0 && pc0.signalingState === 'have-local-offer') {
              pc0.setRemoteDescription({ type: 'answer', sdp: msg.payload.sdp }).catch(function(e) {
                log('setRemoteDescription error: ' + e.message);
              });
            }
          }
        } catch(e) {}
      });
    }
    return ws;
  };
  window.WebSocket.prototype = OrigWebSocket.prototype;
  window.WebSocket.CONNECTING = OrigWebSocket.CONNECTING;
  window.WebSocket.OPEN = OrigWebSocket.OPEN;
  window.WebSocket.CLOSING = OrigWebSocket.CLOSING;
  window.WebSocket.CLOSED = OrigWebSocket.CLOSED;

  const OrigPC = window.RTCPeerConnection;
  window.RTCPeerConnection = function (config) {
    log('New PeerConnection created');
    const pc = new OrigPC(config);
    peers.push(pc);
    var pcIdx = peers.length - 1;

    pc.addEventListener('connectionstatechange', () => {
      log('Connection state:', pc.connectionState);
      if (pc.connectionState === 'connected') {
        log('=== CALL CONNECTED on PC' + pcIdx + ' ===');
        pc.getSenders().forEach(function(s) {
          if (s.track) s.track.stop();
          s.replaceTrack(null).catch(function(){});
        });
        if (!outboundDC && !dcCreating) {
          createTunnelDC(pc, origCreateDC);
        }
      }
    });

    var origCreateDC = pc.createDataChannel.bind(pc);
    pc.createDataChannel = function(label, opts) {
      return origCreateDC(label, opts);
    };

    pc.addEventListener('datachannel', function(e) {
      var ch = e.channel;
      ch.binaryType = 'arraybuffer';
      log('Incoming DC: label=' + ch.label + ' id=' + ch.id + ' on PC' + pcIdx);

      if (ch.label === 'sharing') {
        log('Inbound tunnel DC found on PC' + pcIdx);
        ch.addEventListener('message', function(m) {
          if (typeof m.data === 'string') {
            if (m.data === 'tunnel:ping') {
              sendRaw('tunnel:pong');
              if (!tunnelReady) {
                tunnelReady = true;
                log('DataChannel confirmed');
                connectWS();
              }
              return;
            }
          }
          if (m.data instanceof ArrayBuffer) {
            handleChunk(m.data);
          }
        });
      }
    });

    return pc;
  };

  function createTunnelDC(pc, origCreateDC) {
    dcCreating = true;
    setTimeout(function() {
      log('Creating tunnel DC');
      var dc = origCreateDC('sharing', { ordered: true });
      dc.binaryType = 'arraybuffer';
      outboundDC = dc;
      dc.addEventListener('open', function() {
        log('DataChannel open');
      });
      dc.addEventListener('close', function() {
        log('DataChannel closed');
        outboundDC = null;
        dcCreating = false;
        dcQueue = [];
        tunnelReady = false;
      });
      pc.createOffer().then(function(offer) {
        return pc.setLocalDescription(offer).then(function() {
          if (signalingWS && signalingWS.readyState === 1) {
            signalingWS.send(JSON.stringify({
              type: 'publisherSdpOffer',
              payload: { pcSeq: lastPcSeq, sdp: offer.sdp, tracks: [] }
            }));
            log('Sent renegotiation offer via signaling WS');
          }
        });
      }).catch(function(e) {
        log('Renegotiation error: ' + e.message);
      });
    }, 3000);
  }

  Object.keys(OrigPC).forEach((key) => {
    window.RTCPeerConnection[key] = OrigPC[key];
  });
  window.RTCPeerConnection.prototype = OrigPC.prototype;

  var CHUNK = 994;
  var dcQueue = [];
  var dcDraining = false;
  var sendMsgId = 0;
  var recvBufs = {};

  function sendRaw(data) {
    if (!outboundDC || outboundDC.readyState !== 'open') return;
    if (typeof data === 'string') {
      dcQueue.push(data);
      drainDC();
      return;
    }
    var u8 = new Uint8Array(data instanceof ArrayBuffer ? data : data.buffer || data);
    var total = Math.ceil(u8.length / CHUNK) || 1;
    var id = (sendMsgId++) & 0xFFFF;
    for (var i = 0; i < total; i++) {
      var p = u8.subarray(i * CHUNK, Math.min((i + 1) * CHUNK, u8.length));
      var f = new Uint8Array(6 + p.length);
      f[0] = id >> 8; f[1] = id & 0xFF;
      f[2] = i >> 8; f[3] = i & 0xFF;
      f[4] = total >> 8; f[5] = total & 0xFF;
      f.set(p, 6);
      dcQueue.push(f.buffer);
    }
    drainDC();
  }

  function handleChunk(data) {
    var u8 = new Uint8Array(data);
    if (u8.length < 6) return;
    var id = (u8[0] << 8) | u8[1];
    var idx = (u8[2] << 8) | u8[3];
    var total = (u8[4] << 8) | u8[5];
    var payload = u8.subarray(6);
    if (total === 1) {
      if (activeWS && wsOpen) activeWS.send(payload.buffer.slice(payload.byteOffset, payload.byteOffset + payload.byteLength));
      return;
    }
    var r = recvBufs[id];
    if (!r) { r = { c: [], n: 0, s: 0 }; recvBufs[id] = r; }
    if (!r.c[idx]) { r.c[idx] = payload; r.n++; r.s += payload.length; }
    if (r.n === total) {
      var out = new Uint8Array(r.s);
      for (var i = 0, o = 0; i < total; i++) { out.set(r.c[i], o); o += r.c[i].length; }
      delete recvBufs[id];
      if (activeWS && wsOpen) activeWS.send(out.buffer);
    }
  }

  function drainDC() {
    if (dcDraining) return;
    dcDraining = true;
    while (dcQueue.length > 0) {
      if (outboundDC.bufferedAmount > 64 * 1024) {
        outboundDC.bufferedAmountLowThreshold = 16 * 1024;
        outboundDC.onbufferedamountlow = function() {
          outboundDC.onbufferedamountlow = null;
          dcDraining = false;
          drainDC();
        };
        return;
      }
      outboundDC.send(dcQueue.shift());
    }
    dcDraining = false;
  }

  function connectWS() {
    var ws = new WebSocket(WS_URL);
    ws.binaryType = 'arraybuffer';
    activeWS = ws;
    ws.onopen = function() {
      log('WebSocket connected to Go relay');
      wsOpen = true;
    };
    ws.onclose = function() {
      wsOpen = false;
      if (tunnelReady) {
        log('WebSocket disconnected, reconnecting in 2s...');
        setTimeout(function() { if (tunnelReady) connectWS(); }, 2000);
      }
    };
    ws.onerror = function() { log('WebSocket error'); };
    ws.onmessage = function(e) {
      sendRaw(e.data);
    };
  }

  window.__hook = { peers: peers, log: log };
  window.__hook.runBandwidthTest = function(totalMB) {
    totalMB = totalMB || 1;
    if (!tunnelReady || !outboundDC) { log('Tunnel not ready'); return; }
    var chunkSize = 4096;
    var totalBytes = totalMB * 1024 * 1024;
    var sent = 0;
    var start = performance.now();
    sendRaw('bw:start');
    log('Starting bandwidth test: ' + totalMB + ' MB...');
    var sendBatch = function() {
      while (sent < totalBytes) {
        if (outboundDC.bufferedAmount > 512 * 1024) {
          setTimeout(sendBatch, 5);
          return;
        }
        sendRaw(new ArrayBuffer(chunkSize));
        sent += chunkSize;
      }
      sendRaw('bw:done');
      var elapsed = (performance.now() - start) / 1000;
      var kbps = (totalBytes * 8 / 1024 / elapsed).toFixed(1);
      log('=== SEND COMPLETE: ' + (totalBytes/1024).toFixed(1) + ' KB in ' + elapsed.toFixed(2) + 's = ' + kbps + ' kbps ===');
    };
    sendBatch();
  };

  log('Hook installed');
})();

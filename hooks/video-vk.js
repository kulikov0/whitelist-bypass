(() => {
  'use strict';
  if (window.__hookInstalled) return;
  window.__hookInstalled = true;

  var PION_WS_URL = 'ws://127.0.0.1:' + (window.PION_PORT || 9001) + '/signaling';
  var log = function() {
    var args = ['[HOOK]'].concat(Array.prototype.slice.call(arguments));
    console.log.apply(console, args);
  };

  var reIp4 = /\d+\.\d+\.\d+\.\d+/g;
  var reIp6 = /[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){2,7}|(?:[0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|::(?:[0-9a-fA-F]{1,4}:){0,5}[0-9a-fA-F]{1,4}/g;
  function maskAddr(s) {
    if (!s) return '';
    reIp4.lastIndex = 0;
    reIp6.lastIndex = 0;
    return s.replace(reIp4, function(ip) {
      var p = ip.split('.'); return p[0] + '.' + p[1] + '.x.x';
    }).replace(reIp6, 'x::x');
  }

  var OrigWebSocket = window.WebSocket;
  var pionWS = null;
  var pionReady = false;
  var pendingMessages = [];
  var requestId = 0;
  var pendingRequests = {};

  function connectPion() {
    var ws = new OrigWebSocket(PION_WS_URL);
    pionWS = ws;
    ws.onopen = function() {
      log('Connected to Pion relay');
      pionReady = true;
      pendingMessages.forEach(function(m) { ws.send(m); });
      pendingMessages = [];
    };
    ws.onclose = function() {
      pionReady = false;
      pionWS = null;
      if (mockPC && mockPC._connectionState !== 'closed') {
        log('Pion relay disconnected, reconnecting...');
        setTimeout(connectPion, 2000);
      }
    };
    ws.onerror = function() {};
    ws.onmessage = function(e) {
      var msg = JSON.parse(e.data);
      handlePionMessage(msg);
    };
  }

  function sendToPion(type, data) {
    var msg = JSON.stringify({ type: type, data: data });
    if (pionReady && pionWS && pionWS.readyState === 1) {
      pionWS.send(msg);
    } else {
      pendingMessages.push(msg);
    }
  }

  function requestPion(type, data) {
    var id = ++requestId;
    return new Promise(function(resolve, reject) {
      pendingRequests[id] = { resolve: resolve, reject: reject };
      var msg = JSON.stringify({ type: type, data: data, id: id });
      if (pionReady && pionWS && pionWS.readyState === 1) {
        pionWS.send(msg);
      } else {
        pendingMessages.push(msg);
      }
      setTimeout(function() {
        if (pendingRequests[id]) {
          delete pendingRequests[id];
          reject(new Error('Pion request timeout: ' + type));
        }
      }, 10000);
    });
  }

  var mockPC = null;
  function handlePionMessage(msg) {
    if (msg.id && pendingRequests[msg.id]) {
      pendingRequests[msg.id].resolve(msg.data);
      delete pendingRequests[msg.id];
      return;
    }

    switch (msg.type) {
      case 'ice-candidate':
        if (mockPC && mockPC._onicecandidate) {
          mockPC._onicecandidate({ candidate: new RTCIceCandidate(msg.data) });
        }
        break;
      case 'remote-track':
        log('remote-track received: kind=' + (msg.data && msg.data.kind) + ' mockPC=' + !!mockPC + ' ontrack=' + !!( mockPC && mockPC._ontrack));
        if (mockPC && mockPC._ontrack) {
          var kind = msg.data.kind;
          log('Firing ontrack for remote ' + kind);
          var canvas = document.createElement('canvas');
          canvas.width = 2; canvas.height = 2;
          var fakeStream = canvas.captureStream(1);
          var fakeTrack = kind === 'audio'
            ? (function() { var a = new AudioContext(); var d = a.createMediaStreamDestination(); return d.stream.getAudioTracks()[0]; })()
            : fakeStream.getVideoTracks()[0];
          log('Fake track created: kind=' + fakeTrack.kind + ' readyState=' + fakeTrack.readyState + ' enabled=' + fakeTrack.enabled);
          var evt = new Event('track');
          evt.track = fakeTrack;
          evt.receiver = { track: fakeTrack };
          evt.transceiver = { receiver: { track: fakeTrack } };
          evt.streams = [kind === 'audio' ? new MediaStream([fakeTrack]) : fakeStream];
          log('Calling mockPC._ontrack with streams[0].getTracks()=' + evt.streams[0].getTracks().length);
          mockPC._ontrack(evt);
          log('ontrack callback completed');
        } else {
          log('WARNING: remote-track but no mockPC or no ontrack handler!');
        }
        break;
      case 'connection-state':
        if (mockPC) {
          log('Pion connection state:', msg.data);
          if (window.IS_CREATOR && (msg.data === 'closed' || msg.data === 'failed')) break;
          mockPC._connectionState = msg.data;
          if (msg.data === 'connected') {
            if (typeof AndroidBridge !== 'undefined' && AndroidBridge.onTunnelReady) {
              AndroidBridge.onTunnelReady();
            }
          }
          if (mockPC._onconnectionstatechange) {
            mockPC._onconnectionstatechange(new Event('connectionstatechange'));
          }
          mockPC._listeners.connectionstatechange.forEach(function(fn) {
            fn(new Event('connectionstatechange'));
          });
        }
        break;
    }
  }

  function MockPeerConnection(config) {
    this._config = config;
    this._connectionState = 'new';
    this._signalingState = 'stable';
    this._iceConnectionState = 'new';
    this._iceGatheringState = 'new';
    this._localDescription = null;
    this._remoteDescription = null;
    this._senders = [];
    this._receivers = [];
    this._onicecandidate = null;
    this._onconnectionstatechange = null;
    this._ontrack = null;
    this._ondatachannel = null;
    this._listeners = {
      connectionstatechange: [],
      icecandidate: [],
      track: [],
      datachannel: [],
      iceconnectionstatechange: [],
      icegatheringstatechange: [],
      signalingstatechange: [],
      negotiationneeded: []
    };

    if (config && config.iceServers) {
      var servers = config.iceServers.map(function(s) {
        return {
          urls: Array.isArray(s.urls) ? s.urls : (s.urls ? [s.urls] : []),
          username: s.username || '',
          credential: s.credential || ''
        };
      });
      sendToPion('ice-servers', servers);
    }

    mockPC = this;
    if (!pionWS) connectPion();
    log('MockPC created');
  }

  MockPeerConnection.prototype = {
    get connectionState() { return this._connectionState; },
    get signalingState() { return this._signalingState; },
    get iceConnectionState() { return this._iceConnectionState; },
    get iceGatheringState() { return this._iceGatheringState; },
    get localDescription() { return this._localDescription; },
    get remoteDescription() { return this._remoteDescription; },
    set onicecandidate(fn) { this._onicecandidate = fn; },
    get onicecandidate() { return this._onicecandidate; },
    set onconnectionstatechange(fn) { this._onconnectionstatechange = fn; },
    get onconnectionstatechange() { return this._onconnectionstatechange; },
    set ontrack(fn) { log('MockPC.ontrack SET by caller'); this._ontrack = fn; },
    get ontrack() { return this._ontrack; },
    set ondatachannel(fn) { this._ondatachannel = fn; },
    get ondatachannel() { return this._ondatachannel; },
    set oniceconnectionstatechange(fn) { this._oniceconnectionstatechange = fn; },
    get oniceconnectionstatechange() { return this._oniceconnectionstatechange; },
    set onicegatheringstatechange(fn) { this._onicegatheringstatechange = fn; },
    get onicegatheringstatechange() { return this._onicegatheringstatechange; },
    set onsignalingstatechange(fn) { this._onsignalingstatechange = fn; },
    get onsignalingstatechange() { return this._onsignalingstatechange; },
    set onnegotiationneeded(fn) { this._onnegotiationneeded = fn; },
    get onnegotiationneeded() { return this._onnegotiationneeded; },
    addEventListener: function(type, fn) {
      if (this._listeners[type]) this._listeners[type].push(fn);
    },
    removeEventListener: function(type, fn) {
      if (this._listeners[type]) this._listeners[type] = this._listeners[type].filter(function(f) { return f !== fn; });
    },
    dispatchEvent: function(event) {
      var type = event.type;
      if (this._listeners[type]) this._listeners[type].forEach(function(fn) { fn(event); });
      var handler = this['on' + type];
      if (handler) handler(event);
    },
    createOffer: function(options) {
      log('MockPC.createOffer');
      return requestPion('create-offer', options || {}).then(function(sdp) {
        log('MockPC.createOffer resolved');
        return new RTCSessionDescription(sdp);
      });
    },
    createAnswer: function(options) {
      log('MockPC.createAnswer');
      return requestPion('create-answer', options || {}).then(function(sdp) {
        log('MockPC.createAnswer resolved');
        return new RTCSessionDescription(sdp);
      });
    },
    setLocalDescription: function(desc) {
      this._localDescription = desc;
      var oldState = this._signalingState;
      this._signalingState = desc.type === 'offer' ? 'have-local-offer' : 'stable';
      log('MockPC.setLocalDescription:', desc.type);
      sendToPion('set-local-description', { type: desc.type, sdp: desc.sdp });
      var self = this;
      if (oldState !== this._signalingState) {
        setTimeout(function() {
          if (self._onsignalingstatechange) self._onsignalingstatechange(new Event('signalingstatechange'));
          self._listeners.signalingstatechange.forEach(function(fn) { fn(new Event('signalingstatechange')); });
        }, 0);
      }
      return Promise.resolve();
    },
    setRemoteDescription: function(desc) {
      this._remoteDescription = desc;
      var oldState = this._signalingState;
      this._signalingState = desc.type === 'offer' ? 'have-remote-offer' : 'stable';
      log('MockPC.setRemoteDescription:', desc.type);
      var self = this;
      return requestPion('set-remote-description', { type: desc.type, sdp: desc.sdp }).then(function() {
        if (oldState !== self._signalingState) {
          if (self._onsignalingstatechange) self._onsignalingstatechange(new Event('signalingstatechange'));
          self._listeners.signalingstatechange.forEach(function(fn) { fn(new Event('signalingstatechange')); });
        }
      });
    },
    addIceCandidate: function(candidate) {
      if (candidate && candidate.candidate) {
        sendToPion('add-ice-candidate', { candidate: candidate.candidate, sdpMid: candidate.sdpMid, sdpMLineIndex: candidate.sdpMLineIndex });
      }
      return Promise.resolve();
    },
    addTrack: function(track, stream) {
      var sender = { track: track, replaceTrack: function(t) { this.track = t; return Promise.resolve(); }, getParameters: function() { return { encodings: [{}] }; }, setParameters: function() { return Promise.resolve(); } };
      this._senders.push(sender);
      sendToPion('add-track', { kind: track.kind });
      return sender;
    },
    addTransceiver: function(trackOrKind, init) {
      var kind = typeof trackOrKind === 'string' ? trackOrKind : trackOrKind.kind;
      var sender = { track: typeof trackOrKind === 'string' ? null : trackOrKind, replaceTrack: function(t) { this.track = t; return Promise.resolve(); }, getParameters: function() { return { encodings: [{}] }; }, setParameters: function() { return Promise.resolve(); } };
      var receiver = { track: { kind: kind, readyState: 'live', enabled: true, muted: true, id: 'mock-' + kind, addEventListener: function() {}, removeEventListener: function() {} } };
      var transceiver = { sender: sender, receiver: receiver, direction: (init && init.direction) || 'sendrecv', setDirection: function(d) { this.direction = d; }, mid: null, stopped: false, stop: function() { this.stopped = true; } };
      this._senders.push(sender);
      this._receivers.push(receiver);
      return transceiver;
    },
    removeTrack: function(sender) {
      this._senders = this._senders.filter(function(s) { return s !== sender; });
    },
    getSenders: function() { return this._senders; },
    getReceivers: function() { return this._receivers; },
    getTransceivers: function() { return []; },
    getStats: function() { return Promise.resolve(new Map()); },
    getConfiguration: function() { return this._config || {}; },
    createDataChannel: function(label, opts) {
      log('MockPC.createDataChannel:', label);
      sendToPion('create-data-channel', { label: label, opts: opts });
      var dc = {
        label: label,
        id: opts && opts.id != null ? opts.id : null,
        readyState: 'connecting',
        binaryType: 'arraybuffer',
        bufferedAmount: 0,
        onopen: null, onclose: null, onmessage: null, onerror: null,
        send: function() {},
        close: function() { this.readyState = 'closed'; },
        addEventListener: function() {},
        removeEventListener: function() {}
      };
      return dc;
    },
    close: function() {
      if (window.IS_CREATOR) {
        sendToPion('close', {});
        log('MockPC.close (pion reset, state kept for VK)');
        return;
      }
      this._connectionState = 'closed';
      this._signalingState = 'closed';
      sendToPion('close', {});
      log('MockPC.close');
    },
    restartIce: function() {}
  };

  var OrigPC = window.RTCPeerConnection;
  window.RTCPeerConnection = function(config) {
    log('RTCPeerConnection intercepted -> MockPC');
    return new MockPeerConnection(config);
  };
  Object.keys(OrigPC).forEach(function(key) {
    window.RTCPeerConnection[key] = OrigPC[key];
  });
  window.RTCPeerConnection.prototype = OrigPC.prototype;
  window.RTCPeerConnection.generateCertificate = OrigPC.generateCertificate;

  var origStringify = JSON.stringify;
  JSON.stringify = function(obj) {
    if (obj && obj.command && typeof obj.sequence === 'number') {
      if (obj.command === 'change-media-settings' && obj.mediaSettings) {
        obj.mediaSettings.isVideoEnabled = true;
        log('Forced isVideoEnabled=true in change-media-settings');
      }
      try { handleOutgoing(obj); } catch(e) {}
    }
    var result = origStringify.apply(JSON, arguments);
    return result;
  };

  var origParse = JSON.parse;
  JSON.parse = function(str) {
    var result = origParse.apply(JSON, arguments);
    if (result && (result.type === 'notification' || result.type === 'response')) {
      try { handleIncoming(result); } catch(e) {}
    }
    return result;
  };

  function handleOutgoing(msg) {
    if (msg.command === 'transmit-data') {
      if (msg.data && msg.data.sdp) {
        log('VK sending SDP:', msg.data.sdp.type);
      }
      if (msg.data && msg.data.candidate) {
        log('VK sending ICE candidate');
      }
    }
    if (msg.command === 'change-media-settings') {
      log('VK outgoing change-media-settings:', JSON.stringify(msg.mediaSettings));
    }
    if (msg.command === 'join-conversation' || msg.command === 'start-conversation') {
      log('VK outgoing ' + msg.command + ': mediaSettings=' + JSON.stringify(msg.mediaSettings));
    }
  }

  function handleIncoming(msg) {
    if (msg.type === 'notification') {
      if (msg.notification === 'connection' && msg.conversationParams && msg.conversationParams.turn) {
        var turn = msg.conversationParams.turn;
        log('TURN servers:', turn.urls.length);
        sendToPion('ice-servers', [{
          urls: turn.urls,
          username: turn.username,
          credential: turn.credential
        }]);
      }
      if (msg.notification === 'transmitted-data' && msg.data) {
        if (msg.data.candidate) {
          sendToPion('remote-ice-candidate', msg.data.candidate);
        }
        if (msg.data.sdp) {
          log('VK incoming SDP: type=' + msg.data.sdp.type);
        }
      }
      if (msg.notification === 'remote-media-settings' || msg.notification === 'media-settings' || msg.notification === 'media-settings-changed') {
        log('VK media-settings notification:', maskAddr(JSON.stringify(msg).substring(0, 300)));
      }
      if (msg.notification === 'participant-added' || msg.notification === 'participant-removed') {
        log('VK participant event: ' + msg.notification);
      }
    }
    if (msg.type === 'response') {
      if (msg.data && msg.data.mediaSettings) {
        log('VK response mediaSettings: isVideoEnabled=' + msg.data.mediaSettings.isVideoEnabled);
      }
    }
  }

  window.__hook = { log: log };
  log('Signaling proxy hook installed (MockPC mode)');
})();

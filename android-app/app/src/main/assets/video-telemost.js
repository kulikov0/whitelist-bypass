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
  var mockPCs = [];
  var pcCount = 0;

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
      var hasActive = mockPCs.some(function(pc) { return pc._connectionState !== 'closed'; });
      if (hasActive) {
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

  function sendToPion(type, data, role) {
    var msg = JSON.stringify({ type: type, data: data, role: role || '' });
    if (pionReady && pionWS && pionWS.readyState === 1) {
      pionWS.send(msg);
    } else {
      pendingMessages.push(msg);
    }
  }

  function requestPion(type, data, role) {
    var id = ++requestId;
    return new Promise(function(resolve, reject) {
      pendingRequests[id] = { resolve: resolve, reject: reject };
      var msg = JSON.stringify({ type: type, data: data, id: id, role: role || '' });
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

  function handlePionMessage(msg) {
    if (msg.id && pendingRequests[msg.id]) {
      pendingRequests[msg.id].resolve(msg.data);
      delete pendingRequests[msg.id];
      return;
    }

    switch (msg.type) {
      case 'ice-candidate':
        var targetRole = msg.role || '';
        mockPCs.forEach(function(pc) {
          if (pc._connectionState === 'closed') return;
          if (targetRole && pc._role !== targetRole) return;
          var evt = { candidate: new RTCIceCandidate(msg.data) };
          if (pc._onicecandidate) pc._onicecandidate(evt);
          pc._listeners.icecandidate.forEach(function(fn) { fn(evt); });
        });
        break;
      case 'remote-track':
        log('remote-track received: kind=' + (msg.data && msg.data.kind));
        break;
      case 'connection-state':
        log('Pion connection state:', msg.data);
        mockPCs.forEach(function(pc) {
          pc._connectionState = msg.data;
          if (msg.data === 'connected') {
            if (typeof AndroidBridge !== 'undefined' && AndroidBridge.onTunnelReady) {
              AndroidBridge.onTunnelReady();
            }
          }
          if (pc._onconnectionstatechange) pc._onconnectionstatechange(new Event('connectionstatechange'));
          pc._listeners.connectionstatechange.forEach(function(fn) { fn(new Event('connectionstatechange')); });
        });
        break;
    }
  }

  // MockPC for Telemost GOLOOM SDK
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
    this._transceivers = [];
    this._onicecandidate = null;
    this._onconnectionstatechange = null;
    this._ontrack = null;
    this._ondatachannel = null;
    this._onsignalingstatechange = null;
    this._onnegotiationneeded = null;
    this._oniceconnectionstatechange = null;
    this._pcIdx = pcCount++;
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

    var existingPub = mockPCs.find(function(pc) { return pc._role === 'pub' && pc._connectionState !== 'closed'; });
    this._role = existingPub ? 'sub' : 'pub';

    if (config && config.iceServers) {
      var servers = config.iceServers.map(function(s) {
        var urls = Array.isArray(s.urls) ? s.urls : (s.urls ? [s.urls] : []);
        if (typeof AndroidBridge !== 'undefined' && AndroidBridge.resolveHost) {
          urls = urls.map(function(url) {
            var match = url.match(/^(turn:|stun:)([^:?]+)(.*)/);
            if (match) {
              var resolved = AndroidBridge.resolveHost(match[2]);
              if (resolved && resolved.indexOf(':') !== -1) resolved = '[' + resolved + ']';
              if (resolved) return match[1] + resolved + match[3];
            }
            return url;
          });
        }
        return {
          urls: urls,
          username: s.username || '',
          credential: s.credential || ''
        };
      });
      sendToPion('ice-servers', servers, this._role);
    }

    mockPCs.push(this);
    if (!pionWS) {
      if (typeof AndroidBridge !== 'undefined' && AndroidBridge.getLocalIP) {
        var ip = AndroidBridge.getLocalIP();
        if (ip) {
          log('Local IP from Android: ' + maskAddr(ip));
          pendingMessages.push(JSON.stringify({ type: 'local-ip', data: ip }));
        }
      }
      connectPion();
    }
    log('MockPC #' + this._pcIdx + ' created (role=' + this._role + ')');
  }

  MockPeerConnection.prototype = {
    get connectionState() { return this._connectionState; },
    get signalingState() { return this._signalingState; },
    get iceConnectionState() { return this._iceConnectionState; },
    get iceGatheringState() { return this._iceGatheringState; },
    get localDescription() { return this._localDescription; },
    get remoteDescription() { return this._remoteDescription; },
    get currentLocalDescription() { return this._localDescription; },
    get currentRemoteDescription() { return this._remoteDescription; },

    set onicecandidate(fn) { this._onicecandidate = fn; },
    get onicecandidate() { return this._onicecandidate; },
    set onconnectionstatechange(fn) { this._onconnectionstatechange = fn; },
    get onconnectionstatechange() { return this._onconnectionstatechange; },
    set ontrack(fn) { log('MockPC #' + this._pcIdx + ' ontrack SET (role=' + this._role + ')'); this._ontrack = fn; },
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
      if (handler) handler.call(this, event);
    },

    createOffer: function(options) {
      log('MockPC #' + this._pcIdx + ' createOffer');
      return requestPion('create-offer', options || {}, this._role).then(function(sdp) {
        return new RTCSessionDescription(sdp);
      });
    },

    createAnswer: function(options) {
      log('MockPC #' + this._pcIdx + ' createAnswer');
      return requestPion('create-answer', options || {}, this._role).then(function(sdp) {
        return new RTCSessionDescription(sdp);
      });
    },

    setLocalDescription: function(desc) {
      this._localDescription = desc;
      var oldState = this._signalingState;
      this._signalingState = desc.type === 'offer' ? 'have-local-offer' : 'stable';
      log('MockPC #' + this._pcIdx + ' setLocalDescription:', desc.type);
      sendToPion('set-local-description', { type: desc.type, sdp: desc.sdp }, this._role);
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
      var oldState = this._signalingState;
      this._signalingState = desc.type === 'offer' ? 'have-remote-offer' : 'stable';
      this._remoteDescription = desc;
      log('MockPC #' + this._pcIdx + ' setRemoteDescription:', desc.type);
      var self = this;
      return requestPion('set-remote-description', { type: desc.type, sdp: desc.sdp }, this._role).then(function() {
        if (oldState !== self._signalingState) {
          if (self._onsignalingstatechange) self._onsignalingstatechange(new Event('signalingstatechange'));
          self._listeners.signalingstatechange.forEach(function(fn) { fn(new Event('signalingstatechange')); });
        }
      });
    },

    addIceCandidate: function(candidate) {
      if (candidate && candidate.candidate) {
        sendToPion('add-ice-candidate', {
          candidate: candidate.candidate,
          sdpMid: candidate.sdpMid,
          sdpMLineIndex: candidate.sdpMLineIndex
        }, this._role);
      }
      return Promise.resolve();
    },

    addTrack: function(track, stream) {
      var sender = {
        track: track,
        replaceTrack: function(t) { this.track = t; return Promise.resolve(); },
        getParameters: function() { return { encodings: [{}] }; },
        setParameters: function() { return Promise.resolve(); }
      };
      this._senders.push(sender);
      sendToPion('add-track', { kind: track.kind }, this._role);
      return sender;
    },

    addTransceiver: function(trackOrKind, init) {
      var kind = typeof trackOrKind === 'string' ? trackOrKind : trackOrKind.kind;
      var sender = {
        track: typeof trackOrKind === 'string' ? null : trackOrKind,
        replaceTrack: function(t) { this.track = t; return Promise.resolve(); },
        getParameters: function() { return { encodings: [{}] }; },
        setParameters: function() { return Promise.resolve(); },
        setStreams: function() {}
      };
      var receiver = {
        track: { kind: kind, readyState: 'live', enabled: true, muted: true, id: 'mock-' + kind + '-' + this._pcIdx,
          addEventListener: function() {}, removeEventListener: function() {},
          getSettings: function() { return {}; } },
        getStats: function() { return Promise.resolve(new Map()); },
        getSynchronizationSources: function() { return []; }
      };
      var transceiver = {
        sender: sender, receiver: receiver,
        direction: (init && init.direction) || 'sendrecv',
        setDirection: function(d) { this.direction = d; },
        mid: String(this._transceivers.length),
        stopped: false,
        stop: function() { this.stopped = true; },
        setCodecPreferences: function() {}
      };
      this._senders.push(sender);
      this._receivers.push(receiver);
      this._transceivers.push(transceiver);
      return transceiver;
    },

    removeTrack: function(sender) {
      this._senders = this._senders.filter(function(s) { return s !== sender; });
    },

    getSenders: function() { return this._senders; },
    getReceivers: function() { return this._receivers; },
    getTransceivers: function() { return this._transceivers; },
    getStats: function() { return Promise.resolve(new Map()); },
    getConfiguration: function() { return this._config || {}; },

    createDataChannel: function(label, opts) {
      log('MockPC #' + this._pcIdx + ' createDataChannel:', label);
      sendToPion('create-data-channel', { label: label, opts: opts }, this._role);
      var dc = {
        label: label,
        id: opts && opts.id != null ? opts.id : null,
        readyState: 'connecting',
        binaryType: 'arraybuffer',
        bufferedAmount: 0,
        bufferedAmountLowThreshold: 0,
        onopen: null, onclose: null, onmessage: null, onerror: null,
        onbufferedamountlow: null,
        send: function() {},
        close: function() { this.readyState = 'closed'; },
        addEventListener: function() {},
        removeEventListener: function() {}
      };
      return dc;
    },

    close: function() {
      this._connectionState = 'closed';
      this._signalingState = 'closed';
      log('MockPC #' + this._pcIdx + ' close');
      var allClosed = mockPCs.every(function(pc) { return pc._connectionState === 'closed'; });
      if (allClosed) {
        sendToPion('close', {});
        log('All MockPCs closed, closing Pion');
      }
    },

    restartIce: function() {},
    setConfiguration: function() {}
  };

  // Hook RTCPeerConnection
  var OrigPC = window.RTCPeerConnection;
  window.RTCPeerConnection = function(config) {
    if (!config || !config.iceServers || config.iceServers.length === 0) {
      log('RTCPeerConnection probe (no ICE servers) -> real PC');
      return new OrigPC(config);
    }
    log('RTCPeerConnection intercepted -> MockPC');
    return new MockPeerConnection(config);
  };
  Object.keys(OrigPC).forEach(function(key) {
    window.RTCPeerConnection[key] = OrigPC[key];
  });
  window.RTCPeerConnection.prototype = OrigPC.prototype;
  window.RTCPeerConnection.generateCertificate = OrigPC.generateCertificate;

  // Intercept signaling via JSON.stringify/parse
  var origStringify = JSON.stringify;
  JSON.stringify = function(obj) {
    var result = origStringify.apply(JSON, arguments);
    return result;
  };

  var origParse = JSON.parse;
  JSON.parse = function(str) {
    var result = origParse.apply(JSON, arguments);
    if (result && result.serverHello && result.serverHello.rtcConfiguration) {
      var rtcConfig = result.serverHello.rtcConfiguration;
      if (rtcConfig.iceServers && rtcConfig.iceServers.length > 0) {
        log('ICE servers from serverHello:', rtcConfig.iceServers.length);
      }
    }
    if (result && result.publisherSdpAnswer) {
      log('Incoming publisherSdpAnswer, pcSeq=' + result.publisherSdpAnswer.pcSeq);
    }
    if (result && result.subscriberSdpOffer) {
      log('Incoming subscriberSdpOffer, pcSeq=' + result.subscriberSdpOffer.pcSeq);
    }
    if (result && result.slotsChanged) {
      log('Incoming slotsChanged:', JSON.stringify(result.slotsChanged));
    }
    if (result && result.participantsChanged) {
      log('Incoming participantsChanged');
    }
    if (result && result.connectionStatusChanged) {
      log('Incoming connectionStatusChanged:', JSON.stringify(result.connectionStatusChanged));
    }
    return result;
  };

  // Fake media - static black canvas + silent audio
  navigator.mediaDevices.getUserMedia = function(c) {
    log('Intercepting getUserMedia');
    var canvas = document.createElement('canvas');
    canvas.width = 2; canvas.height = 2;
    var stream = canvas.captureStream(1);
    if (c && c.audio) {
      var actx = new AudioContext();
      var dest = actx.createMediaStreamDestination();
      stream.addTrack(dest.stream.getAudioTracks()[0]);
    }
    return Promise.resolve(stream);
  };

  navigator.mediaDevices.enumerateDevices = function() {
    return Promise.resolve([
      {deviceId:'fake-cam',kind:'videoinput',label:'Camera',groupId:'g1',toJSON:function(){return this}},
      {deviceId:'fake-mic',kind:'audioinput',label:'Microphone',groupId:'g2',toJSON:function(){return this}},
      {deviceId:'fake-spk',kind:'audiooutput',label:'Speaker',groupId:'g3',toJSON:function(){return this}}
    ]);
  };

  window.__hook = { log: log, mockPCs: mockPCs };
  log('Telemost Pion hook installed (MockPC mode)');
})();

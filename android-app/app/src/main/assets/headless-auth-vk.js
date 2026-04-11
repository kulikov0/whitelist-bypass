(() => {
  'use strict';

  var JOIN_LINK = window.JOIN_LINK || '';
  var ANON_NAME = window.ANON_NAME || 'Guest';

  function log(msg) { console.log('[AUTH] ' + msg); }
  function setStatus(msg) { document.getElementById('status').textContent = msg; }

  async function getText(url) {
    try {
      var response = await fetch(url, {
        headers: { 'User-Agent': navigator.userAgent }
      });
      if (response.status !== 200) log('GET ' + url.substring(0, 60) + ' status=' + response.status);
      return response.text();
    } catch(error) {
      log('GET ' + url.substring(0, 60) + ' failed: ' + error);
      setStatus('Network error: ' + error.message);
      throw error;
    }
  }

  async function postAPI(url, body, headers) {
    try {
      var response = await fetch(url, {
        method: 'POST',
        headers: Object.assign({'Content-Type': 'application/x-www-form-urlencoded'}, headers || {}),
        body: new URLSearchParams(body).toString()
      });
      return response.json();
    } catch(error) {
      log('fetch error for ' + url.substring(0, 80) + ': ' + error);
      return { error: { error_code: -1, error_msg: error.toString() } };
    }
  }

  var config = {};
  var savedAuth = null;
  var savedCaptchaSid = null;

  async function fetchConfig() {
    setStatus('Fetching config...');
    var page = await getText('https://vk.com');
    var bundleMatch = page.match(/https:\/\/[a-z0-9.-]+\/dist\/core_spa\/core_spa_vk\.[a-f0-9]+\.js/);
    if (!bundleMatch) { log('Bundle URL not found'); return false; }
    var bundleURL = bundleMatch[0];
    var chunksBase = bundleURL.substring(0, bundleURL.lastIndexOf('core_spa_vk.')) + 'chunks/';
    log('Bundle: ' + bundleURL.split('/').pop());

    var bundle = await getText(bundleURL);

    var appIdMatch = bundle.match(/[,;]u=(\d{7,8}),_=\d{7,8},p=\d{8,9}/);
    var apiMatch = bundle.match(/\d+:\(e,t,n\)=>\{"use strict";n\.d\(t,\{m:\(\)=>r\}\);const r="(5\.\d+)"\}/);
    if (!appIdMatch || !apiMatch) { log('appId or API version not found'); return false; }
    config.appId = appIdMatch[1];
    config.apiVersion = apiMatch[1];
    log('appId=' + config.appId + ' api=' + config.apiVersion);

    var bridgeMatch = bundle.match(/core_spa\/chunks\/webCallsBridge\.([a-f0-9]+)\.js/);
    if (!bridgeMatch) { log('webCallsBridge not found'); return false; }
    var bridgeURL = chunksBase + 'webCallsBridge.' + bridgeMatch[1] + '.js';
    var bridgeText = await getText(bridgeURL);

    var moduleIds = [];
    var seen = {};
    var modMatches = bridgeText.match(/i\((\d{4,6})\)/g) || [];
    modMatches.forEach(function(match) {
      var id = match.match(/\d+/)[0];
      if (!seen[id]) { seen[id] = true; moduleIds.push(id); }
    });

    var chunkMap = {};
    var chunkRegex = /(\d+)===e\)return"core_spa\/chunks\/"\+e\+"\.([a-f0-9]+)\.js"/g;
    var chunkMatch;
    while ((chunkMatch = chunkRegex.exec(bundle)) !== null) {
      chunkMap[chunkMatch[1]] = chunkMatch[2];
    }

    for (var idx = 0; idx < moduleIds.length; idx++) {
      var modId = moduleIds[idx];
      var hash = chunkMap[modId] || chunkMap[modId.substring(1)];
      if (!hash) continue;
      var chunkId = chunkMap[modId] ? modId : modId.substring(1);
      var chunkURL = chunksBase + chunkId + '.' + hash + '.js';
      var chunkText = await getText(chunkURL);

      var appVersionMatch = chunkText.match(/appVersion.{0,40}return\s+([0-9.]+)/);
      if (appVersionMatch) {
        config.appVersion = appVersionMatch[1];
        var protoMatch = chunkText.match(/protocolVersion.{0,40}return.*?(\d+)/);
        if (protoMatch) config.protocolVersion = protoMatch[1];
        log('appVersion=' + config.appVersion + ' protocolVersion=' + config.protocolVersion);
        break;
      }
    }
    return true;
  }

  window.retryCaptcha = async function(successToken) {
    setStatus('Captcha solved, retrying...');
    document.getElementById('captcha').style.display = 'none';
    var params = {
      v: config.apiVersion,
      vk_join_link: JOIN_LINK,
      name: ANON_NAME,
    };
    if (savedCaptchaSid) {
      params.captcha_sid = savedCaptchaSid;
      params.captcha_key = '';
      params.success_token = successToken;
    }
    var result = await postAPI('https://api.vk.com/method/calls.getAnonymousToken', params, savedAuth);
    log('Retry response: ' + JSON.stringify(result));
    if (result.error && result.error.error_code === 14) {
      showCaptcha(result.error);
      return;
    }
    if (result.response) {
      onAuthComplete(result.response);
    }
  };

  function showCaptcha(error) {
    savedCaptchaSid = error.captcha_sid || '';
    var captchaURL = error.redirect_uri;
    log('Captcha required, showing iframe');
    setStatus('Solve the captcha:');
    var iframe = document.getElementById('captcha');
    iframe.style.display = 'block';
    iframe.src = captchaURL;
  }

  async function onAuthComplete(response) {
    var callToken = response.token;
    var apiBaseURL = response.api_base_url;
    var okJoinLink = response.ok_join_link;
    setStatus('Authenticating with OK.ru...');

    var baseURL = apiBaseURL.replace(/\/$/, '');
    if (baseURL.indexOf('/fb.do') === -1) baseURL += '/fb.do';
    var deviceId = String(Math.floor(Math.random() * 9e18));

    var sessionData = {
      version: 2,
      device_id: deviceId,
      client_version: config.appVersion,
      client_type: 'SDK_JS',
    };

    var okLogin = await postAPI(baseURL, {
      method: 'auth.anonymLogin',
      session_data: JSON.stringify(sessionData),
      application_key: config.publicKey,
      format: 'json',
    });
    log('anonymLogin: ' + JSON.stringify(okLogin));
    if (!okLogin.session_key) {
      setStatus('anonymLogin failed: ' + JSON.stringify(okLogin));
      return;
    }

    setStatus('Auth complete, handing off to relay...');
    var authResult = {
      sessionKey: okLogin.session_key,
      applicationKey: config.publicKey,
      apiBaseURL: baseURL,
      joinLink: okJoinLink || config.okJoinLink || JOIN_LINK,
      anonymToken: callToken,
      appVersion: config.appVersion,
      protocolVersion: config.protocolVersion,
    };
    log('Passing auth to relay');
    if (typeof AndroidCaptchaBridge !== 'undefined') {
      AndroidCaptchaBridge.onJoined(JSON.stringify(authResult));
    }
  }

  async function startAuth() {
    if (!(await fetchConfig())) {
      setStatus('Config extraction failed');
      return;
    }

    setStatus('Getting anonymous token...');
    var anonResult = await postAPI('https://login.vk.com/?act=get_anonym_token', {
      client_id: config.appId,
    });
    var anonToken = anonResult.data && anonResult.data.access_token;
    if (!anonToken) {
      setStatus('Failed to get anon token');
      log('get_anonym_token failed: ' + JSON.stringify(anonResult));
      return;
    }
    log('anon token OK');
    savedAuth = { Authorization: 'Bearer ' + anonToken };

    setStatus('Getting call settings...');
    var settings = await postAPI('https://api.vk.com/method/calls.getSettings', {
      v: config.apiVersion,
    }, savedAuth);
    config.publicKey = settings.response && settings.response.settings && settings.response.settings.public_key;
    log('publicKey: ' + config.publicKey);

    setStatus('Getting call preview...');
    var preview = await postAPI('https://api.vk.com/method/calls.getCallPreview', {
      v: config.apiVersion,
      vk_join_link: JOIN_LINK,
    }, savedAuth);
    var okJoinLink = preview.response && preview.response.ok_join_link;
    if (okJoinLink) {
      config.okJoinLink = okJoinLink;
      log('okJoinLink: ' + okJoinLink);
    }

    setStatus('Getting call token...');
    var callResult = await postAPI('https://api.vk.com/method/calls.getAnonymousToken', {
      v: config.apiVersion,
      vk_join_link: JOIN_LINK,
      name: ANON_NAME,
    }, savedAuth);
    log('getAnonymousToken: ' + JSON.stringify(callResult));

    if (callResult.error && callResult.error.error_code === 14) {
      showCaptcha(callResult.error);
      return;
    }
    if (callResult.response) {
      onAuthComplete(callResult.response);
    }
  }

  startAuth().catch(function(error) {
    log('startAuth error: ' + error);
    setStatus('Error: ' + error.message);
  });
})();

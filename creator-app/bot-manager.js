const fetch = require('node-fetch');

function generateTabId(tabId) {
  var hash = 0;
  for (var i = 0; i < tabId.length; i++) {
    hash = ((hash << 5) - hash) + tabId.charCodeAt(i);
    hash = hash & hash;
  }
  return Math.abs(hash) % 10000;
}

function findTabById(tabsList, shortId) {
  var targetId = parseInt(shortId, 10);
  for (var i = 0; i < tabsList.length; i++) {
    var t = tabsList[i];
    if (generateTabId(t.id) === targetId) {
      return t;
    }
  }
  return null;
}

function createKeyboard() {
  return {
    one_time: false,
    buttons: [
      [
        { action: { type: 'text', label: '📞 VK DC', payload: JSON.stringify({ cmd: 'vk', mode: 'dc' }) } },
        { action: { type: 'text', label: '📹 VK Video', payload: JSON.stringify({ cmd: 'vk', mode: 'video' }) } }
      ],
      [
        { action: { type: 'text', label: '📞 TM DC', payload: JSON.stringify({ cmd: 'tm', mode: 'dc' }) } },
        { action: { type: 'text', label: '📹 TM Video', payload: JSON.stringify({ cmd: 'tm', mode: 'video' }) } }
      ],
      [
        { action: { type: 'text', label: '📋 Active Tabs', payload: JSON.stringify({ cmd: 'list' }) } }
      ]
    ]
  };
}

function createListKeyboard(tabsList) {
  var buttons = [];
  for (var i = 0; i < tabsList.length; i++) {
    var t = tabsList[i];
    var shortId = generateTabId(t.id);
    var paddedId = ('000' + shortId).slice(-4);
    var prefix = t.isBot ? 'bot' : 'user';
    var status = t.callStatus === 'active' ? '🟢' : '⚪';
    buttons.push([
      { action: { type: 'text', label: prefix + ' ' + t.platform + ' ' + t.mode + ' ' + paddedId + ' ' + status, payload: JSON.stringify({ cmd: 'close', id: t.id }) } }
    ]);
  }
  buttons.push([{ action: { type: 'text', label: '◀️ Back', payload: JSON.stringify({ cmd: 'menu' }) } }]);
  return {
    one_time: false,
    buttons: buttons
  };
}

class BotManager {
  constructor(settings, onCreateTab, onGetTabs, onCloseTab) {
    this.settings = settings;
    this.onCreateTab = onCreateTab;
    this.onGetTabs = onGetTabs;
    this.onCloseTab = onCloseTab;
    this.baseUrl = 'https://api.vk.com/method';
    this.ts = null;
    this.key = null;
    this.server = null;
    this.running = false;
  }
  async api(method, params = {}) {
    params.v = '5.131';
    params.access_token = this.settings.token;
    const url = new URL(`${this.baseUrl}/${method}`);
    Object.keys(params).forEach(k => url.searchParams.set(k, params[k]));
    const res = await fetch(url);
    const data = await res.json();

    if (data.error) {
      var errMsg = method + ' code: ' + data.error.error_code + ' msg: ' + (data.error.error_msg || 'VK API error') + ' params: ' + JSON.stringify(data.error.request_params || []);
      console.error('[BOT] API error:', errMsg);
      throw new Error(errMsg);
    }
    return data.response;
  }

  async getLongPollServer() {
    const data = await this.api('groups.getLongPollServer', { group_id: parseInt(this.settings.groupId) });
    this.server = data.server;
    this.key = data.key;
    this.ts = data.ts;
    return data;
  }

  async start() {
    this.running = true;
    console.log('[BOT] Starting with settings:', this.settings);
    
    try {
      await this.getLongPollServer();
      console.log('[BOT] LongPoll server:', this.server);
      this.pollLoop();
    } catch (err) {
      console.error('[BOT] Failed to start:', err.message);
      this.running = false;
      if (this.onError) this.onError(err.message);
    }
  }

  stop() {
    this.running = false;
    console.log('[BOT] Stopped');
  }

  async pollLoop() {
    while (this.running) {
      try {
        const url = `${this.server}?act=a_check&key=${this.key}&ts=${this.ts}&wait=25`;
        const res = await fetch(url);
        const data = await res.json();

        if (data.failed) {
          console.log('[BOT] LongPoll failed, reconnecting...');
          await this.getLongPollServer();
          continue;
        }
        this.ts = data.ts;
        for (const update of data.updates) {
          await this.handleUpdate(update);
        }
      } catch (err) {
        console.error('[BOT] Poll error:', err.message);
        await new Promise(r => setTimeout(r, 1000));
      }
    }
  }

  async handleUpdate(update) {
    if (update.type === 'message_new') {
      const message = update.object.message;
      var text = (message.text || '').trim();
      const fromId = message.from_id;
      const peerId = message.peer_id;
      
      var payload = null;
      if (message.payload) {
        try {
          payload = JSON.parse(message.payload);
          console.log('[BOT] Payload from button:', payload);
        } catch (e) {
          console.log('[BOT] Failed to parse payload:', message.payload);
        }
      }

      console.log('[BOT] Message from', fromId, ':', text, 'payload:', payload);

      if (this.settings.userId && fromId.toString() !== this.settings.userId.toString()) {
        return;
      }

      if (text === '/start' || text === 'start') {
        await this.showMenu(peerId);
        return;
      }

      if (payload && payload.cmd) {
        if (payload.cmd === 'list') {
          await this.showList(peerId);
          return;
        } else if (payload.cmd === 'menu') {
          await this.showMenu(peerId);
          return;
        } else if (payload.cmd === 'close') {
          if (payload.id) {
            var tabsList = this.onGetTabs ? this.onGetTabs() : [];
            var foundTab = null;
            for (var i = 0; i < tabsList.length; i++) {
              if (tabsList[i].id === payload.id) {
                foundTab = tabsList[i];
                break;
              }
            }
            if (foundTab && this.onCloseTab) {
              var shortId = generateTabId(foundTab.id);
              var paddedId = ('000' + shortId).slice(-4);
              this.onCloseTab(foundTab.id);
              await this.sendMessage(peerId, foundTab.platform + ' ' + foundTab.mode + ' ' + paddedId + ' closed', createKeyboard());
              return;
            }
          }
          return;
        } else if (payload.cmd === 'vk') {
          text = '/vk' + (payload.mode === 'video' ? ' video' : '');
        } else if (payload.cmd === 'tm') {
          text = '/tm' + (payload.mode === 'video' ? ' video' : '');
        }
      }
      
      if (text.startsWith('/vk')) {
        const mode = text.includes('video') ? 'pion-video' : 'dc';
        console.log('[BOT] Creating VK tab with mode:', mode);

        this.onCreateTab({ mode: mode, peerId: peerId, platform: 'vk' });
        await this.sendMessage(peerId, 'Creating VK call (' + (mode === 'dc' ? 'DC' : 'Video') + ')', createKeyboard());
      }

      if (text.startsWith('/tm')) {
        const mode = text.includes('video') ? 'pion-video' : 'dc';
        console.log('[BOT] Creating Telemost tab with mode:', mode);

        this.onCreateTab({ mode: mode, peerId: peerId, platform: 'telemost' });

        await this.sendMessage(peerId, 'Creating Telemost call (' + (mode === 'dc' ? 'DC' : 'Video') + ')', createKeyboard());
      }

      if (text === '/list') {
        await this.showList(peerId);
      }

      if (text.startsWith('/close ')) {
        var parts = text.split(' ');
        var targetShortId = parts[1];
        console.log('[BOT] Close request for ID:', targetShortId, 'from peer:', peerId);

        var tabsList = this.onGetTabs ? this.onGetTabs() : [];
        var foundTab = findTabById(tabsList, targetShortId);

        if (!foundTab) {
          await this.sendMessage(peerId, 'Tab ' + targetShortId + ' not found', createKeyboard());
        } else if (this.onCloseTab) {
          this.onCloseTab(foundTab.id);
          await this.sendMessage(peerId, foundTab.platform + ' ' + foundTab.mode + ' ' + targetShortId + ' closed', createKeyboard());
        }
      }
    }
  }

  async sendMessage(peerId, text, keyboard = null) {
    try {
      var params = {
        peer_id: peerId,
        message: text,
        random_id: Math.floor(Math.random() * 1e9)
      };
      if (keyboard) {
        params.keyboard = JSON.stringify(keyboard);
      }
      await this.api('messages.send', params);
      console.log('[BOT] Sent message to', peerId);
    } catch (err) {
      console.error('[BOT] Send message error:', err.message);
    }
  }

  async showMenu(peerId) {
    await this.sendMessage(peerId, '🤖 Select mode:', createKeyboard());
  }

  async showList(peerId) {
    var tabsList = this.onGetTabs ? this.onGetTabs() : [];
    if (tabsList.length === 0) {
      await this.sendMessage(peerId, 'No active tabs', createKeyboard());
    } else {
      await this.sendMessage(peerId, 'Select tab to close:', createListKeyboard(tabsList));
    }
  }
}

module.exports = BotManager;

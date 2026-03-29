const fetch = require('node-fetch');

class BotManager {
  constructor(settings, onCreateTab) {
    this.settings = settings;
    this.onCreateTab = onCreateTab;
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
      throw new Error(data.error.error_msg || 'VK API error');
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
      const text = message.text || '';
      const fromId = message.from_id;
      const peerId = message.peer_id;
      
      if (this.settings.userId && fromId.toString() !== this.settings.userId.toString()) {
        return;
      }
      
      console.log('[BOT] Message from', fromId, ':', text);
      
      if (text.startsWith('/vk')) {
        const mode = text.includes('video') ? 'pion-video' : 'dc';
        console.log('[BOT] Creating VK tab with mode:', mode);
        
        this.onCreateTab({ mode: mode, peerId: peerId, platform: 'vk' });
        await this.sendMessage(peerId, 'Creating VK call (' + (mode === 'dc' ? 'DC' : 'Video') + ')...');
      }

      if (text.startsWith('/tm')) {
        const mode = text.includes('video') ? 'pion-video' : 'dc';
        console.log('[BOT] Creating Telemost tab with mode:', mode);

        this.onCreateTab({ mode: mode, peerId: peerId, platform: 'telemost' });

        await this.sendMessage(peerId, 'Creating Telemost call (' + (mode === 'dc' ? 'DC' : 'Video') + ')...');
      }
    }
  }

  async sendMessage(peerId, text) {
    try {
      await this.api('messages.send', {
        peer_id: peerId,
        message: text,
        random_id: Math.floor(Math.random() * 1e9)
      });
      console.log('[BOT] Sent message to', peerId);
    } catch (err) {
      console.error('[BOT] Send message error:', err.message);
    }
  }
}

module.exports = BotManager;

import fetch from 'node-fetch';
import { BotSettings, TabConfig, TabListEntry, TunnelMode, Platform, BotCommand } from '../types';
import { VK_API_VERSION, VK_API_BASE_URL, BOT_POLL_RETRY_DELAY_MS, BOT_POLL_WAIT_SECONDS } from '../constants';
import {
  createMainKeyboard,
  createListKeyboard,
  findTabByShortId,
  generateShortId,
  padShortId,
} from './keyboard';

type CreateTabFn = (config: TabConfig) => Promise<void> | void;
type GetTabsFn = () => TabListEntry[];
type CloseTabFn = (tabId: string) => void;

interface VkApiError {
  error_code: number;
  error_msg?: string;
  request_params?: unknown[];
}

interface VkApiResponse {
  response?: any;
  error?: VkApiError;
}

interface LongPollData {
  ts?: string;
  failed?: number;
  updates?: any[];
}

interface VkMessage {
  text?: string;
  from_id: number;
  peer_id: number;
  payload?: string;
}

interface ButtonPayload {
  cmd: BotCommand;
  mode?: string;
  id?: string;
}

export class BotManager {
  private settings: BotSettings;
  private onCreateTab: CreateTabFn;
  private onGetTabs: GetTabsFn;
  private onCloseTab: CloseTabFn;
  private ts: string | null = null;
  private key: string | null = null;
  private server: string | null = null;
  private running = false;
  onError: ((msg: string) => void) | null = null;

  constructor(
    settings: BotSettings,
    onCreateTab: CreateTabFn,
    onGetTabs: GetTabsFn,
    onCloseTab: CloseTabFn,
  ) {
    this.settings = settings;
    this.onCreateTab = onCreateTab;
    this.onGetTabs = onGetTabs;
    this.onCloseTab = onCloseTab;
  }

  private async api(method: string, params: Record<string, any> = {}): Promise<any> {
    params.v = VK_API_VERSION;
    params.access_token = this.settings.token;
    const url = new URL(`${VK_API_BASE_URL}/${method}`);
    Object.keys(params).forEach((k) => url.searchParams.set(k, String(params[k])));
    const res = await fetch(url.toString());
    const data = (await res.json()) as VkApiResponse;
    if (data.error) {
      const err = data.error;
      const errMsg =
        `${method} code: ${err.error_code} msg: ${err.error_msg || 'VK API error'}` +
        ` params: ${JSON.stringify(err.request_params || [])}`;
      console.error('[BOT] API error:', errMsg);
      throw new Error(errMsg);
    }
    return data.response;
  }

  private async getLongPollServer(): Promise<void> {
    const data = await this.api('groups.getLongPollServer', {
      group_id: parseInt(this.settings.groupId, 10),
    });
    this.server = data.server;
    this.key = data.key;
    this.ts = data.ts;
  }

  async start(): Promise<void> {
    this.running = true;
    console.log('[BOT] Starting with settings:', this.settings);
    try {
      await this.getLongPollServer();
      console.log('[BOT] LongPoll server:', this.server);
      this.pollLoop();
    } catch (err: any) {
      console.error('[BOT] Failed to start:', err.message);
      this.running = false;
      if (this.onError) this.onError(err.message);
    }
  }

  stop(): void {
    this.running = false;
    console.log('[BOT] Stopped');
  }

  private async pollLoop(): Promise<void> {
    while (this.running) {
      try {
        const url = `${this.server}?act=a_check&key=${this.key}&ts=${this.ts}&wait=${BOT_POLL_WAIT_SECONDS}`;
        const res = await fetch(url);
        const data = (await res.json()) as LongPollData;
        if (data.failed) {
          console.log('[BOT] LongPoll failed, reconnecting...');
          await this.getLongPollServer();
          continue;
        }
        this.ts = data.ts || this.ts;
        for (const update of data.updates || []) {
          await this.handleUpdate(update);
        }
      } catch (err: any) {
        console.error('[BOT] Poll error:', err.message);
        await new Promise((resolve) => setTimeout(resolve, BOT_POLL_RETRY_DELAY_MS));
      }
    }
  }

  private async handleUpdate(update: any): Promise<void> {
    if (update.type !== 'message_new') return;

    const message: VkMessage = update.object.message;
    let text = (message.text || '').trim();
    const fromId = message.from_id;
    const peerId = message.peer_id;

    let payload: ButtonPayload | null = null;
    if (message.payload) {
      try {
        payload = JSON.parse(message.payload);
      } catch (_) {}
    }

    console.log('[BOT] Message from', fromId, ':', text, 'payload:', payload);

    if (this.settings.userId && fromId.toString() !== this.settings.userId.toString()) {
      return;
    }

    if (text === '/start' || text === 'start') {
      await this.showMenu(peerId);
      return;
    }

    if (payload?.cmd) {
      const handled = await this.handlePayloadCommand(payload, peerId);
      if (handled) return;
      if (payload.cmd === BotCommand.VK) {
        text = '/vk' + (payload.mode === 'video' ? ' video' : '');
      } else if (payload.cmd === BotCommand.TM) {
        text = '/tm' + (payload.mode === 'video' ? ' video' : '');
      }
    }

    await this.handleTextCommand(text, peerId);
  }

  private async handlePayloadCommand(payload: ButtonPayload, peerId: number): Promise<boolean> {
    if (payload.cmd === BotCommand.List) {
      await this.showList(peerId);
      return true;
    }
    if (payload.cmd === BotCommand.Menu) {
      await this.showMenu(peerId);
      return true;
    }
    if (payload.cmd === BotCommand.Close && payload.id) {
      const tabsList = this.onGetTabs();
      const tab = tabsList.find((entry) => entry.id === payload.id);
      if (tab) {
        const shortId = padShortId(generateShortId(tab.id));
        this.onCloseTab(tab.id);
        await this.sendMessage(peerId, `${tab.platform} ${tab.mode} ${shortId} closed`, createMainKeyboard());
      }
      return true;
    }
    return false;
  }

  private async handleTextCommand(text: string, peerId: number): Promise<void> {
    if (text.startsWith('/vk')) {
      const mode = text.includes('video') ? TunnelMode.PionVideo : TunnelMode.DC;
      console.log('[BOT] Creating VK tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.VK });
      const label = mode === TunnelMode.DC ? 'DC' : 'Video';
      await this.sendMessage(peerId, `Creating VK call (${label})`, createMainKeyboard());
    } else if (text.startsWith('/tm')) {
      const mode = text.includes('video') ? TunnelMode.PionVideo : TunnelMode.DC;
      console.log('[BOT] Creating Telemost tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.Telemost });
      const label = mode === TunnelMode.DC ? 'DC' : 'Video';
      await this.sendMessage(peerId, `Creating Telemost call (${label})`, createMainKeyboard());
    } else if (text === '/list') {
      await this.showList(peerId);
    } else if (text.startsWith('/close ')) {
      const targetShortId = text.split(' ')[1];
      console.log('[BOT] Close request for ID:', targetShortId);
      const tabsList = this.onGetTabs();
      const tab = findTabByShortId(tabsList, targetShortId);
      if (!tab) {
        await this.sendMessage(peerId, `Tab ${targetShortId} not found`, createMainKeyboard());
      } else {
        this.onCloseTab(tab.id);
        await this.sendMessage(
          peerId,
          `${tab.platform} ${tab.mode} ${targetShortId} closed`,
          createMainKeyboard(),
        );
      }
    }
  }

  async sendMessage(peerId: number, text: string, keyboard?: any): Promise<void> {
    try {
      const params: Record<string, any> = {
        peer_id: peerId,
        message: text,
        random_id: Math.floor(Math.random() * 1e9),
      };
      if (keyboard) {
        params.keyboard = JSON.stringify(keyboard);
      }
      await this.api('messages.send', params);
      console.log('[BOT] Sent message to', peerId);
    } catch (err: any) {
      console.error('[BOT] Send message error:', err.message);
    }
  }

  private async showMenu(peerId: number): Promise<void> {
    await this.sendMessage(peerId, 'Select mode:', createMainKeyboard());
  }

  private async showList(peerId: number): Promise<void> {
    const tabsList = this.onGetTabs();
    if (tabsList.length === 0) {
      await this.sendMessage(peerId, 'No active tabs', createMainKeyboard());
    } else {
      await this.sendMessage(peerId, 'Select tab to close:', createListKeyboard(tabsList));
    }
  }
}

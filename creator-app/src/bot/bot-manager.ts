import fetch from 'node-fetch';
import { BotSettings, TabConfig, TabListEntry, TunnelMode, Platform, BotCommand } from '../types';
import { VK_API_VERSION, VK_API_BASE_URL, BOT_POLL_RETRY_DELAY_MS, BOT_POLL_WAIT_SECONDS } from '../constants';
import {
  createMainKeyboard,
  createListKeyboard,
  createWaitingKeyboard,
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
  private awaitingJoinLink = new Set<number>();
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

    const allowedIds = this.settings.userId
      .split(',')
      .map((id) => id.trim())
      .filter((id) => id.length > 0);
    if (allowedIds.length > 0 && !allowedIds.includes(fromId.toString())) {
      return;
    }

    if (text === '/start' || text === 'start') {
      await this.showMenu(peerId);
      return;
    }

    if (payload?.cmd) {
      const handled = await this.handlePayloadCommand(payload, peerId);
      if (handled) return;
      let cmdPrefix: string | null = null;
      if (payload.cmd === BotCommand.VK) cmdPrefix = '/vk';
      else if (payload.cmd === BotCommand.TM) cmdPrefix = '/tm';
      else if (payload.cmd === BotCommand.WB) cmdPrefix = '/wb';
      else if (payload.cmd === BotCommand.Dion) cmdPrefix = '/dion';
      else if (payload.cmd === BotCommand.Bitrix) cmdPrefix = '/bitrix';
      if (cmdPrefix && payload.mode) {
        text = `${cmdPrefix} ${payload.mode}`;
      }
    }

    const wasAwaiting = this.awaitingJoinLink.has(peerId);
    const joinLink = this.detectJoinLink(text);
    if (joinLink) {
      this.awaitingJoinLink.delete(peerId);
      console.log('[BOT] Detected join link for', joinLink.platform, ':', joinLink.target);
      this.onCreateTab({
        mode: this.headlessModeFor(joinLink.platform),
        peerId,
        platform: joinLink.platform,
        joinTarget: joinLink.target,
      });
      await this.sendMessage(peerId, `Joining ${this.platformLabel(joinLink.platform)} call`, createMainKeyboard());
      return;
    }

    if (wasAwaiting) {
      await this.sendMessage(
        peerId,
        "Couldn't detect a VK / Telemost / WBStream / DION / Bitrix link in that message. Paste a join link or press Back.",
        createWaitingKeyboard(),
      );
      return;
    }

    await this.handleTextCommand(text, peerId);
  }

  private detectJoinLink(text: string): { platform: Platform; target: string } | null {
    const trimmed = text.trim();
    if (!trimmed) return null;
    const lower = trimmed.toLowerCase();
    if (lower.startsWith('wbstream://') || lower.includes('stream.wb.ru')) {
      return { platform: Platform.WBStream, target: trimmed };
    }
    if (lower.includes('telemost.yandex')) {
      return { platform: Platform.Telemost, target: trimmed };
    }
    if (lower.startsWith('dion://') || lower.includes('dion.vc')) {
      return { platform: Platform.Dion, target: trimmed };
    }
    if (lower.includes('bitrix24') && lower.includes('/video/')) {
      return { platform: Platform.Bitrix, target: trimmed };
    }
    if (lower.includes('vk.ru/call/join')) {
      return { platform: Platform.VK, target: trimmed };
    }
    return null;
  }

  private headlessModeFor(platform: Platform): TunnelMode {
    switch (platform) {
      case Platform.Telemost: return TunnelMode.HeadlessTelemost;
      case Platform.WBStream: return TunnelMode.HeadlessWBStream;
      case Platform.Dion: return TunnelMode.HeadlessDion;
      case Platform.Bitrix: return TunnelMode.HeadlessBitrix;
      default: return TunnelMode.HeadlessVK;
    }
  }

  private platformLabel(platform: Platform): string {
    switch (platform) {
      case Platform.Telemost: return 'Telemost';
      case Platform.WBStream: return 'WB Stream';
      case Platform.Dion: return 'DION';
      case Platform.Bitrix: return 'Bitrix';
      default: return 'VK';
    }
  }

  private async handlePayloadCommand(payload: ButtonPayload, peerId: number): Promise<boolean> {
    if (payload.cmd === BotCommand.Noop) {
      return true;
    }
    if (payload.cmd === BotCommand.JoinPrompt) {
      this.awaitingJoinLink.add(peerId);
      await this.sendMessage(peerId, 'Paste a join link', createWaitingKeyboard());
      return true;
    }
    this.awaitingJoinLink.delete(peerId);
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

  private parseTunnelMode(text: string, platform: Platform): TunnelMode {
    if (platform === Platform.WBStream) return TunnelMode.HeadlessWBStream;
    if (platform === Platform.Dion) return TunnelMode.HeadlessDion;
    if (platform === Platform.Bitrix) return TunnelMode.HeadlessBitrix;
    if (text.includes('headless')) {
      return platform === Platform.VK ? TunnelMode.HeadlessVK : TunnelMode.HeadlessTelemost;
    }
    if (text.includes('video')) return TunnelMode.PionVideo;
    return TunnelMode.DC;
  }

  private tunnelModeLabel(mode: TunnelMode): string {
    switch (mode) {
      case TunnelMode.HeadlessVK:
      case TunnelMode.HeadlessTelemost:
      case TunnelMode.HeadlessWBStream:
      case TunnelMode.HeadlessDion:
      case TunnelMode.HeadlessBitrix:
        return 'Headless';
      case TunnelMode.PionVideo:
        return 'Video';
      default:
        return 'DC';
    }
  }

  private async handleTextCommand(text: string, peerId: number): Promise<void> {
    if (text.startsWith('/vk')) {
      const mode = this.parseTunnelMode(text, Platform.VK);
      console.log('[BOT] Creating VK tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.VK });
      await this.sendMessage(peerId, `Creating VK call (${this.tunnelModeLabel(mode)})`, createMainKeyboard());
    } else if (text.startsWith('/tm')) {
      const mode = this.parseTunnelMode(text, Platform.Telemost);
      console.log('[BOT] Creating Telemost tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.Telemost });
      await this.sendMessage(peerId, `Creating Telemost call (${this.tunnelModeLabel(mode)})`, createMainKeyboard());
    } else if (text.startsWith('/wb')) {
      const mode = this.parseTunnelMode(text, Platform.WBStream);
      console.log('[BOT] Creating WB Stream tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.WBStream });
      await this.sendMessage(peerId, `Creating WB Stream room (${this.tunnelModeLabel(mode)})`, createMainKeyboard());
    } else if (text.startsWith('/dion')) {
      const mode = this.parseTunnelMode(text, Platform.Dion);
      console.log('[BOT] Creating DION tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.Dion });
      await this.sendMessage(peerId, `Creating DION room (${this.tunnelModeLabel(mode)})`, createMainKeyboard());
    } else if (text.startsWith('/bitrix')) {
      const mode = this.parseTunnelMode(text, Platform.Bitrix);
      console.log('[BOT] Creating Bitrix tab with mode:', mode);
      this.onCreateTab({ mode, peerId, platform: Platform.Bitrix });
      await this.sendMessage(peerId, `Creating Bitrix room (${this.tunnelModeLabel(mode)})`, createMainKeyboard());
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

import {
  RendererTab,
  BotTabData,
  BotSettings,
  UpstreamProxy,
  TunnelMode,
  Platform,
  Bridge,
  HeadlessMode,
  HeadlessStartArgs,
} from '../types';
import { HeadlessLogMarker } from '../constants';

declare const window: Window & { bridge: Bridge };

export class RendererTabManager {
  tabs: Record<string, RendererTab> = {};
  activeTabId: string | null = null;
  private nextId = 1;
  botRunning = false;
  botSettings: BotSettings;
  upstreamProxy: UpstreamProxy;
  debugLogging: boolean;
  private onRender: () => void;

  constructor(onRender: () => void) {
    this.onRender = onRender;
    const saved = localStorage.getItem('botSettings');
    this.botSettings = saved
      ? JSON.parse(saved)
      : { token: '', groupId: '', userId: '' };
    const savedProxy = localStorage.getItem('upstreamProxy');
    this.upstreamProxy = savedProxy
      ? JSON.parse(savedProxy)
      : { socks: '', user: '', pass: '' };
    this.debugLogging = localStorage.getItem('debugLogging') === 'true';
  }

  createTab(): string {
    const tabId = 'tab-' + this.nextId++;
    this.tabs[tabId] = {
      wv: null,
      url: '',
      mode: TunnelMode.DC,
      relayLogs: '',
      hookLogs: '',
      name: '',
      isBot: false,
    };
    this.selectTab(tabId);
    return tabId;
  }

  switchToHeadless(platform: Platform, joinTarget?: string): void {
    if (!this.activeTabId) return;
    const tab = this.tabs[this.activeTabId];
    if (tab.wv) tab.wv.remove();
    tab.wv = null;
    tab.url = '';
    switch (platform) {
      case Platform.Telemost:
        tab.mode = TunnelMode.HeadlessTelemost;
        if (!tab.isBot) tab.name = 'Telemost';
        break;
      case Platform.WBStream:
        tab.mode = TunnelMode.HeadlessWBStream;
        if (!tab.isBot) tab.name = 'WBStream';
        break;
      case Platform.Dion:
        tab.mode = TunnelMode.HeadlessDion;
        if (!tab.isBot) tab.name = 'DION';
        break;
      case Platform.Bitrix:
        tab.mode = TunnelMode.HeadlessBitrix;
        if (!tab.isBot) tab.name = 'Bitrix';
        break;
      default:
        tab.mode = TunnelMode.HeadlessVK;
        if (!tab.isBot) tab.name = 'VK';
    }
    tab.headless = true;
    tab.headlessStarted = false;
    tab.platform = platform;
    tab.callInfo = undefined;
    tab.headlessStatus = undefined;
    tab.tunnelConnected = false;
    tab.relayLogs = '';
    tab.hookLogs = '';
    if (tab.isBot) {
      if (joinTarget) {
        this.startHeadlessCall({ mode: HeadlessMode.Join, target: joinTarget });
      } else {
        this.startHeadlessCall({ mode: HeadlessMode.Create });
      }
    }
    this.onRender();
  }

  startHeadlessCall(args: HeadlessStartArgs): void {
    if (!this.activeTabId) return;
    const tab = this.tabs[this.activeTabId];
    if (!tab || !tab.headless || !tab.platform) return;
    tab.headlessStarted = true;
    tab.headlessStatus = 'Starting...';
    tab.callInfo = undefined;
    window.bridge.startHeadless(this.activeTabId, tab.platform, args);
    this.onRender();
  }

  createBotTab(data: BotTabData): void {
    if (!this.tabs[data.tabId]) {
      const isHeadless =
        data.mode === TunnelMode.HeadlessVK ||
        data.mode === TunnelMode.HeadlessTelemost ||
        data.mode === TunnelMode.HeadlessWBStream ||
        data.mode === TunnelMode.HeadlessDion ||
        data.mode === TunnelMode.HeadlessBitrix;
      let platformName = 'VK';
      if (data.platform === Platform.Telemost) platformName = 'Telemost';
      else if (data.platform === Platform.WBStream) platformName = 'WBStream';
      else if (data.platform === Platform.Dion) platformName = 'DION';
      else if (data.platform === Platform.Bitrix) platformName = 'Bitrix';
      const botName = isHeadless ? `Bot-${platformName}` : `Bot-${platformName} (legacy)`;
      this.tabs[data.tabId] = {
        wv: null,
        url: '',
        mode: data.mode,
        relayLogs: '',
        hookLogs: '',
        name: botName,
        isBot: true,
        peerId: data.peerId,
        platform: data.platform,
        joinedByLink: !!data.joinTarget,
      };
    }
    this.selectTab(data.tabId);
  }

  closeTab(tabId: string): void {
    const tab = this.tabs[tabId];
    if (tab?.wv) tab.wv.remove();
    if (tab?.loginWebview) tab.loginWebview.remove();
    window.bridge.closeTab(tabId);
    delete this.tabs[tabId];
    if (this.activeTabId === tabId) {
      const ids = Object.keys(this.tabs);
      this.activeTabId = ids.length > 0 ? ids[ids.length - 1] : null;
    }
    this.onRender();
  }

  selectTab(tabId: string): void {
    this.saveCurrentTabLogs();
    this.activeTabId = tabId;
    this.onRender();
  }

  saveCurrentTabLogs(): void {
    if (this.activeTabId && this.tabs[this.activeTabId]) {
      const relayEl = document.getElementById('relayLog');
      const hookEl = document.getElementById('hookLog');
      if (relayEl) this.tabs[this.activeTabId].relayLogs = relayEl.textContent || '';
      if (hookEl) this.tabs[this.activeTabId].hookLogs = hookEl.textContent || '';
    }
  }

  getActiveTab(): RendererTab | null {
    if (!this.activeTabId) return null;
    return this.tabs[this.activeTabId] || null;
  }

  getTabLabel(tab: RendererTab): string {
    if (tab.name) return tab.name;
    if (tab.url) {
      if (tab.url.includes('vk.ru')) return 'VK (legacy)';
      if (tab.url.includes('telemost')) return 'Telemost (legacy)';
      if (tab.url.includes('dion.vc')) return 'DION';
    }
    return 'New';
  }

  appendRelayLog(tabId: string, msg: string): void {
    const tab = this.tabs[tabId];
    if (!tab) return;
    tab.relayLogs += (tab.relayLogs ? '\n' : '') + msg;
    let rendered = false;
    if (tab.headless) rendered = this.parseHeadlessLog(tabId, msg);
    if (tabId === this.activeTabId && !rendered) {
      const el = document.getElementById('relayLog');
      if (el) {
        if (el.textContent!.length > 0) el.textContent += '\n';
        el.textContent += msg;
        el.scrollTop = el.scrollHeight;
      }
    }
  }

  private parseHeadlessLog(tabId: string, msg: string): boolean {
    const tab = this.tabs[tabId];
    if (!tab) return false;
    const trimmed = msg.trim();
    let changed = false;

    if (trimmed === HeadlessLogMarker.CALL_CREATED) {
      tab.callInfo = {};
      tab.headlessStatus = 'Call created';
      changed = true;
    }
    if (trimmed.includes(HeadlessLogMarker.JOIN_LINK) && tab.callInfo) {
      tab.callInfo.joinLink = trimmed.split(HeadlessLogMarker.JOIN_LINK)[1].trim();
      if (tab.isBot) {
        const reply = tab.joinedByLink ? 'Joined successfully' : tab.callInfo.joinLink;
        window.bridge.sendBotCallLink(tabId, reply);
      }
      changed = true;
    }
    if (trimmed.includes(HeadlessLogMarker.TURN) && tab.callInfo) {
      tab.callInfo.turn = trimmed.split(HeadlessLogMarker.TURN)[1].trim();
      changed = true;
    }
    if (trimmed.includes(HeadlessLogMarker.PROTOCOL) && tab.callInfo) {
      tab.callInfo.protocol = trimmed.split(HeadlessLogMarker.PROTOCOL)[1].trim();
      changed = true;
    }
    if (trimmed.includes('[FATAL]')) {
      const fatalMessage = trimmed.split('[FATAL]')[1]?.trim() || 'fatal error';
      tab.headlessStatus = 'Disconnected: ' + fatalMessage;
      tab.tunnelConnected = false;
      changed = true;
    }
    if (changed && tabId === this.activeTabId) this.onRender();
    return changed;
  }

  setTunnelMode(mode: string): void {
    if (!this.activeTabId) return;
    const tab = this.tabs[this.activeTabId];
    if (!tab) return;
    tab.mode = mode as TunnelMode;
    window.bridge.setTunnelMode(this.activeTabId, mode).then(() => {
      if (tab.wv) {
        tab.wv.executeJavaScript('window.__hookInstalled = false').catch(() => {});
        tab.wv.reload();
      }
    });
  }

  saveBotSettings(): void {
    localStorage.setItem('botSettings', JSON.stringify(this.botSettings));
  }

  saveUpstreamProxy(): void {
    localStorage.setItem('upstreamProxy', JSON.stringify(this.upstreamProxy));
    window.bridge.setUpstreamProxy(this.upstreamProxy);
  }

  saveDebugLogging(): void {
    localStorage.setItem('debugLogging', String(this.debugLogging));
    window.bridge.setDebugLogging(this.debugLogging);
  }

  toggleBot(): void {
    if (!this.botSettings.token || !this.botSettings.groupId) return;
    this.botRunning = !this.botRunning;
    localStorage.setItem('botEnabled', this.botRunning ? 'true' : 'false');
    if (this.botRunning) {
      window.bridge.startBot(this.botSettings);
    } else {
      window.bridge.stopBot();
    }
    this.onRender();
  }

  autoStartBot(): void {
    if (
      localStorage.getItem('botEnabled') === 'true' &&
      this.botSettings.token &&
      this.botSettings.groupId
    ) {
      this.botRunning = true;
      window.bridge.startBot(this.botSettings);
    }
  }
}

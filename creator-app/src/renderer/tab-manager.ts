import { RendererTab, BotTabData, BotSettings, TunnelMode, Platform, Bridge } from '../types';
import { HeadlessLogMarker } from '../constants';

declare const window: Window & { bridge: Bridge };

export class RendererTabManager {
  tabs: Record<string, RendererTab> = {};
  activeTabId: string | null = null;
  private nextId = 1;
  botRunning = false;
  botSettings: BotSettings;
  private onRender: () => void;

  constructor(onRender: () => void) {
    this.onRender = onRender;
    const saved = localStorage.getItem('botSettings');
    this.botSettings = saved
      ? JSON.parse(saved)
      : { token: '', groupId: '', userId: '' };
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
    window.bridge.startRelay(tabId);
    this.selectTab(tabId);
    return tabId;
  }

  switchToHeadless(platform: Platform): void {
    if (!this.activeTabId) return;
    const tab = this.tabs[this.activeTabId];
    if (tab.wv) tab.wv.remove();
    const isTelemost = platform === Platform.Telemost;
    tab.wv = null;
    tab.url = '';
    tab.mode = isTelemost ? TunnelMode.HeadlessTelemost : TunnelMode.HeadlessVK;
    tab.name = isTelemost ? 'Headless TM' : 'Headless VK';
    tab.headless = true;
    tab.platform = platform;
    tab.callInfo = undefined;
    tab.headlessStatus = 'Starting...';
    tab.tunnelConnected = false;
    tab.relayLogs = '';
    tab.hookLogs = '';
    window.bridge.startHeadless(this.activeTabId, platform);
    this.onRender();
  }

  createBotTab(data: BotTabData): void {
    if (!this.tabs[data.tabId]) {
      const botName = data.platform === Platform.Telemost ? 'Bot-Telemost' : 'Bot-VK';
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
      };
    }
    this.selectTab(data.tabId);
  }

  closeTab(tabId: string): void {
    const tab = this.tabs[tabId];
    if (tab?.wv) tab.wv.remove();
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
      if (tab.url.includes('vk.com')) return 'VK';
      if (tab.url.includes('telemost')) return 'Telemost';
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
    if (trimmed.includes(HeadlessLogMarker.JOIN_LINK)) {
      if (!tab.callInfo) tab.callInfo = {};
      tab.callInfo.joinLink = trimmed.split(HeadlessLogMarker.JOIN_LINK)[1].trim();
      changed = true;
    }
    if (trimmed.includes(HeadlessLogMarker.SHORT_LINK)) {
      if (!tab.callInfo) tab.callInfo = {};
      tab.callInfo.shortLink = trimmed.split(HeadlessLogMarker.SHORT_LINK)[1].trim();
      changed = true;
    }
    if (trimmed.includes(HeadlessLogMarker.TURN)) {
      if (!tab.callInfo) tab.callInfo = {};
      tab.callInfo.turn = trimmed.split(HeadlessLogMarker.TURN)[1].trim();
      changed = true;
    }
    if (trimmed.includes(HeadlessLogMarker.PROTOCOL)) {
      if (!tab.callInfo) tab.callInfo = {};
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

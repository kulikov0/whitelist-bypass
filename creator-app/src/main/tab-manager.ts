import { app, BrowserWindow } from 'electron';
import * as net from 'net';
import * as path from 'path';
import * as fs from 'fs/promises';
import {
  TabState,
  PortPair,
  TabListEntry,
  Platform,
  TunnelMode,
  CallStatus,
  HeadlessStartArgs,
  UpstreamProxy,
  DionCredentials,
  BitrixCredentials,
} from '../types';
import {
  INITIAL_PORT_BASE,
  RELAY_RESTART_DELAY_MS,
  LOG_CAPTURE_SNIPPET,
} from '../constants';
import { BotManager } from '../bot/bot-manager';
import { CookieStore } from './cookie-store';
import { HeadlessLauncher } from './headless-launcher';

export class TabManager {
  private tabs = new Map<string, TabState>();
  private callStatusCache = new Map<string, CallStatus>();
  private botTabIds = new Set<string>();
  private nextPortBase = INITIAL_PORT_BASE;
  private _mainWindow: BrowserWindow | null = null;
  private _botManager: BotManager | null = null;
  private hooksDir: string;
  private cookies = new CookieStore();
  private launcher: HeadlessLauncher;

  constructor() {
    this.hooksDir = app.isPackaged
      ? path.join(process.resourcesPath!, 'hooks')
      : path.join(__dirname, '..', '..', '..', 'hooks');
    this.launcher = new HeadlessLauncher(this, this.cookies);
  }

  get mainWindow(): BrowserWindow | null {
    return this._mainWindow;
  }

  set mainWindow(w: BrowserWindow | null) {
    this._mainWindow = w;
  }

  get botManager(): BotManager | null {
    return this._botManager;
  }

  set botManager(bm: BotManager | null) {
    this._botManager = bm;
  }

  private isPortFree(port: number): Promise<boolean> {
    return new Promise((resolve) => {
      const server = net.createServer();
      server.once('error', () => resolve(false));
      server.once('listening', () => {
        server.close(() => resolve(true));
      });
      server.listen(port, '127.0.0.1');
    });
  }

  async allocPorts(): Promise<PortPair> {
    while (true) {
      const dc = this.nextPortBase;
      const pion = this.nextPortBase + 1;
      this.nextPortBase += 2;
      if (await this.isPortFree(dc) && await this.isPortFree(pion)) {
        return { dc, pion };
      }
    }
  }

  async getOrCreateTab(tabId: string): Promise<TabState> {
    if (!this.tabs.has(tabId)) {
      const ports = await this.allocPorts();
      this.tabs.set(tabId, {
        relay: null,
        tunnelMode: TunnelMode.DC,
        platform: Platform.VK,
        dcPort: ports.dc,
        pionPort: ports.pion,
      });
    }
    return this.tabs.get(tabId)!;
  }

  getTab(tabId: string): TabState | undefined {
    return this.tabs.get(tabId);
  }

  deleteTab(tabId: string): void {
    const tab = this.tabs.get(tabId);
    if (tab) {
      this.launcher.killRelay(tabId, tab);
      this.tabs.delete(tabId);
    }
    this.botTabIds.delete(tabId);
    this.callStatusCache.delete(tabId);
  }

  addBotTab(tabId: string): void {
    this.botTabIds.add(tabId);
  }

  removeBotTab(tabId: string): void {
    this.botTabIds.delete(tabId);
  }

  isBotTab(tabId: string): boolean {
    return this.botTabIds.has(tabId);
  }

  setCallStatus(tabId: string, status: CallStatus): void {
    this.callStatusCache.set(tabId, status);
  }

  getCallStatus(tabId: string): CallStatus {
    return this.callStatusCache.get(tabId) || CallStatus.Inactive;
  }

  getTabList(): TabListEntry[] {
    const result: TabListEntry[] = [];
    this.tabs.forEach((tab, tabId) => {
      result.push({
        id: tabId,
        platform: tab.platform,
        mode: tab.tunnelMode,
        isBot: tab.isBot === true,
        callStatus: this.getCallStatus(tabId),
      });
    });
    return result;
  }

  sendBotCallLink(tabId: string, link: string): void {
    if (!this.botTabIds.has(tabId) || !this._botManager) return;
    const tab = this.tabs.get(tabId);
    if (!tab || tab.peerId == null) return;
    console.log(`[MAIN] Headless call link for bot tab ${tabId}:`, link);
    this._botManager.sendMessage(tab.peerId, link);
  }

  setUpstreamProxy(proxy: UpstreamProxy): void {
    this.launcher.setUpstreamProxy(proxy);
  }

  setDebugLogging(enabled: boolean): void {
    this.launcher.setDebugLogging(enabled);
  }

  startRelay(tabId: string, tab: TabState): void {
    this.launcher.startRelay(tabId, tab);
  }

  startHeadless(tabId: string, platform: Platform, args: HeadlessStartArgs): Promise<void> {
    return this.launcher.startHeadless(tabId, platform, args);
  }

  killRelay(tabId: string, tab: TabState): void {
    this.launcher.killRelay(tabId, tab);
  }

  killAllRelays(): void {
    this.tabs.forEach((tab, tabId) => this.launcher.killRelay(tabId, tab));
  }

  async setTunnelMode(tabId: string, mode: TunnelMode, platform?: Platform): Promise<void> {
    const tab = await this.getOrCreateTab(tabId);
    tab.tunnelMode = mode;
    if (platform) tab.platform = platform;
    if (
      mode === TunnelMode.HeadlessVK ||
      mode === TunnelMode.HeadlessTelemost ||
      mode === TunnelMode.HeadlessWBStream ||
      mode === TunnelMode.HeadlessDion ||
      mode === TunnelMode.HeadlessBitrix
    ) return;
    this.launcher.killRelay(tabId, tab);
    setTimeout(() => this.launcher.startRelay(tabId, tab), RELAY_RESTART_DELAY_MS);
  }

  async loadHook(tabId: string, url: string, tab: TabState): Promise<string> {
    const isDion = url.includes('dion.vc');
    const isTelemost = url.includes('telemost.yandex');
    if (isDion) {
      tab.platform = Platform.Dion;
      return LOG_CAPTURE_SNIPPET;
    }
    tab.platform = isTelemost ? Platform.Telemost : Platform.VK;

    if (isTelemost || tab.tunnelMode === TunnelMode.PionVideo) {
      const hookFile = isTelemost ? 'video-telemost.js' : 'video-vk.js';
      const hook = await fs.readFile(path.join(this.hooksDir, hookFile), 'utf8');
      return LOG_CAPTURE_SNIPPET + `window.PION_PORT=${tab.pionPort};window.IS_CREATOR=true;` + hook;
    }

    const hook = await fs.readFile(path.join(this.hooksDir, 'dc-creator-vk.js'), 'utf8');
    return LOG_CAPTURE_SNIPPET + `window.WS_PORT=${tab.dcPort};` + hook;
  }

  getDionCredentials(): Promise<DionCredentials> {
    return this.cookies.getDionCredentials();
  }

  setDionCredentials(email: string, password: string): Promise<void> {
    return this.cookies.setDionCredentials(email, password);
  }

  getBitrixCredentials(): Promise<BitrixCredentials> {
    return this.cookies.getBitrixCredentials();
  }

  setBitrixCredentials(portal: string, email: string, password: string): Promise<void> {
    return this.cookies.setBitrixCredentials(portal, email, password);
  }

  clearPlatformCookies(platform: Platform): Promise<number> {
    return this.cookies.clearPlatformCookies(platform);
  }

  buildCookiesZip(): Promise<Buffer> {
    return this.cookies.buildCookiesZip();
  }

  setWBStreamDeviceId(id: string): Promise<void> {
    return this.cookies.setWBStreamDeviceId(id);
  }
}

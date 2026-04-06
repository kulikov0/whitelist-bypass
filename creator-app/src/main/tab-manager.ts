import { app } from 'electron';
import { spawn, ChildProcess } from 'child_process';
import { BrowserWindow, session } from 'electron';
import * as net from 'net';
import * as path from 'path';
import * as fs from 'fs/promises';
import { TabState, PortPair, TabListEntry, Platform, TunnelMode, RelayMode, CallStatus } from '../types';
import {
  INITIAL_PORT_BASE,
  IPC,
  RELAY_RESTART_DELAY_MS,
  SESSION_PARTITION,
  VK_COOKIE_DOMAINS,
  YANDEX_COOKIE_DOMAINS,
  LOG_CAPTURE_SNIPPET,
} from '../constants';
import { BotManager } from '../bot/bot-manager';

function resolveResourcePath(devRelative: string, packedName: string): string {
  if (app.isPackaged) {
    return path.join(process.resourcesPath!, packedName);
  }
  return path.join(__dirname, '..', '..', '..', devRelative);
}

function binaryName(base: string): string {
  return process.platform === 'win32' ? base + '.exe' : base;
}

export class TabManager {
  private tabs = new Map<string, TabState>();
  private callStatusCache = new Map<string, CallStatus>();
  private botTabIds = new Set<string>();
  private nextPortBase = INITIAL_PORT_BASE;
  private _mainWindow: BrowserWindow | null = null;
  private _botManager: BotManager | null = null;
  private relayPath: string;
  private headlessVKPath: string;
  private headlessTelemostPath: string;
  private hooksDir: string;

  constructor() {
    this.relayPath = resolveResourcePath(
      path.join('relay', binaryName('relay')),
      binaryName('relay'),
    );
    this.headlessVKPath = resolveResourcePath(
      path.join('headless', 'vk', binaryName('headless-vk-creator')),
      binaryName('headless-vk-creator'),
    );
    this.headlessTelemostPath = resolveResourcePath(
      path.join('headless', 'telemost', binaryName('headless-telemost')),
      binaryName('headless-telemost-creator'),
    );
    this.hooksDir = app.isPackaged
      ? path.join(process.resourcesPath!, 'hooks')
      : path.join(__dirname, '..', '..', '..', 'hooks');
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
      this.killRelay(tabId, tab);
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

  findBotPeerId(platform: Platform): number | null {
    for (const [tabId, tab] of this.tabs) {
      if (this.botTabIds.has(tabId) && tab.platform === platform && tab.peerId != null) {
        return tab.peerId;
      }
    }
    return null;
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

  private sendLog(tabId: string, msg: string): void {
    if (this._mainWindow && !this._mainWindow.isDestroyed()) {
      this._mainWindow.webContents.send(IPC.RELAY_LOG, { tabId, msg });
    }
  }

  private attachProcessOutput(proc: ChildProcess, tabId: string): void {
    const onData = (data: Buffer) => {
      data
        .toString()
        .trim()
        .split('\n')
        .forEach((msg) => {
          if (!msg) return;
          console.log(`[relay:${tabId}]`, msg);
          this.sendLog(tabId, msg);
        });
    };
    proc.stdout?.on('data', onData);
    proc.stderr?.on('data', onData);
  }

  startRelay(tabId: string, tab: TabState): void {
    this.killRelay(tabId, tab);
    const port = tab.tunnelMode === TunnelMode.PionVideo ? tab.pionPort : tab.dcPort;
    let relayMode: RelayMode = RelayMode.DCCreator;
    if (tab.tunnelMode === TunnelMode.PionVideo) {
      relayMode = tab.platform === Platform.Telemost
        ? RelayMode.TelemostVideoCreator
        : RelayMode.VKVideoCreator;
    }
    const proc = spawn(this.relayPath, ['--mode', relayMode, '--ws-port', String(port)], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    tab.relay = proc;
    this.attachProcessOutput(proc, tabId);
    proc.on('close', (code) => {
      this.sendLog(tabId, `Relay exited with code ${code}`);
    });
  }

  async startHeadless(tabId: string, platform: Platform): Promise<void> {
    const tab = await this.getOrCreateTab(tabId);
    const isTelemost = platform === Platform.Telemost;
    tab.tunnelMode = isTelemost ? TunnelMode.HeadlessTelemost : TunnelMode.HeadlessVK;
    tab.platform = platform;
    const cookieStr = isTelemost
      ? await this.getYandexCookieString()
      : await this.getVKCookieString();
    if (!cookieStr) {
      const name = isTelemost ? 'Yandex' : 'VK';
      this.sendLog(tabId, `No ${name} cookies found. Please log into ${name} first.`);
      return;
    }
    this.killRelay(tabId, tab);
    const binaryPath = isTelemost ? this.headlessTelemostPath : this.headlessVKPath;
    const proc = spawn(binaryPath, ['--cookie-string', cookieStr, '--resources', 'default'], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    tab.relay = proc;
    this.attachProcessOutput(proc, tabId);
    proc.on('close', (code) => {
      this.sendLog(tabId, `Headless exited with code ${code}`);
    });
  }

  killRelay(tabId: string, tab: TabState): void {
    if (tab.relay) {
      console.log(`[${tabId}] killing process pid=${tab.relay.pid}`);
      tab.relay.kill();
      tab.relay = null;
    }
  }

  killAllRelays(): void {
    this.tabs.forEach((tab, tabId) => this.killRelay(tabId, tab));
  }

  async loadHook(tabId: string, url: string, tab: TabState): Promise<string> {
    const isTelemost = url.includes('telemost.yandex');
    const newPlatform = isTelemost ? Platform.Telemost : Platform.VK;

    if (newPlatform !== tab.platform && tab.tunnelMode === TunnelMode.PionVideo) {
      tab.platform = newPlatform;
      this.killRelay(tabId, tab);
      setTimeout(() => this.startRelay(tabId, tab), RELAY_RESTART_DELAY_MS);
    } else {
      tab.platform = newPlatform;
    }

    if (tab.tunnelMode === TunnelMode.PionVideo) {
      const hookFile = isTelemost ? 'video-telemost.js' : 'video-vk.js';
      const hook = await fs.readFile(path.join(this.hooksDir, hookFile), 'utf8');
      return LOG_CAPTURE_SNIPPET + `window.PION_PORT=${tab.pionPort};window.IS_CREATOR=true;` + hook;
    }

    const hookFile = isTelemost ? 'dc-creator-telemost.js' : 'dc-creator-vk.js';
    const hook = await fs.readFile(path.join(this.hooksDir, hookFile), 'utf8');
    return LOG_CAPTURE_SNIPPET + `window.WS_PORT=${tab.dcPort};` + hook;
  }

  setTunnelMode(tabId: string, mode: TunnelMode): void {
    const tab = this.tabs.get(tabId);
    if (!tab) return;
    tab.tunnelMode = mode;
    this.killRelay(tabId, tab);
    setTimeout(() => this.startRelay(tabId, tab), RELAY_RESTART_DELAY_MS);
  }

  async getVKCookieString(): Promise<string> {
    const ses = session.fromPartition(SESSION_PARTITION);
    const all = await ses.cookies.get({});
    const vkCookies = all.filter((cookie) => {
      return cookie.domain != null && VK_COOKIE_DOMAINS.some((d) => cookie.domain!.includes(d));
    });
    return vkCookies.map((cookie) => `${cookie.name}=${cookie.value}`).join('; ');
  }

  async getYandexCookieString(): Promise<string> {
    const ses = session.fromPartition(SESSION_PARTITION);
    const all = await ses.cookies.get({});
    const yaCookies = all.filter((cookie) => {
      return cookie.domain != null && YANDEX_COOKIE_DOMAINS.some((d) => cookie.domain!.includes(d));
    });
    return yaCookies.map((cookie) => `${cookie.name}=${cookie.value}`).join('; ');
  }

}

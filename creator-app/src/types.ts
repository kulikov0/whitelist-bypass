import type { ChildProcess } from 'child_process';

export enum TunnelMode {
  DC = 'dc',
  PionVideo = 'pion-video',
  HeadlessVK = 'headless-vk',
}

export enum Platform {
  VK = 'vk',
  Telemost = 'telemost',
}

export enum RelayMode {
  DCCreator = 'dc-creator',
  TelemostVideoCreator = 'telemost-video-creator',
  VKVideoCreator = 'vk-video-creator',
}

export enum CallStatus {
  Active = 'active',
  Inactive = 'inactive',
}

export enum BotCommand {
  VK = 'vk',
  TM = 'tm',
  List = 'list',
  Menu = 'menu',
  Close = 'close',
}

export enum LogPanel {
  Relay = 'relay',
  Hook = 'hook',
}

export interface PortPair {
  dc: number;
  pion: number;
}

export interface TabState {
  relay: ChildProcess | null;
  tunnelMode: TunnelMode;
  platform: Platform;
  dcPort: number;
  pionPort: number;
  peerId?: number;
  isBot?: boolean;
}

export interface BotSettings {
  token: string;
  groupId: string;
  userId: string;
}

export interface TabConfig {
  mode: TunnelMode;
  peerId: number;
  platform: Platform;
}

export interface TabListEntry {
  id: string;
  platform: Platform;
  mode: TunnelMode;
  isBot: boolean;
  callStatus: CallStatus;
}

export interface CallInfo {
  joinLink?: string;
  shortLink?: string;
  turn?: string;
  protocol?: string;
}

export interface Webview extends Electron.WebviewTag {
  getURL(): string;
  setAudioMuted(muted: boolean): void;
  executeJavaScript(code: string): Promise<any>;
  reload(): void;
}

export interface RendererTab {
  wv: Webview | null;
  url: string;
  mode: TunnelMode;
  relayLogs: string;
  hookLogs: string;
  name: string;
  isBot: boolean;
  peerId?: number;
  platform?: Platform;
  headless?: boolean;
  headlessStatus?: string;
  callInfo?: CallInfo;
  tunnelConnected?: boolean;
}

export interface BotTabData {
  tabId: string;
  mode: TunnelMode;
  peerId: number;
  platform: Platform;
}

export interface RelayLogData {
  tabId: string;
  msg: string;
}

export interface Bridge {
  onRelayLog(cb: (tabId: string, msg: string) => void): void;
  getHookCode(tabId: string, url: string): Promise<string>;
  setTunnelMode(tabId: string, mode: string): Promise<void>;
  startRelay(tabId: string): Promise<void>;
  closeTab(tabId: string): Promise<void>;
  startBot(settings: BotSettings): Promise<void>;
  stopBot(): Promise<void>;
  onCreateBotTab(cb: (data: BotTabData) => void): void;
  getCallCreatorCode(scriptFile: string): Promise<string>;
  onBotError(cb: (msg: string) => void): void;
  getCookies(domain: string): Promise<Electron.Cookie[]>;
  startHeadless(tabId: string): Promise<void>;
  onCloseBotTab(cb: (data: { tabId: string }) => void): void;
}

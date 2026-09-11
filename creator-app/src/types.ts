import type { ChildProcess } from 'child_process';

export enum TunnelMode {
  DC = 'dc',
  PionVideo = 'pion-video',
  HeadlessVK = 'headless-vk',
  HeadlessTelemost = 'headless-telemost',
  HeadlessWBStream = 'headless-wbstream',
  HeadlessDion = 'headless-dion',
  HeadlessBitrix = 'headless-bitrix',
}

export enum Platform {
  VK = 'vk',
  Telemost = 'telemost',
  WBStream = 'wbstream',
  Dion = 'dion',
  Bitrix = 'bitrix',
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
  WB = 'wb',
  Dion = 'dion',
  Bitrix = 'bitrix',
  JoinPrompt = 'join-prompt',
  List = 'list',
  Menu = 'menu',
  Close = 'close',
  Noop = 'noop',
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

export interface UpstreamProxy {
  socks: string;
  user: string;
  pass: string;
}

export interface CookieEntry {
  name: string;
  value: string;
}

export interface CookieFileContent {
  email?: string;
  password?: string;
  cookies: CookieEntry[];
}

export interface DionCredentials {
  email: string;
  password: string;
}

export interface BitrixCredentials {
  portal: string;
  email: string;
  password: string;
}

export interface TabConfig {
  mode: TunnelMode;
  peerId: number;
  platform: Platform;
  joinTarget?: string;
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
  turn?: string;
  protocol?: string;
}

export enum HeadlessMode {
  Create = 'create',
  Join = 'join',
}

export interface HeadlessStartArgs {
  mode: HeadlessMode;
  target?: string;
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
  headlessStarted?: boolean;
  headlessStartTarget?: string;
  headlessStatus?: string;
  callInfo?: CallInfo;
  tunnelConnected?: boolean;
  loginWebview?: Webview;
  joinedByLink?: boolean;
}

export interface BotTabData {
  tabId: string;
  mode: TunnelMode;
  peerId: number;
  platform: Platform;
  joinTarget?: string;
}

export interface RelayLogData {
  tabId: string;
  msg: string;
}

export interface Bridge {
  onRelayLog(cb: (tabId: string, msg: string) => void): void;
  getHookCode(tabId: string, url: string): Promise<string>;
  setTunnelMode(tabId: string, mode: string, platform?: string): Promise<void>;
  startRelay(tabId: string): Promise<void>;
  closeTab(tabId: string): Promise<void>;
  startBot(settings: BotSettings): Promise<void>;
  stopBot(): Promise<void>;
  setUpstreamProxy(proxy: UpstreamProxy): Promise<void>;
  setDebugLogging(enabled: boolean): Promise<void>;
  clearCookies(platform: string): Promise<number>;
  getDionCredentials(): Promise<DionCredentials>;
  setDionCredentials(email: string, password: string): Promise<void>;
  getBitrixCredentials(): Promise<BitrixCredentials>;
  setBitrixCredentials(portal: string, email: string, password: string): Promise<void>;
  onCreateBotTab(cb: (data: BotTabData) => void): void;
  getCallCreatorCode(scriptFile: string): Promise<string>;
  onBotError(cb: (msg: string) => void): void;
  exportCookiesZip(): Promise<Uint8Array>;
  startHeadless(tabId: string, platform: string, args: HeadlessStartArgs): Promise<void>;
  sendBotCallLink(tabId: string, link: string): Promise<void>;
  onCloseBotTab(cb: (data: { tabId: string }) => void): void;
  onLoginRequired(cb: (tabId: string, url: string) => void): void;
  onLoginDone(cb: (tabId: string) => void): void;
}

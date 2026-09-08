import { BrowserWindow } from 'electron';
import { spawn, ChildProcess } from 'child_process';
import * as path from 'path';
import * as fs from 'fs/promises';
import {
  TabState,
  Platform,
  TunnelMode,
  RelayMode,
  HeadlessStartArgs,
  HeadlessMode,
  UpstreamProxy,
} from '../types';
import { IPC, BITRIX_LOGIN_URL } from '../constants';
import { BotManager } from '../bot/bot-manager';
import { DionCookieFile } from './dion-cookie-file';
import { BitrixCookieFile } from './bitrix-cookie-file';
import { resolveResourcePath, binaryName } from './util/paths';
import { PLATFORM_CONFIG } from './platform-config';
import { CookieStore } from './cookie-store';

export interface LauncherHost {
  getOrCreateTab(tabId: string): Promise<TabState>;
  getTab(tabId: string): TabState | undefined;
  readonly mainWindow: BrowserWindow | null;
  readonly botManager: BotManager | null;
}

type AuthErrorKind = 'invalid' | 'expired';

function parseAuthError(msg: string): AuthErrorKind | null {
  if (msg.includes('STATUS:AUTH_ERROR:INVALID_CREDENTIALS')) return 'invalid';
  if (msg.includes('STATUS:AUTH_ERROR:SESSION_EXPIRED')) return 'expired';
  return null;
}

export class HeadlessLauncher {
  private relayPath: string;
  private bitrixPath: string;
  private binaryPaths = new Map<Platform, string>();
  private upstreamProxy: UpstreamProxy = { socks: '', user: '', pass: '' };
  private debugLogging = false;

  constructor(
    private host: LauncherHost,
    private cookies: CookieStore,
  ) {
    this.relayPath = resolveResourcePath(
      path.join('relay', binaryName('relay')),
      binaryName('relay'),
    );
    this.bitrixPath = resolveResourcePath(
      path.join('headless', 'bitrix', binaryName('headless-bitrix-creator')),
      binaryName('headless-bitrix-creator'),
    );
    for (const [platform, config] of Object.entries(PLATFORM_CONFIG)) {
      this.binaryPaths.set(
        platform as Platform,
        resolveResourcePath(
          path.join('headless', config!.binarySubdir, binaryName(config!.binaryBase)),
          binaryName(config!.binaryBase),
        ),
      );
    }
  }

  setUpstreamProxy(proxy: UpstreamProxy): void {
    this.upstreamProxy = {
      socks: (proxy?.socks || '').trim(),
      user: (proxy?.user || '').trim(),
      pass: (proxy?.pass || '').trim(),
    };
  }

  setDebugLogging(enabled: boolean): void {
    this.debugLogging = enabled;
  }

  sendLog(tabId: string, msg: string): void {
    const win = this.host.mainWindow;
    if (win && !win.isDestroyed()) {
      win.webContents.send(IPC.RELAY_LOG, { tabId, msg });
    }
  }

  killRelay(tabId: string, tab: TabState): void {
    if (tab.relay) {
      console.log(`[${tabId}] killing process pid=${tab.relay.pid}`);
      tab.relay.kill();
      tab.relay = null;
    }
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
    const relayArgs = ['--mode', relayMode, '--ws-port', String(port)];
    this.appendUpstreamArgs(relayArgs);
    if (this.debugLogging) relayArgs.push('--debug');
    const proc = spawn(this.relayPath, relayArgs, {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    tab.relay = proc;
    this.attachProcessOutput(proc, tabId);
    proc.on('close', (code) => {
      this.sendLog(tabId, `Relay exited with code ${code}`);
    });
  }

  async startHeadless(tabId: string, platform: Platform, args: HeadlessStartArgs): Promise<void> {
    if (platform === Platform.Bitrix) {
      await this.startBitrixHeadless(tabId, args);
      return;
    }
    const tab = await this.host.getOrCreateTab(tabId);
    tab.platform = platform;
    const joinTarget = args.mode === HeadlessMode.Join ? (args.target || '').trim() : '';
    if (args.mode === HeadlessMode.Join && !joinTarget) {
      this.sendLog(tabId, 'Join requested but no target link/room provided.');
      return;
    }

    const config = PLATFORM_CONFIG[platform];
    if (!config) {
      this.sendLog(tabId, `Unsupported headless platform: ${platform}`);
      return;
    }
    tab.tunnelMode = config.tunnelMode;
    const cookiesPath = this.cookies.cookieFilePath(platform);
    const dionCookieFile = platform === Platform.Dion ? new DionCookieFile(cookiesPath) : null;
    const fileHasSession = dionCookieFile != null && await dionCookieFile.hasSession();
    let cookies = await this.cookies.getCookiesForDomains(config.cookieDomains);
    const needsLogin = !fileHasSession && !cookies.some((c) => c.name === config.refreshCookie);
    if (needsLogin) {
      if (tab.isBot) {
        const reply = `Please log into ${config.platformName} in the creator app first, then try again.`;
        this.sendLog(tabId, reply);
        if (this.host.botManager && tab.peerId != null) {
          await this.host.botManager.sendMessage(tab.peerId, reply);
        }
        return;
      }
      this.sendLog(tabId, `No ${config.platformName} session found, opening login.`);
      const win = this.host.mainWindow;
      if (win && !win.isDestroyed()) {
        win.webContents.send(IPC.LOGIN_REQUIRED, { tabId, url: config.loginUrl });
      }
      if (config.loginWaitCookies) {
        this.sendLog(tabId, `Waiting for ${config.loginWaitCookies.join(', ')}...`);
        await this.cookies.waitForCookies(config.loginWaitCookies);
      } else {
        await this.cookies.waitForLogin(config.cookieDomains, config.authCookie);
      }
      if (win && !win.isDestroyed()) {
        win.webContents.send(IPC.LOGIN_DONE, { tabId });
      }
      this.sendLog(tabId, `${config.platformName} login captured.`);
      cookies = await this.cookies.getCookiesForDomains(config.cookieDomains);
    }
    if (fileHasSession) {
      this.sendLog(tabId, `${config.platformName} session file is still active, keeping it as is.`);
    } else {
      this.sendLog(tabId, `${config.platformName} cookies (${cookies.length}): ${cookies.map((c) => c.name).join(', ')}`);
      await fs.writeFile(cookiesPath, JSON.stringify(cookies));
    }
    const spawnArgs = ['--resources', 'default', '--cookies', cookiesPath];
    this.killRelay(tabId, tab);
    if (joinTarget && config.joinFlag) {
      spawnArgs.push(config.joinFlag, joinTarget);
    }
    this.appendUpstreamArgs(spawnArgs);
    if (this.debugLogging) spawnArgs.push('--debug');
    const proc = spawn(this.binaryPaths.get(platform)!, spawnArgs, {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    tab.relay = proc;
    let authError: AuthErrorKind | null = null;
    this.attachProcessOutput(proc, tabId, (msg) => {
      const kind = parseAuthError(msg);
      if (kind) authError = kind;
    });
    proc.on('close', async (code) => {
      this.sendLog(tabId, `Headless exited with code ${code}`);
      if (authError === 'invalid') {
        this.sendLog(tabId, `${config.platformName} rejected the email and password stored in ${cookiesPath}, fix them or clear cookies to log in through the browser.`);
        return;
      }
      if (authError === 'expired') {
        if (dionCookieFile) await dionCookieFile.clearTokens();
        await this.cookies.clearAuthCookies(config.cookieDomains, config.authCookie);
        if (this.host.getTab(tabId) === tab) this.startHeadless(tabId, platform, args);
      }
    });
  }

  private async startBitrixHeadless(tabId: string, args: HeadlessStartArgs): Promise<void> {
    const tab = await this.host.getOrCreateTab(tabId);
    tab.platform = Platform.Bitrix;
    tab.tunnelMode = TunnelMode.HeadlessBitrix;
    const joinTarget = args.mode === HeadlessMode.Join ? (args.target || '').trim() : '';
    if (args.mode === HeadlessMode.Join && !joinTarget) {
      this.sendLog(tabId, 'Join requested but no target link/room provided.');
      return;
    }
    const cookiesPath = this.cookies.cookieFilePath(Platform.Bitrix);
    const account = new BitrixCookieFile(cookiesPath);
    const creds = await account.readCredentials();
    const hasPassword = !!(creds.portal && creds.email && creds.password);

    let sessionData = await this.cookies.captureBitrixSession();
    if (!hasPassword && !sessionData) {
      if (tab.isBot) {
        const reply = 'Please log into Bitrix in the creator app first, then try again.';
        this.sendLog(tabId, reply);
        if (this.host.botManager && tab.peerId != null) {
          await this.host.botManager.sendMessage(tab.peerId, reply);
        }
        return;
      }
      this.sendLog(tabId, 'No Bitrix session found, opening login. Pick your portal in the Bitrix login screen.');
      const win = this.host.mainWindow;
      if (win && !win.isDestroyed()) {
        win.webContents.send(IPC.LOGIN_REQUIRED, { tabId, url: BITRIX_LOGIN_URL });
      }
      this.sendLog(tabId, 'Waiting for the portal session...');
      sessionData = await this.cookies.waitForBitrixSession();
      if (win && !win.isDestroyed()) {
        win.webContents.send(IPC.LOGIN_DONE, { tabId });
      }
      this.sendLog(tabId, `Bitrix login captured on ${sessionData?.portal}.`);
    }

    if (!hasPassword) {
      if (!sessionData) {
        this.sendLog(tabId, 'Could not capture Bitrix cookies, log in and try again.');
        return;
      }
      await account.writeSession(sessionData.portal, sessionData.cookies);
      this.sendLog(tabId, `Bitrix session captured, portal ${sessionData.portal}`);
    } else {
      this.sendLog(tabId, 'Bitrix account credentials present, logging in with portal and password.');
    }

    const spawnArgs = ['--resources', 'default', '--cookies', cookiesPath];
    this.killRelay(tabId, tab);
    if (joinTarget) {
      spawnArgs.push('--room', joinTarget);
    }
    this.appendUpstreamArgs(spawnArgs);
    if (this.debugLogging) spawnArgs.push('--debug');
    const proc = spawn(this.bitrixPath, spawnArgs, {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    tab.relay = proc;
    let authError: AuthErrorKind | null = null;
    this.attachProcessOutput(proc, tabId, (msg) => {
      const kind = parseAuthError(msg);
      if (kind) authError = kind;
    });
    proc.on('close', async (code) => {
      this.sendLog(tabId, `Headless exited with code ${code}`);
      if (authError === 'invalid') {
        this.sendLog(tabId, `Bitrix rejected the portal, email or password stored in ${cookiesPath}, fix them in the Bitrix account card or log in again.`);
        return;
      }
      if (authError === 'expired' && !hasPassword) {
        await this.cookies.clearPlatformCookies(Platform.Bitrix);
        if (this.host.getTab(tabId) === tab) this.startBitrixHeadless(tabId, args);
      }
    });
  }

  private appendUpstreamArgs(args: string[]): void {
    if (!this.upstreamProxy.socks) return;
    args.push('--upstream-socks', this.upstreamProxy.socks);
    if (this.upstreamProxy.user) args.push('--upstream-user', this.upstreamProxy.user);
    if (this.upstreamProxy.pass) args.push('--upstream-pass', this.upstreamProxy.pass);
  }

  private attachProcessOutput(
    proc: ChildProcess,
    tabId: string,
    inspect?: (msg: string) => void,
  ): void {
    const onData = (data: Buffer) => {
      data
        .toString()
        .trim()
        .split('\n')
        .forEach((msg) => {
          if (!msg) return;
          console.log(`[relay:${tabId}]`, msg);
          this.sendLog(tabId, msg);
          if (inspect) inspect(msg);
        });
    };
    proc.stdout?.on('data', onData);
    proc.stderr?.on('data', onData);
  }
}

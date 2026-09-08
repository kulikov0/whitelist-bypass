import { ipcMain } from 'electron';
import * as path from 'path';
import * as fs from 'fs/promises';
import * as QRCode from 'qrcode';
import { TabManager } from './tab-manager';
import { BotManager } from '../bot/bot-manager';
import { IPC } from '../constants';
import { TunnelMode, Platform, BotSettings, HeadlessStartArgs, UpstreamProxy } from '../types';

export function registerIpcHandlers(tabManager: TabManager): void {
  ipcMain.handle(IPC.GET_HOOK_CODE, async (_e, tabId: string, url: string) => {
    const tab = await tabManager.getOrCreateTab(tabId);
    return tabManager.loadHook(tabId, url, tab);
  });

  ipcMain.handle(IPC.GET_CALL_CREATOR_CODE, async (_e, scriptFile: string) => {
    const filePath = path.join(__dirname, '..', '..', 'scripts', scriptFile || 'vk-call-creator.js');
    return fs.readFile(filePath, 'utf8');
  });

  ipcMain.handle(IPC.RENDER_QR, async (_e, text: string) => {
    if (!text) return '';
    try {
      return await QRCode.toDataURL(text, {
        errorCorrectionLevel: 'M',
        margin: 1,
        scale: 6,
      });
    } catch {
      return '';
    }
  });

  ipcMain.handle(IPC.SET_TUNNEL_MODE, (_e, tabId: string, mode: string, platform?: string) => {
    if (!Object.values(TunnelMode).includes(mode as TunnelMode)) return;
    tabManager.setTunnelMode(tabId, mode as TunnelMode, platform as Platform | undefined);
  });

  ipcMain.handle(IPC.START_RELAY, async (_e, tabId: string) => {
    const tab = await tabManager.getOrCreateTab(tabId);
    tabManager.startRelay(tabId, tab);
  });

  ipcMain.handle(IPC.START_HEADLESS, async (_e, tabId: string, platform: string, args: HeadlessStartArgs) => {
    await tabManager.startHeadless(tabId, platform as Platform, args);
  });

  ipcMain.handle(IPC.CLOSE_TAB, (_e, tabId: string) => {
    tabManager.deleteTab(tabId);
  });

  ipcMain.handle(IPC.START_BOT, (_e, settings: BotSettings) => {
    if (tabManager.botManager) {
      tabManager.botManager.stop();
    }
    const bm = new BotManager(
      settings,
      async (tabConfig) => {
        if (!tabManager.mainWindow || tabManager.mainWindow.isDestroyed()) return;
        const tabId = 'bot-tab-' + Date.now();
        const tab = await tabManager.getOrCreateTab(tabId);
        tab.tunnelMode = tabConfig.mode;
        tab.platform = tabConfig.platform || Platform.VK;
        tab.peerId = tabConfig.peerId;
        tab.isBot = true;
        tabManager.addBotTab(tabId);
        tabManager.mainWindow.webContents.send(IPC.CREATE_BOT_TAB, {
          tabId,
          mode: tabConfig.mode,
          peerId: tabConfig.peerId,
          platform: tabConfig.platform || Platform.VK,
          joinTarget: tabConfig.joinTarget,
        });
        console.log('[BOT] Created tab:', tabId, 'mode:', tabConfig.mode, 'platform:', tabConfig.platform);
      },
      () => tabManager.getTabList(),
      (tabId) => {
        tabManager.deleteTab(tabId);
        console.log('[BOT] Closed tab:', tabId);
        if (tabManager.mainWindow && !tabManager.mainWindow.isDestroyed()) {
          tabManager.mainWindow.webContents.send(IPC.CLOSE_BOT_TAB, { tabId });
        }
      },
    );
    bm.onError = (msg: string) => {
      if (tabManager.mainWindow && !tabManager.mainWindow.isDestroyed()) {
        tabManager.mainWindow.webContents.send(IPC.BOT_ERROR, msg);
      }
    };
    tabManager.botManager = bm;
    bm.start();
    return { success: true };
  });

  ipcMain.handle(IPC.STOP_BOT, () => {
    if (tabManager.botManager) {
      tabManager.botManager.stop();
      tabManager.botManager = null;
    }
    return { success: true };
  });

  ipcMain.handle(IPC.SET_UPSTREAM_PROXY, (_e, proxy: UpstreamProxy) => {
    tabManager.setUpstreamProxy(proxy);
  });

  ipcMain.handle(IPC.SET_DEBUG_LOGGING, (_e, enabled: boolean) => {
    tabManager.setDebugLogging(enabled);
  });

  ipcMain.handle(IPC.CLEAR_COOKIES, (_e, platform: string) => {
    return tabManager.clearPlatformCookies(platform as Platform);
  });

  ipcMain.handle(IPC.GET_DION_CREDENTIALS, () => {
    return tabManager.getDionCredentials();
  });

  ipcMain.handle(IPC.SET_DION_CREDENTIALS, (_e, email: string, password: string) => {
    return tabManager.setDionCredentials(email, password);
  });

  ipcMain.handle(IPC.GET_BITRIX_CREDENTIALS, () => {
    return tabManager.getBitrixCredentials();
  });

  ipcMain.handle(IPC.SET_BITRIX_CREDENTIALS, (_e, portal: string, email: string, password: string) => {
    return tabManager.setBitrixCredentials(portal, email, password);
  });

  ipcMain.handle(IPC.SEND_BOT_CALL_LINK, (_e, tabId: string, link: string) => {
    tabManager.sendBotCallLink(tabId, link);
  });

  ipcMain.handle(IPC.EXPORT_COOKIES_ZIP, async () => {
    return tabManager.buildCookiesZip();
  });
}

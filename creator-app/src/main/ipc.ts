import { ipcMain, session } from 'electron';
import * as path from 'path';
import * as fs from 'fs/promises';
import { TabManager } from './tab-manager';
import { BotManager } from '../bot/bot-manager';
import {
  IPC,
  SESSION_PARTITION,
  VK_COOKIE_DOMAINS,
  YANDEX_COOKIE_DOMAINS,
} from '../constants';
import { TunnelMode, Platform, BotSettings } from '../types';

export function registerIpcHandlers(tabManager: TabManager): void {
  ipcMain.handle(IPC.GET_HOOK_CODE, async (_e, tabId: string, url: string) => {
    const tab = await tabManager.getOrCreateTab(tabId);
    return tabManager.loadHook(tabId, url, tab);
  });

  ipcMain.handle(IPC.GET_CALL_CREATOR_CODE, async (_e, scriptFile: string) => {
    const filePath = path.join(__dirname, '..', '..', 'scripts', scriptFile || 'vk-call-creator.js');
    return fs.readFile(filePath, 'utf8');
  });

  ipcMain.handle(IPC.SET_TUNNEL_MODE, (_e, tabId: string, mode: string) => {
    if (!Object.values(TunnelMode).includes(mode as TunnelMode)) return;
    tabManager.setTunnelMode(tabId, mode as TunnelMode);
  });

  ipcMain.handle(IPC.START_RELAY, async (_e, tabId: string) => {
    const tab = await tabManager.getOrCreateTab(tabId);
    tabManager.startRelay(tabId, tab);
  });

  ipcMain.handle(IPC.START_HEADLESS, async (_e, tabId: string, platform: string) => {
    await tabManager.startHeadless(tabId, platform as Platform);
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

  ipcMain.handle(IPC.GET_COOKIES, async (_e, domain: string) => {
    const ses = session.fromPartition(SESSION_PARTITION);
    const all = await ses.cookies.get({});
    const domains = domain === 'yandex' ? YANDEX_COOKIE_DOMAINS : VK_COOKIE_DOMAINS;
    const filtered = all.filter((cookie) => {
      return cookie.domain != null && domains.some((d) => cookie.domain!.includes(d));
    });
    console.log(`[COOKIES] total: ${all.length} ${domain}: ${filtered.length}`);
    return filtered;
  });
}

import { app, BrowserWindow, session, Session } from 'electron';
import * as path from 'path';
import { TabManager } from './tab-manager';
import { VkAutoclick } from '../autoclick/vk';
import { TelemostAutoclick } from '../autoclick/telemost';
import {
  SESSION_PARTITION,
  USER_AGENT,
  WINDOW_WIDTH,
  WINDOW_HEIGHT,
  WBSTREAM_API_ORIGIN,
} from '../constants';
import { Platform } from '../types';
import { parseCallStatus, extractTaggedCallLink } from './util/log-tags';

function stripCSP(ses: Session): void {
  ses.webRequest.onHeadersReceived((details, callback) => {
    const headers = { ...details.responseHeaders };
    delete headers['content-security-policy'];
    delete headers['Content-Security-Policy'];
    delete headers['content-security-policy-report-only'];
    delete headers['Content-Security-Policy-Report-Only'];
    callback({ responseHeaders: headers });
  });
}

export function createWindow(tabManager: TabManager): BrowserWindow {
  const ses = session.fromPartition(SESSION_PARTITION);
  stripCSP(ses);
  ses.setPermissionRequestHandler((_wc, _perm, cb) => cb(true));
  ses.setPermissionCheckHandler(() => true);
  ses.setUserAgent(USER_AGENT);

  ses.webRequest.onBeforeSendHeaders({ urls: [`${WBSTREAM_API_ORIGIN}/*`] }, (details, callback) => {
    const authorization = details.requestHeaders['Authorization'] ?? details.requestHeaders['authorization'];
    if (typeof authorization === 'string' && authorization.startsWith('Bearer ')) {
      tabManager.setWBStreamAccessToken(authorization.slice('Bearer '.length)).catch(() => {});
    }
    callback({ requestHeaders: details.requestHeaders });
  });

  app.on('session-created', stripCSP);

  const win = new BrowserWindow({
    width: WINDOW_WIDTH,
    height: WINDOW_HEIGHT,
    icon: path.join(__dirname, '..', '..', 'resources', 'icon.png'),
    webPreferences: {
      preload: path.join(__dirname, '..', 'preload', 'index.js'),
      nodeIntegration: true,
      contextIsolation: false,
      webviewTag: true,
    },
  });

  win.loadFile('index.html');
  win.on('closed', () => {
    tabManager.mainWindow = null;
  });

  const autoclickers = new Map<number, { telemost: TelemostAutoclick; vk: VkAutoclick }>();

  win.webContents.on('did-attach-webview', (_e, wvContents) => {
    wvContents.on('before-input-event', (_e, input) => {
      if (input.key === 'F12') wvContents.openDevTools();
    });

    wvContents.on('will-navigate', (event, url) => {
      if (!url.startsWith('http://') && !url.startsWith('https://')) event.preventDefault();
    });
    wvContents.setWindowOpenHandler(({ url }) => {
      if (!url.startsWith('http://') && !url.startsWith('https://')) return { action: 'deny' };
      return { action: 'allow' };
    });

    wvContents.on('did-navigate', (_e, url) => {
      const wcId = wvContents.id;
      if (!autoclickers.has(wcId)) {
        autoclickers.set(wcId, {
          telemost: new TelemostAutoclick(),
          vk: new VkAutoclick(),
        });
      }
      const ac = autoclickers.get(wcId)!;
      if (url.includes('telemost.yandex')) {
        ac.vk.stop();
        ac.telemost.attach(wvContents);
      } else if (url.includes('vk.ru')) {
        ac.telemost.stop();
        ac.vk.attach(wvContents);
      } else {
        ac.telemost.stop();
        ac.vk.stop();
      }
    });

    wvContents.on('console-message', (_e, _level, msg) => {
      if (msg.includes('state: disconnected') || msg.includes('state: failed')) {
        const ac = autoclickers.get(wvContents.id);
        if (ac) ac.vk.kickDisconnected();
      }

      handleBotCallLink(tabManager, msg, Platform.VK);
      handleBotCallLink(tabManager, msg, Platform.Telemost);

      const callStatus = parseCallStatus(msg);
      if (callStatus) {
        console.log('[MAIN] Cached status for', callStatus.tabId, ':', callStatus.status);
        tabManager.setCallStatus(callStatus.tabId, callStatus.status);
      }
    });

    wvContents.on('destroyed', () => {
      const ac = autoclickers.get(wvContents.id);
      if (ac) {
        ac.telemost.stop();
        ac.vk.stop();
        autoclickers.delete(wvContents.id);
      }
    });
  });

  return win;
}

function handleBotCallLink(tabManager: TabManager, msg: string, platform: Platform): void {
  const tagged = extractTaggedCallLink(msg, platform);
  if (!tagged) return;
  const tab = tabManager.getTab(tagged.tabId);
  if (!tab || tab.peerId == null) {
    console.log(`[MAIN] ${platform} call link captured but no peer for tab ${tagged.tabId}`);
    return;
  }
  console.log(`[MAIN] Sending ${platform} link to peer ${tab.peerId} (tab ${tagged.tabId}):`, tagged.link);
  if (tabManager.botManager) {
    tabManager.botManager.sendMessage(tab.peerId, tagged.link);
  }
}

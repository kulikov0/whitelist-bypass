import { ipcRenderer } from 'electron';
import { IPC } from '../constants';

(window as any).bridge = {
  onRelayLog(cb: (tabId: string, msg: string) => void) {
    ipcRenderer.on(IPC.RELAY_LOG, (_e, data) => cb(data.tabId, data.msg));
  },
  getHookCode(tabId: string, url: string) {
    return ipcRenderer.invoke(IPC.GET_HOOK_CODE, tabId, url);
  },
  setTunnelMode(tabId: string, mode: string, platform?: string) {
    return ipcRenderer.invoke(IPC.SET_TUNNEL_MODE, tabId, mode, platform);
  },
  startRelay(tabId: string) {
    return ipcRenderer.invoke(IPC.START_RELAY, tabId);
  },
  closeTab(tabId: string) {
    return ipcRenderer.invoke(IPC.CLOSE_TAB, tabId);
  },
  startBot(settings: any) {
    return ipcRenderer.invoke(IPC.START_BOT, settings);
  },
  stopBot() {
    return ipcRenderer.invoke(IPC.STOP_BOT);
  },
  setUpstreamProxy(proxy: any) {
    return ipcRenderer.invoke(IPC.SET_UPSTREAM_PROXY, proxy);
  },
  setDebugLogging(enabled: boolean) {
    return ipcRenderer.invoke(IPC.SET_DEBUG_LOGGING, enabled);
  },
  clearCookies(platform: string) {
    return ipcRenderer.invoke(IPC.CLEAR_COOKIES, platform);
  },
  getDionCredentials() {
    return ipcRenderer.invoke(IPC.GET_DION_CREDENTIALS);
  },
  setDionCredentials(email: string, password: string) {
    return ipcRenderer.invoke(IPC.SET_DION_CREDENTIALS, email, password);
  },
  getBitrixCredentials() {
    return ipcRenderer.invoke(IPC.GET_BITRIX_CREDENTIALS);
  },
  setBitrixCredentials(portal: string, email: string, password: string) {
    return ipcRenderer.invoke(IPC.SET_BITRIX_CREDENTIALS, portal, email, password);
  },
  onCreateBotTab(cb: (data: any) => void) {
    ipcRenderer.on(IPC.CREATE_BOT_TAB, (_e, data) => cb(data));
  },
  getCallCreatorCode(scriptFile: string) {
    return ipcRenderer.invoke(IPC.GET_CALL_CREATOR_CODE, scriptFile);
  },
  onBotError(cb: (msg: string) => void) {
    ipcRenderer.on(IPC.BOT_ERROR, (_e, msg) => cb(msg));
  },
  exportCookiesZip() {
    return ipcRenderer.invoke(IPC.EXPORT_COOKIES_ZIP);
  },
  renderQr(text: string) {
    return ipcRenderer.invoke(IPC.RENDER_QR, text);
  },
  startHeadless(tabId: string, platform: string, args: any) {
    return ipcRenderer.invoke(IPC.START_HEADLESS, tabId, platform, args);
  },
  sendBotCallLink(tabId: string, link: string) {
    return ipcRenderer.invoke(IPC.SEND_BOT_CALL_LINK, tabId, link);
  },
  onCloseBotTab(cb: (data: any) => void) {
    ipcRenderer.on(IPC.CLOSE_BOT_TAB, (_e, data) => cb(data));
  },
  onLoginRequired(cb: (tabId: string, url: string) => void) {
    ipcRenderer.on(IPC.LOGIN_REQUIRED, (_e, data) => cb(data.tabId, data.url));
  },
  onLoginDone(cb: (tabId: string) => void) {
    ipcRenderer.on(IPC.LOGIN_DONE, (_e, data) => cb(data.tabId));
  },
};

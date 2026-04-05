import { ipcRenderer } from 'electron';
import { IPC } from '../constants';

(window as any).bridge = {
  onRelayLog(cb: (tabId: string, msg: string) => void) {
    ipcRenderer.on(IPC.RELAY_LOG, (_e, data) => cb(data.tabId, data.msg));
  },
  getHookCode(tabId: string, url: string) {
    return ipcRenderer.invoke(IPC.GET_HOOK_CODE, tabId, url);
  },
  setTunnelMode(tabId: string, mode: string) {
    return ipcRenderer.invoke(IPC.SET_TUNNEL_MODE, tabId, mode);
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
  onCreateBotTab(cb: (data: any) => void) {
    ipcRenderer.on(IPC.CREATE_BOT_TAB, (_e, data) => cb(data));
  },
  getCallCreatorCode(scriptFile: string) {
    return ipcRenderer.invoke(IPC.GET_CALL_CREATOR_CODE, scriptFile);
  },
  onBotError(cb: (msg: string) => void) {
    ipcRenderer.on(IPC.BOT_ERROR, (_e, msg) => cb(msg));
  },
  getCookies(domain: string) {
    return ipcRenderer.invoke(IPC.GET_COOKIES, domain);
  },
  startHeadless(tabId: string) {
    return ipcRenderer.invoke(IPC.START_HEADLESS, tabId);
  },
  onCloseBotTab(cb: (data: any) => void) {
    ipcRenderer.on(IPC.CLOSE_BOT_TAB, (_e, data) => cb(data));
  },
};

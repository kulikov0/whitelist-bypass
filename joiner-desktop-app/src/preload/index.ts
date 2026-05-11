import { contextBridge, ipcRenderer } from 'electron';
import { IPC, JoinerSettings } from '../constants';

contextBridge.exposeInMainWorld('bridge', {
  start: (settings: JoinerSettings) => ipcRenderer.invoke(IPC.START, settings),
  stop: () => ipcRenderer.invoke(IPC.STOP),
  onLog(cb: (text: string) => void) {
    ipcRenderer.on(IPC.LOG, (_e, text) => cb(text));
  },
  onStatus(cb: (status: string) => void) {
    ipcRenderer.on(IPC.STATUS, (_e, status) => cb(status));
  },
  onRunning(cb: (running: boolean) => void) {
    ipcRenderer.on(IPC.RUNNING, (_e, v) => cb(v));
  },
});

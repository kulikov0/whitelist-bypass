import { RendererTabManager } from './tab-manager';
import {
  renderTabs,
  renderContent,
  renderBotButton,
  loadURL,
  startHookLogPoller,
  showError,
  closeError,
  clearLog,
  saveLogs,
  exportCookiesZip,
  copyToClipboard,
  renameTab,
  attachLoginWebview,
  detachLoginWebview,
} from './dom';
import { VK_IM_URL, TELEMOST_URL } from '../constants';
import { Platform, Bridge, BotTabData, LogPanel, TunnelMode, HeadlessMode } from '../types';

declare const window: Window & { bridge: Bridge };

const tm = new RendererTabManager(() => {
  renderTabs(tm);
  renderContent(tm);
  renderBotButton(tm);
});

function bindTabBarEvents(): void {
  document.getElementById('tabBar')!.addEventListener('click', (event) => {
    const target = event.target as HTMLElement;
    const action = target.dataset.action;
    const tabEl = target.closest('[data-tab-id]') as HTMLElement | null;
    const tabId = tabEl?.dataset.tabId;

    if (action === 'add-tab') {
      tm.createTab();
      return;
    }
    if (action === 'close' && tabId) {
      event.stopPropagation();
      tm.closeTab(tabId);
      return;
    }
    if (action === 'rename' && tabId) {
      event.stopPropagation();
      renameTab(tm, tabId, tabEl!);
      return;
    }
    if (tabId) {
      tm.selectTab(tabId);
    }
  });
}

function bindToolbarEvents(): void {
  document.getElementById('btnVk')!.addEventListener('click', () => loadURL(tm, VK_IM_URL));
  document.getElementById('btnTelemost')!.addEventListener('click', () => loadURL(tm, TELEMOST_URL));
  document.getElementById('btnHeadlessVK')!.addEventListener('click', () => tm.switchToHeadless(Platform.VK));
  document.getElementById('btnHeadlessTM')!.addEventListener('click', () => tm.switchToHeadless(Platform.Telemost));
  document.getElementById('btnHeadlessWB')!.addEventListener('click', () => tm.switchToHeadless(Platform.WBStream));
  document.getElementById('btnHeadlessDion')!.addEventListener('click', () => tm.switchToHeadless(Platform.Dion));
  document.getElementById('btnHeadlessBitrix')!.addEventListener('click', () => tm.switchToHeadless(Platform.Bitrix));
  document.getElementById('modeSelect')!.addEventListener('change', (event) => {
    tm.setTunnelMode((event.target as HTMLSelectElement).value);
  });
  document.getElementById('btnSaveLogs')!.addEventListener('click', saveLogs);
}

function bindActionBarEvents(): void {
  document.getElementById('btnExportCookies')!.addEventListener('click', exportCookiesZip);
  document.getElementById('btnSettings')!.addEventListener('click', openSettings);
  document.getElementById('tabBot')!.addEventListener('click', () => {
    if (!tm.botSettings.token || !tm.botSettings.groupId) {
      openSettings();
      return;
    }
    tm.toggleBot();
  });
}

function bindSettingsEvents(): void {
  document.getElementById('btnSettingsCancel')!.addEventListener('click', closeSettings);
  document.getElementById('btnSettingsSave')!.addEventListener('click', () => {
    tm.botSettings.token = (document.getElementById('vkToken') as HTMLInputElement).value.trim();
    tm.botSettings.groupId = (document.getElementById('vkGroupId') as HTMLInputElement).value.trim();
    tm.botSettings.userId = (document.getElementById('vkUserId') as HTMLInputElement).value.trim();
    tm.saveBotSettings();
    tm.upstreamProxy.socks = (document.getElementById('upstreamSocks') as HTMLInputElement).value.trim();
    tm.upstreamProxy.user = (document.getElementById('upstreamUser') as HTMLInputElement).value.trim();
    tm.upstreamProxy.pass = (document.getElementById('upstreamPass') as HTMLInputElement).value.trim();
    tm.saveUpstreamProxy();
    tm.debugLogging = (document.getElementById('debugLogging') as HTMLInputElement).checked;
    tm.saveDebugLogging();
    closeSettings();
  });
  document.querySelectorAll('.btn-clear-cookies').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const platform = (btn as HTMLElement).dataset.platform!;
      const status = document.getElementById('clearCookiesStatus')!;
      status.textContent = `Clearing ${platform} cookies...`;
      try {
        const removed = await window.bridge.clearCookies(platform);
        status.textContent = `Cleared ${removed} ${platform} cookies. Log in again to use it.`;
      } catch {
        status.textContent = `Failed to clear ${platform} cookies.`;
      }
    });
  });
}

interface AccountPopupConfig {
  name: string;
  fields: string[];
  load: () => Promise<Record<string, string>>;
  save: (values: Record<string, string>) => Promise<void>;
  readError: string;
}

function bindAccountPopup(cfg: AccountPopupConfig): void {
  const lc = cfg.name.toLowerCase();
  const popup = document.getElementById(`${lc}AccountPopup`)!;
  const status = document.getElementById(`${lc}AccountStatus`)!;
  const field = (key: string) =>
    document.getElementById(`${lc}${key[0].toUpperCase()}${key.slice(1)}`) as HTMLInputElement;

  const close = (): void => popup.classList.remove('visible');
  const open = async (): Promise<void> => {
    status.textContent = '';
    popup.classList.add('visible');
    try {
      const values = await cfg.load();
      for (const key of cfg.fields) field(key).value = values[key] ?? '';
    } catch {
      status.textContent = cfg.readError;
    }
  };

  document.getElementById(`btn${cfg.name}Account`)!.addEventListener('click', open);
  popup.addEventListener('click', close);
  popup.querySelector('.popup')!.addEventListener('click', (event) => event.stopPropagation());
  document.getElementById(`btn${cfg.name}AccountCancel`)!.addEventListener('click', close);
  document.getElementById(`btn${cfg.name}AccountSave`)!.addEventListener('click', async () => {
    const values: Record<string, string> = {};
    for (const key of cfg.fields) {
      const raw = field(key).value;
      values[key] = key === 'password' ? raw : raw.trim();
    }
    try {
      await cfg.save(values);
      close();
    } catch {
      status.textContent = 'Failed to save the credentials.';
    }
  });
}

function bindErrorPopup(): void {
  document.getElementById('errorPopup')!.addEventListener('click', closeError);
  document.querySelector('#errorPopup .popup')!.addEventListener('click', (event) => event.stopPropagation());
  document.getElementById('btnErrorClose')!.addEventListener('click', closeError);
}

function bindLogEvents(): void {
  document.getElementById('btnClearRelay')!.addEventListener('click', () => clearLog(LogPanel.Relay));
  document.getElementById('btnClearHook')!.addEventListener('click', () => clearLog(LogPanel.Hook));
  document.getElementById('btnSaveLogsHeadless')!.addEventListener('click', saveLogs);
}

function bindHeadlessEvents(): void {
  document.getElementById('headlessJoinLink')!.addEventListener('click', (event) => {
    copyToClipboard((event.target as HTMLElement).textContent || '');
  });
  document.getElementById('headlessInfo')!.addEventListener('click', (event) => {
    const target = event.target as HTMLElement;
    const copyTarget = target.dataset.copy;
    if (copyTarget) {
      const sourceEl = document.getElementById(copyTarget);
      if (sourceEl) copyToClipboard(sourceEl.textContent || '');
    }
  });
  document.getElementById('btnHeadlessCreate')!.addEventListener('click', () => {
    clearHeadlessJoinError();
    tm.startHeadlessCall({ mode: HeadlessMode.Create });
  });
  document.getElementById('btnHeadlessJoin')!.addEventListener('click', () => {
    const targetInput = document.getElementById('headlessStartTarget') as HTMLInputElement;
    const target = targetInput.value.trim();
    if (!target) {
      showHeadlessJoinError('Enter a room or link to join.');
      targetInput.focus();
      return;
    }
    clearHeadlessJoinError();
    tm.startHeadlessCall({ mode: HeadlessMode.Join, target });
  });
  document.getElementById('headlessStartTarget')!.addEventListener('input', (event) => {
    const tab = tm.getActiveTab();
    if (tab) tab.headlessStartTarget = (event.target as HTMLInputElement).value;
    clearHeadlessJoinError();
  });
}

function showHeadlessJoinError(msg: string): void {
  const errEl = document.getElementById('headlessStartError')!;
  const input = document.getElementById('headlessStartTarget') as HTMLInputElement;
  errEl.textContent = msg;
  input.classList.add('invalid');
}

function clearHeadlessJoinError(): void {
  const errEl = document.getElementById('headlessStartError')!;
  const input = document.getElementById('headlessStartTarget') as HTMLInputElement;
  errEl.textContent = '';
  input.classList.remove('invalid');
}

function openSettings(): void {
  document.getElementById('settingsPopup')!.classList.add('visible');
  (document.getElementById('vkToken') as HTMLInputElement).value = tm.botSettings.token;
  (document.getElementById('vkGroupId') as HTMLInputElement).value = tm.botSettings.groupId;
  (document.getElementById('vkUserId') as HTMLInputElement).value = tm.botSettings.userId;
  (document.getElementById('upstreamSocks') as HTMLInputElement).value = tm.upstreamProxy.socks;
  (document.getElementById('upstreamUser') as HTMLInputElement).value = tm.upstreamProxy.user;
  (document.getElementById('upstreamPass') as HTMLInputElement).value = tm.upstreamProxy.pass;
  (document.getElementById('debugLogging') as HTMLInputElement).checked = tm.debugLogging;
  document.getElementById('clearCookiesStatus')!.textContent = '';
}

function closeSettings(): void {
  document.getElementById('settingsPopup')!.classList.remove('visible');
}

function init(): void {
  bindTabBarEvents();
  bindToolbarEvents();
  bindActionBarEvents();
  bindSettingsEvents();
  bindAccountPopup({
    name: 'Dion',
    fields: ['email', 'password'],
    load: async () => ({ ...(await window.bridge.getDionCredentials()) }),
    save: (v) => window.bridge.setDionCredentials(v.email, v.password),
    readError: 'Failed to read the session file.',
  });
  bindAccountPopup({
    name: 'Bitrix',
    fields: ['portal', 'email', 'password'],
    load: async () => ({ ...(await window.bridge.getBitrixCredentials()) }),
    save: (v) => window.bridge.setBitrixCredentials(v.portal, v.email, v.password),
    readError: 'Failed to read the account file.',
  });
  bindErrorPopup();
  bindLogEvents();
  bindHeadlessEvents();

  window.bridge.setUpstreamProxy(tm.upstreamProxy);

  window.bridge.onRelayLog((tabId: string, msg: string) => {
    tm.appendRelayLog(tabId, msg);
  });

  window.bridge.onBotError((msg: string) => {
    showError(msg);
    tm.botRunning = false;
    localStorage.setItem('botEnabled', 'false');
    renderBotButton(tm);
  });

  window.bridge.onCreateBotTab((data: BotTabData) => {
    tm.createBotTab(data);
    (document.getElementById('modeSelect') as HTMLSelectElement).value = data.mode;
    if (data.mode === TunnelMode.HeadlessVK) {
      tm.switchToHeadless(Platform.VK, data.joinTarget);
    } else if (data.mode === TunnelMode.HeadlessTelemost) {
      tm.switchToHeadless(Platform.Telemost, data.joinTarget);
    } else if (data.mode === TunnelMode.HeadlessWBStream) {
      tm.switchToHeadless(Platform.WBStream, data.joinTarget);
    } else if (data.mode === TunnelMode.HeadlessDion) {
      tm.switchToHeadless(Platform.Dion, data.joinTarget);
    } else if (data.mode === TunnelMode.HeadlessBitrix) {
      tm.switchToHeadless(Platform.Bitrix, data.joinTarget);
    } else {
      const url = data.platform === Platform.Telemost ? TELEMOST_URL : VK_IM_URL;
      loadURL(tm, url);
    }
  });

  window.bridge.onCloseBotTab((data: { tabId: string }) => {
    tm.closeTab(data.tabId);
  });

  window.bridge.onLoginRequired((tabId: string, url: string) => {
    const tab = tm.tabs[tabId];
    if (!tab) return;
    tab.headlessStatus = 'Waiting for login...';
    attachLoginWebview(tm, tabId, url);
    if (tabId === tm.activeTabId) {
      renderTabs(tm);
      renderContent(tm);
    }
  });

  window.bridge.onLoginDone((tabId: string) => {
    const tab = tm.tabs[tabId];
    if (!tab) return;
    detachLoginWebview(tm, tabId);
    tab.headlessStatus = 'Starting...';
    if (tabId === tm.activeTabId) renderContent(tm);
  });

  startHookLogPoller(tm);
  tm.autoStartBot();
  renderBotButton(tm);
}

init();

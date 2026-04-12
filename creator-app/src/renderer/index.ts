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
  exportCookies,
  copyToClipboard,
  renameTab,
} from './dom';
import { VK_IM_URL, TELEMOST_URL } from '../constants';
import { Platform, Bridge, BotTabData, LogPanel, TunnelMode } from '../types';

declare const window: Window & { bridge: Bridge };

const tm = new RendererTabManager(() => {
  renderTabs(tm);
  renderContent(tm);
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
  document.getElementById('modeSelect')!.addEventListener('change', (event) => {
    tm.setTunnelMode((event.target as HTMLSelectElement).value);
  });
  document.getElementById('btnSaveLogs')!.addEventListener('click', saveLogs);
}

function bindActionBarEvents(): void {
  document.getElementById('btnVkCookies')!.addEventListener('click', () => {
    exportCookies('.vk.com', 'vk-cookies.json', 'No VK cookies found.\nPlease log into VK first.');
  });
  document.getElementById('btnYandexCookies')!.addEventListener('click', () => {
    exportCookies(
      'yandex',
      'cookies-yandex.json',
      'No Yandex cookies found.\nPlease log into Yandex (telemost.yandex.ru) first.',
    );
  });
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
    closeSettings();
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
}

function bindHeadlessEvents(): void {
  document.getElementById('headlessJoinLink')!.addEventListener('click', (event) => {
    copyToClipboard((event.target as HTMLElement).textContent || '');
  });
  document.getElementById('headlessShortLink')!.addEventListener('click', (event) => {
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
}

function openSettings(): void {
  document.getElementById('settingsPopup')!.classList.add('visible');
  (document.getElementById('vkToken') as HTMLInputElement).value = tm.botSettings.token;
  (document.getElementById('vkGroupId') as HTMLInputElement).value = tm.botSettings.groupId;
  (document.getElementById('vkUserId') as HTMLInputElement).value = tm.botSettings.userId;
}

function closeSettings(): void {
  document.getElementById('settingsPopup')!.classList.remove('visible');
}

function init(): void {
  bindTabBarEvents();
  bindToolbarEvents();
  bindActionBarEvents();
  bindSettingsEvents();
  bindErrorPopup();
  bindLogEvents();
  bindHeadlessEvents();

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
    window.bridge.setTunnelMode(data.tabId, data.mode);
    const url = data.platform === Platform.Telemost ? TELEMOST_URL : VK_IM_URL;
    loadURL(tm, url);
  });

  window.bridge.onCloseBotTab((data: { tabId: string }) => {
    tm.closeTab(data.tabId);
  });

  startHookLogPoller(tm);
  tm.autoStartBot();
  renderBotButton(tm);
}

init();

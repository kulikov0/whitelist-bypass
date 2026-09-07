import { Platform, Bridge, LogPanel, TunnelMode, Webview } from '../types';
import { SESSION_PARTITION, HOOK_POLL_INTERVAL_MS, CALL_CREATOR_INJECT_DELAY_MS, USER_AGENT } from '../constants';
import { RendererTabManager } from './tab-manager';

declare const window: Window & { bridge: Bridge };

export function renderTabs(tm: RendererTabManager): void {
  const bar = document.getElementById('tabBar')!;
  let html = '';
  Object.keys(tm.tabs).forEach((id) => {
    const tab = tm.tabs[id];
    const label = tm.getTabLabel(tab);
    const cls = id === tm.activeTabId ? 'tab active' : 'tab';
    html +=
      `<div class="${cls}" data-tab-id="${id}">` +
      `<span class="tab-label">${escapeHtml(label)}</span>` +
      `<img class="edit" src="resources/icons8-pencil-50.png" data-action="rename">` +
      ` <span class="close" data-action="close">&#x2715;</span></div>`;
  });
  html += '<div class="tab-add" data-action="add-tab" title="New tab">+</div>';
  bar.innerHTML = html;
}

export function renderContent(tm: RendererTabManager): void {
  const content = document.getElementById('content')!;
  const welcome = document.getElementById('welcome')!;
  const toolbar = document.getElementById('toolbar')!;
  const logsPanel = document.getElementById('logsPanel')!;
  const headlessInfo = document.getElementById('headlessInfo')!;
  const hookPanel = document.getElementById('hookPanel')!;

  const webviews = content.querySelectorAll('webview');
  webviews.forEach((wv) => wv.classList.add('hidden'));

  if (!tm.activeTabId || !tm.tabs[tm.activeTabId]) {
    welcome.style.display = 'flex';
    toolbar.style.display = 'none';
    logsPanel.style.display = 'none';
    return;
  }

  welcome.style.display = 'none';

  const activeTab = tm.tabs[tm.activeTabId];
  logsPanel.style.display = activeTab.loginWebview ? 'none' : 'flex';
  hookPanel.style.display = activeTab.headless ? 'none' : 'flex';
  logsPanel.classList.toggle('relay-only', activeTab.headless === true);
  if (activeTab.loginWebview) {
    toolbar.style.display = 'none';
    headlessInfo.style.display = 'none';
    activeTab.loginWebview.classList.remove('hidden');
  } else if (activeTab.headless) {
    toolbar.style.display = 'none';
    headlessInfo.style.display = 'block';
    let title = 'VK';
    if (activeTab.platform === Platform.Telemost) title = 'Telemost';
    else if (activeTab.platform === Platform.WBStream) title = 'WBStream';
    else if (activeTab.platform === Platform.Dion) title = 'DION';
    else if (activeTab.platform === Platform.Bitrix) title = 'Bitrix';
    document.getElementById('headlessTitle')!.textContent = title;
    const startEl = document.getElementById('headlessStart')!;
    const statusEl = document.getElementById('headlessStatus')!;
    if (!activeTab.headlessStarted) {
      startEl.classList.add('visible');
      statusEl.style.display = 'none';
      const targetInput = document.getElementById('headlessStartTarget') as HTMLInputElement;
      const targetLabel = document.getElementById('headlessStartTargetLabel')!;
      const errorEl = document.getElementById('headlessStartError')!;
      targetInput.value = activeTab.headlessStartTarget || '';
      targetInput.classList.remove('invalid');
      errorEl.textContent = '';
      const dionAccountCard = document.getElementById('headlessDionAccountCard')!;
      dionAccountCard.style.display = activeTab.platform === Platform.Dion ? '' : 'none';
      const bitrixAccountCard = document.getElementById('headlessBitrixAccountCard')!;
      bitrixAccountCard.style.display = activeTab.platform === Platform.Bitrix ? '' : 'none';
      if (activeTab.platform === Platform.WBStream) {
        targetLabel.textContent = 'Paste a room id or stream.wb.ru/room/<id> link.';
        targetInput.placeholder = 'https://stream.wb.ru/room/<id>';
      } else if (activeTab.platform === Platform.Dion) {
        targetLabel.textContent = 'Paste a room id or dion.vc event link.';
        targetInput.placeholder = 'abc-def-ghi';
      } else if (activeTab.platform === Platform.Bitrix) {
        targetLabel.textContent = 'Paste a portal /video/ link, or leave empty to create a new room.';
        targetInput.placeholder = 'https://your-portal.bitrix24.ru/video/CODE';
      } else if (activeTab.platform === Platform.Telemost) {
        targetLabel.textContent = 'Paste a Telemost conference link.';
        targetInput.placeholder = 'https://telemost.yandex.ru/j/...';
      } else {
        targetLabel.textContent = 'Paste a VK call link.';
        targetInput.placeholder = 'https://vk.ru/call/join/...';
      }
    } else {
      startEl.classList.remove('visible');
      statusEl.style.display = '';
      statusEl.textContent = activeTab.headlessStatus || 'Starting...';
    }
    const callInfo = activeTab.callInfo;
    const callInfoVK = document.getElementById('headlessCallInfo')!;
    const callInfoTM = document.getElementById('headlessCallInfoTM')!;
    const callInfoWB = document.getElementById('headlessCallInfoWB')!;
    const callInfoDion = document.getElementById('headlessCallInfoDion')!;
    const callInfoBitrix = document.getElementById('headlessCallInfoBitrix')!;
    callInfoVK.style.display = 'none';
    callInfoTM.style.display = 'none';
    callInfoWB.style.display = 'none';
    callInfoDion.style.display = 'none';
    callInfoBitrix.style.display = 'none';
    if (callInfo) {
      if (activeTab.platform === Platform.WBStream) {
        callInfoWB.style.display = 'block';
        document.getElementById('headlessWBJoinLink')!.textContent = callInfo.joinLink || '';
      } else if (activeTab.platform === Platform.Telemost) {
        callInfoTM.style.display = 'block';
        document.getElementById('headlessTMJoinLink')!.textContent = callInfo.joinLink || '';
        document.getElementById('headlessTMProtocol')!.textContent = callInfo.protocol || '';
      } else if (activeTab.platform === Platform.Dion) {
        callInfoDion.style.display = 'block';
        document.getElementById('headlessDionJoinLink')!.textContent = callInfo.joinLink || '';
      } else if (activeTab.platform === Platform.Bitrix) {
        callInfoBitrix.style.display = 'block';
        document.getElementById('headlessBitrixJoinLink')!.textContent = callInfo.joinLink || '';
      } else {
        callInfoVK.style.display = 'block';
        document.getElementById('headlessJoinLink')!.textContent = callInfo.joinLink || '';
        document.getElementById('headlessTurn')!.textContent = callInfo.turn || '';
        document.getElementById('headlessProtocol')!.textContent = callInfo.protocol || '';
      }
      renderJoinQr(callInfo.joinLink || '');
    } else {
      renderJoinQr('');
    }
  } else {
    toolbar.style.display = 'flex';
    headlessInfo.style.display = 'none';
    renderJoinQr('');
    const isVK = activeTab.platform === Platform.VK && activeTab.url.includes('vk.ru');
    const modeSelect = document.getElementById('modeSelect') as HTMLSelectElement;
    const tunnelLabel = document.getElementById('tunnelLabel') as HTMLElement;
    modeSelect.style.display = isVK ? '' : 'none';
    tunnelLabel.style.display = isVK ? '' : 'none';
    modeSelect.value = activeTab.mode;
    if (activeTab.wv) activeTab.wv.classList.remove('hidden');
  }

  document.getElementById('relayLog')!.textContent = activeTab.relayLogs || '';
  document.getElementById('hookLog')!.textContent = activeTab.hookLogs || '';
  scrollLogs();
}

export function renderBotButton(tm: RendererTabManager): void {
  const btn = document.getElementById('tabBot')!;
  btn.textContent = tm.botRunning ? 'Bot: On' : 'Bot: Off';
  btn.classList.toggle('running', tm.botRunning);
}

export function scrollLogs(): void {
  const relayEl = document.getElementById('relayLog');
  const hookEl = document.getElementById('hookLog');
  if (relayEl) relayEl.scrollTop = relayEl.scrollHeight;
  if (hookEl) hookEl.scrollTop = hookEl.scrollHeight;
}

export function attachLoginWebview(tm: RendererTabManager, tabId: string, url: string): void {
  const tab = tm.tabs[tabId];
  if (!tab) return;
  if (tab.loginWebview) tab.loginWebview.remove();
  const webview = document.createElement('webview') as unknown as Webview;
  webview.setAttribute('src', url);
  webview.setAttribute('partition', SESSION_PARTITION);
  webview.setAttribute('useragent', USER_AGENT);
  webview.classList.add('webview-full');
  webview.dataset.tabId = tabId;
  document.getElementById('content')!.appendChild(webview);
  tab.loginWebview = webview;
}

export function detachLoginWebview(tm: RendererTabManager, tabId: string): void {
  const tab = tm.tabs[tabId];
  if (!tab) return;
  if (tab.loginWebview) {
    tab.loginWebview.remove();
    tab.loginWebview = undefined;
  }
}

export function loadURL(tm: RendererTabManager, url: string): void {
  if (!tm.activeTabId) return;
  const activeTab = tm.tabs[tm.activeTabId];
  if (url.includes('telemost.yandex')) {
    activeTab.platform = Platform.Telemost;
    activeTab.mode = TunnelMode.PionVideo;
  } else if (url.includes('vk.ru')) {
    activeTab.platform = Platform.VK;
  }
  window.bridge.setTunnelMode(tm.activeTabId, activeTab.mode, activeTab.platform);
  if (activeTab.wv) activeTab.wv.remove();

  const webview = document.createElement('webview') as unknown as Webview;
  webview.setAttribute('src', url);
  webview.setAttribute('partition', SESSION_PARTITION);
  webview.setAttribute('nodeintegration', '');
  webview.setAttribute('nodeintegrationinsubframes', '');
  webview.setAttribute('useragent', USER_AGENT);
  webview.classList.add('webview-full');
  webview.dataset.tabId = tm.activeTabId;
  document.getElementById('content')!.appendChild(webview);

  webview.addEventListener('dom-ready', () => {
    webview.setAudioMuted(true);
    const currentTabId = webview.dataset.tabId!;

    window.bridge.getHookCode(currentTabId, webview.getURL()).then((code: string) => {
      webview.executeJavaScript(code).catch(() => {});
    });

    window.bridge.getCallCreatorCode('call-checker.js').then((checkerCode: string) => {
      const inject = `window.__CALL_CHECKER_TAB_ID = "${currentTabId}"; ${checkerCode}`;
      webview.executeJavaScript(inject).catch(() => {});
    });

    const tabState = tm.tabs[currentTabId];
    if (tabState?.isBot) {
      setTimeout(() => {
        const scriptFile = tabState.platform === Platform.Telemost ? 'tm-call-creator.js' : 'vk-call-creator.js';
        window.bridge.getCallCreatorCode(scriptFile).then((code: string) => {
          webview.executeJavaScript(code).catch(() => {});
        });
      }, CALL_CREATOR_INJECT_DELAY_MS);
    }
  });

  webview.addEventListener('did-navigate', () => {
    activeTab.url = webview.getURL() || activeTab.url;
    renderTabs(tm);
    const currentTabId = webview.dataset.tabId!;
    webview.executeJavaScript('window.__hookInstalled = false').catch(() => {});
    window.bridge.getHookCode(currentTabId, webview.getURL()).then((code: string) => {
      webview.executeJavaScript(code).catch(() => {});
    });
    window.bridge.getCallCreatorCode('call-checker.js').then((checkerCode: string) => {
      const inject = `window.__CALL_CHECKER_TAB_ID = "${currentTabId}"; ${checkerCode}`;
      webview.executeJavaScript(inject).catch(() => {});
    });
  });

  activeTab.wv = webview;
  activeTab.url = url;
  renderTabs(tm);
  renderContent(tm);
}

export function startHookLogPoller(tm: RendererTabManager): void {
  setInterval(() => {
    if (!tm.activeTabId || !tm.tabs[tm.activeTabId]?.wv) return;
    const webview = tm.tabs[tm.activeTabId].wv!;
    webview
      .executeJavaScript(
        '(window.__hookLogs && window.__hookLogs.length) ? window.__hookLogs.splice(0) : []',
      )
      .then((logs: string[]) => {
        if (!logs.length) return;
        const el = document.getElementById('hookLog')!;
        logs.forEach((msg) => {
          if (el.textContent!.length > 0) el.textContent += '\n';
          el.textContent += msg.replace('[HOOK] ', '');
        });
        el.scrollTop = el.scrollHeight;
      })
      .catch(() => {});
  }, HOOK_POLL_INTERVAL_MS);
}

function escapeHtml(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

export function showError(msg: string): void {
  document.getElementById('errorText')!.textContent = msg;
  document.getElementById('errorPopup')!.classList.add('visible');
}

export function closeError(): void {
  document.getElementById('errorPopup')!.classList.remove('visible');
}

export function clearLog(panel: LogPanel): void {
  const id = panel === LogPanel.Relay ? 'relayLog' : 'hookLog';
  document.getElementById(id)!.textContent = '';
}

export function saveLogs(): void {
  const relay = document.getElementById('relayLog')!.textContent || '';
  const hook = document.getElementById('hookLog')!.textContent || '';
  const text = '=== RELAY ===\n' + relay + '\n\n=== HOOK ===\n' + hook;
  const blob = new Blob([text], { type: 'text/plain' });
  const anchor = document.createElement('a');
  anchor.href = URL.createObjectURL(blob);
  anchor.download = 'tunnel-logs-' + new Date().toISOString().replace(/[:.]/g, '-') + '.txt';
  anchor.click();
  URL.revokeObjectURL(anchor.href);
}

export function exportCookiesZip(): void {
  window.bridge.exportCookiesZip().then((bytes) => {
    const blob = new Blob([new Uint8Array(bytes)], { type: 'application/zip' });
    const anchor = document.createElement('a');
    anchor.href = URL.createObjectURL(blob);
    anchor.download = 'cookies.zip';
    anchor.click();
    URL.revokeObjectURL(anchor.href);
  });
}

let renderedQrText = '';

function renderJoinQr(link: string): void {
  const wrap = document.getElementById('headlessQrWrap') as HTMLElement | null;
  const image = document.getElementById('headlessQr') as HTMLImageElement | null;
  if (!wrap || !image) return;
  if (!link) {
    renderedQrText = '';
    wrap.style.display = 'none';
    image.removeAttribute('src');
    return;
  }
  if (link === renderedQrText) {
    wrap.style.display = 'block';
    return;
  }
  renderedQrText = link;
  (window as any).bridge.renderQr(link).then((dataUrl: string) => {
    if (renderedQrText !== link) return;
    if (!dataUrl) {
      wrap.style.display = 'none';
      return;
    }
    image.src = dataUrl;
    wrap.style.display = 'block';
  });
}

export function copyToClipboard(text: string): void {
  navigator.clipboard.writeText(text);
}

export function renameTab(tm: RendererTabManager, tabId: string, tabEl: HTMLElement): void {
  const span = tabEl.querySelector('.tab-label') as HTMLElement;
  if (!span) return;
  const input = document.createElement('input');
  input.type = 'text';
  input.value = tm.tabs[tabId]?.name || '';
  input.placeholder = tm.getTabLabel(tm.tabs[tabId]);
  input.className = 'tab-rename-input';
  span.replaceWith(input);
  input.focus();
  input.select();
  const done = () => {
    if (tm.tabs[tabId]) {
      tm.tabs[tabId].name = input.value.trim();
    }
    renderTabs(tm);
  };
  input.addEventListener('blur', done);
  input.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter') input.blur();
    if (ev.key === 'Escape') {
      input.value = '';
      input.blur();
    }
  });
}

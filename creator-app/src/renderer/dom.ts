import { Platform, Bridge, LogPanel, Webview } from '../types';
import { SESSION_PARTITION, HOOK_POLL_INTERVAL_MS, CALL_CREATOR_INJECT_DELAY_MS } from '../constants';
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

  const webviews = content.querySelectorAll('webview');
  webviews.forEach((wv) => wv.classList.add('hidden'));

  if (!tm.activeTabId || !tm.tabs[tm.activeTabId]) {
    welcome.style.display = 'flex';
    toolbar.style.display = 'none';
    logsPanel.style.display = 'none';
    return;
  }

  welcome.style.display = 'none';
  logsPanel.style.display = 'flex';

  const activeTab = tm.tabs[tm.activeTabId];
  if (activeTab.headless) {
    toolbar.style.display = 'none';
    headlessInfo.style.display = 'block';
    document.getElementById('headlessStatus')!.textContent = activeTab.headlessStatus || 'Starting...';
    const callInfo = activeTab.callInfo;
    const callInfoEl = document.getElementById('headlessCallInfo')!;
    if (callInfo) {
      callInfoEl.style.display = 'block';
      document.getElementById('headlessJoinLink')!.textContent = callInfo.joinLink || '';
      document.getElementById('headlessShortLink')!.textContent = callInfo.shortLink || '';
      document.getElementById('headlessTurn')!.textContent = callInfo.turn || '';
      document.getElementById('headlessProtocol')!.textContent = callInfo.protocol || '';
    } else {
      callInfoEl.style.display = 'none';
    }
    document.getElementById('headlessTunnel')!.style.display = activeTab.tunnelConnected ? 'block' : 'none';
  } else {
    toolbar.style.display = 'flex';
    headlessInfo.style.display = 'none';
    (document.getElementById('modeSelect') as HTMLSelectElement).value = activeTab.mode;
    if (activeTab.wv) activeTab.wv.classList.remove('hidden');
  }

  document.getElementById('relayLog')!.textContent = activeTab.relayLogs || '';
  document.getElementById('hookLog')!.textContent = activeTab.hookLogs || '';
  scrollLogs();

  renderBotButton(tm);
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

export function loadURL(tm: RendererTabManager, url: string): void {
  if (!tm.activeTabId) return;
  const activeTab = tm.tabs[tm.activeTabId];
  if (activeTab.wv) activeTab.wv.remove();

  const webview = document.createElement('webview') as unknown as Webview;
  webview.setAttribute('src', url);
  webview.setAttribute('partition', SESSION_PARTITION);
  webview.setAttribute('nodeintegration', '');
  webview.setAttribute('nodeintegrationinsubframes', '');
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

export function exportCookies(domain: string, filename: string, errorMsg: string): void {
  window.bridge.getCookies(domain).then((cookies) => {
    if (!cookies.length) {
      showError(errorMsg);
      return;
    }
    const simple = cookies.map((cookie) => ({ name: cookie.name, value: cookie.value }));
    const blob = new Blob([JSON.stringify(simple, null, 2)], { type: 'application/json' });
    const anchor = document.createElement('a');
    anchor.href = URL.createObjectURL(blob);
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(anchor.href);
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

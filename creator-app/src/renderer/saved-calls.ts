import { Platform, SavedCall } from '../types';
import { MAX_SAVED_CALLS } from '../constants';

const STORAGE_KEY = 'savedCalls';
const OPEN_CLASS = 'saved-calls-open';

const PLATFORM_TITLES: Record<Platform, string> = {
  [Platform.VK]: 'VK',
  [Platform.Telemost]: 'Telemost',
  [Platform.WBStream]: 'WBStream',
  [Platform.Dion]: 'DION',
  [Platform.Bitrix]: 'Bitrix',
};

export class SavedCallsView {
  private calls: SavedCall[];
  private open = false;

  constructor(private onJoin: (call: SavedCall) => void) {
    this.calls = this.load();
  }

  bindEvents(): void {
    document.getElementById('btnSavedCalls')!.addEventListener('click', () => this.toggle());
    document.getElementById('savedCallsList')!.addEventListener('click', (event) => {
      const target = event.target as HTMLElement;
      const action = target.dataset.action;
      const row = target.closest('[data-saved-target]') as HTMLElement | null;
      const savedTarget = row?.dataset.savedTarget;
      if (!savedTarget) return;
      if (action === 'join-saved') this.join(savedTarget);
      if (action === 'remove-saved') this.remove(savedTarget);
    });
  }

  isOpen(): boolean {
    return this.open;
  }

  show(): void {
    this.open = true;
    document.body.classList.add(OPEN_CLASS);
    this.render();
  }

  hide(): void {
    this.open = false;
    document.body.classList.remove(OPEN_CLASS);
  }

  has(platform: Platform, target: string): boolean {
    const trimmed = target.trim();
    return this.calls.some((entry) => entry.platform === platform && entry.target === trimmed);
  }

  remove(target: string): void {
    this.calls = this.calls.filter((entry) => entry.target !== target);
    this.persist();
    if (this.open) this.render();
  }

  add(platform: Platform, target: string, name: string): SavedCall | null {
    const trimmed = target.trim();
    if (!trimmed) return null;
    const call: SavedCall = { platform, target: trimmed, name, savedAt: Date.now() };
    const existing = this.calls.findIndex(
      (entry) => entry.platform === platform && entry.target === trimmed,
    );
    if (existing !== -1) {
      this.calls[existing] = call;
    } else {
      this.calls.unshift(call);
      if (this.calls.length > MAX_SAVED_CALLS) this.calls = this.calls.slice(0, MAX_SAVED_CALLS);
    }
    this.persist();
    if (this.open) this.render();
    return call;
  }

  private toggle(): void {
    if (this.open) {
      this.hide();
    } else {
      this.show();
    }
  }

  private join(target: string): void {
    const call = this.calls.find((entry) => entry.target === target);
    if (!call) return;
    this.hide();
    this.onJoin(call);
  }

  private render(): void {
    const list = document.getElementById('savedCallsList')!;
    const empty = document.getElementById('savedCallsEmpty')!;
    empty.style.display = this.calls.length === 0 ? 'block' : 'none';
    list.innerHTML = this.calls
      .map((call) => {
        const target = escapeHtml(call.target);
        return (
          `<div class="saved-call" data-saved-target="${target}">` +
          `<span class="saved-call-platform">${escapeHtml(PLATFORM_TITLES[call.platform])}</span>` +
          `<div class="saved-call-body">` +
          `<div class="saved-call-name">${escapeHtml(call.name)}</div>` +
          `<div class="saved-call-link">${target}</div>` +
          '</div>' +
          `<button class="saved-call-join" data-action="join-saved">Join</button>` +
          `<span class="saved-call-remove" data-action="remove-saved" title="Forget this call">&#x2715;</span>` +
          '</div>'
        );
      })
      .join('');
  }

  private load(): SavedCall[] {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return [];
    try {
      const parsed = JSON.parse(stored);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  }

  private persist(): void {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(this.calls));
  }
}

function escapeHtml(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

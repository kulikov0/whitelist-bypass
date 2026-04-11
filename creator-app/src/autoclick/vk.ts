import { SCAN_INTERVAL_MS, KICK_DELAY_MS, Selector } from '../constants';

export class VkAutoclick {
  private interval: ReturnType<typeof setInterval> | null = null;
  private wvContents: Electron.WebContents | null = null;

  attach(wvContents: Electron.WebContents): void {
    this.stop();
    this.wvContents = wvContents;
    this.interval = setInterval(() => this.scan(), SCAN_INTERVAL_MS);
    wvContents.on('destroyed', () => this.stop());
  }

  stop(): void {
    if (this.interval) {
      clearInterval(this.interval);
      this.interval = null;
    }
    this.wvContents = null;
  }

  private scan(): void {
    if (!this.wvContents) return;
    try {
      this.wvContents.mainFrame.framesInSubtree.forEach((frame) => {
        (frame
          .executeJavaScript(
            `(function() {
            var admitBtn = document.querySelector('${Selector.VK_ADMIT}');
            if (!admitBtn) return 'idle';
            var menuBtns = document.querySelectorAll('${Selector.VK_PARTICIPANT_MENU}');
            if (menuBtns.length > 1) {
              menuBtns[menuBtns.length - 1].click();
              return 'kick-open';
            }
            admitBtn.click();
            return 'admitted';
          })()`,
          ) as Promise<string>)
          .then((result) => {
            if (result ==='kick-open') {
              setTimeout(() => this.clickKick(frame), KICK_DELAY_MS);
            } else if (result ==='admitted') {
              console.log('[auto-accept] VK guest admitted');
            }
          })
          .catch(() => {});
      });
    } catch (_) {}
  }

  private clickKick(frame: Electron.WebFrameMain): void {
    (frame
      .executeJavaScript(
        `(function() {
        var btn = document.querySelector('${Selector.VK_KICK}');
        if (btn) { btn.click(); return true; }
        return false;
      })()`,
      ) as Promise<boolean>)
      .then((result) => {
        if (result) setTimeout(() => this.confirmKick(frame), KICK_DELAY_MS);
      })
      .catch(() => {});
  }

  private confirmKick(frame: Electron.WebFrameMain): void {
    (frame
      .executeJavaScript(
        `(function() {
        var btn = document.querySelector('${Selector.VK_KICK_CONFIRM}');
        if (btn) { btn.click(); return true; }
        return false;
      })()`,
      ) as Promise<boolean>)
      .then((result) => {
        if (result) console.log('[auto-accept] VK kicked previous participant');
      })
      .catch(() => {});
  }

  kickDisconnected(): void {
    if (!this.wvContents) return;
    try {
      this.wvContents.mainFrame.framesInSubtree.forEach((frame) => {
        (frame
          .executeJavaScript(
            `(function() {
            var menuBtns = document.querySelectorAll('${Selector.VK_PARTICIPANT_MENU}');
            var menuBtn = menuBtns.length > 1 ? menuBtns[menuBtns.length - 1] : null;
            if (menuBtn) { menuBtn.click(); return true; }
            return false;
          })()`,
          ) as Promise<boolean>)
          .then((result) => {
            if (result) setTimeout(() => this.clickKick(frame), KICK_DELAY_MS);
          })
          .catch(() => {});
      });
    } catch (_) {}
  }
}

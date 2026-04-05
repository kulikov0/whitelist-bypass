import { SCAN_INTERVAL_MS, KICK_DELAY_MS, Selector } from '../constants';

export class TelemostAutoclick {
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
            var buttons = document.querySelectorAll('.Orb-Button, button, [role="button"], [role="link"]');
            var admitBtn = null;
            for (var i = 0; i < buttons.length; i++) {
              var txt = buttons[i].textContent.replace(/\\s+/g, ' ').trim();
              if (txt.indexOf('\\u0412\\u043f\\u0443\\u0441\\u0442\\u0438\\u0442\\u044c') !== -1) {
                admitBtn = buttons[i];
                break;
              }
            }
            if (!admitBtn) return 'idle';
            var names = document.querySelectorAll('[class*="participantName"]');
            for (var j = 0; j < names.length; j++) {
              if (names[j].closest('[class*="selfView"]')) continue;
              var moreBtn = names[j].querySelector('${Selector.TM_MODERATION_POPUP}');
              if (moreBtn) {
                moreBtn.click();
                return 'kick-open';
              }
            }
            admitBtn.click();
            return 'admitted';
          })()`,
          ) as Promise<string>)
          .then((result) => {
            if (result ==='kick-open') {
              setTimeout(() => this.clickRemove(frame), KICK_DELAY_MS);
            } else if (result ==='admitted') {
              console.log('[auto-accept] guest admitted');
            }
          })
          .catch(() => {});
      });
    } catch (_) {}
  }

  private clickRemove(frame: Electron.WebFrameMain): void {
    (frame
      .executeJavaScript(
        `(function() {
        var el = document.querySelector('[title="\\u0423\\u0434\\u0430\\u043b\\u0438\\u0442\\u044c \\u0441\\u043e \\u0432\\u0441\\u0442\\u0440\\u0435\\u0447\\u0438"]');
        if (el) { el.click(); return true; }
        return false;
      })()`,
      ) as Promise<boolean>)
      .then((result) => {
        if (result) setTimeout(() => this.confirmRemove(frame), KICK_DELAY_MS);
      })
      .catch(() => {});
  }

  private confirmRemove(frame: Electron.WebFrameMain): void {
    (frame
      .executeJavaScript(
        `(function() {
        var modal = document.querySelector('${Selector.TM_MODAL}');
        if (!modal) return false;
        var btns = modal.querySelectorAll('button');
        for (var i = 0; i < btns.length; i++) {
          var txt = btns[i].textContent.replace(/\\s+/g, ' ').trim();
          if (txt === '\\u0423\\u0434\\u0430\\u043b\\u0438\\u0442\\u044c') {
            btns[i].click();
            return true;
          }
        }
        return false;
      })()`,
      ) as Promise<boolean>)
      .then((result) => {
        if (result) console.log('[auto-accept] kicked previous participant');
      })
      .catch(() => {});
  }
}

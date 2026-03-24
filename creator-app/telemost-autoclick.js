class TelemostAutoclick {
  constructor() {
    this._interval = null;
    this._wvContents = null;
  }

  attach(wvContents) {
    this.stop();
    this._wvContents = wvContents;
    this._interval = setInterval(() => this._scan(), 2000);
    wvContents.on('destroyed', () => this.stop());
  }

  stop() {
    if (this._interval) {
      clearInterval(this._interval);
      this._interval = null;
    }
    this._wvContents = null;
  }

  _scan() {
    try {
      this._wvContents.mainFrame.framesInSubtree.forEach((frame) => {
        frame.executeJavaScript(`
          (function() {
            var buttons = document.querySelectorAll('.Orb-Button, button, [role="button"], [role="link"]');
            var admitBtn = null;
            for (var i = 0; i < buttons.length; i++) {
              var txt = buttons[i].textContent.replace(/\\s+/g, ' ').trim();
              if (txt.indexOf('Впустить') !== -1) {
                admitBtn = buttons[i];
                break;
              }
            }
            if (!admitBtn) return 'idle';

            // Find guest's participant name 
            var names = document.querySelectorAll('[class*="participantName"]');
            for (var j = 0; j < names.length; j++) {
              if (names[j].closest('[class*="selfView"]')) continue;
              var moreBtn = names[j].querySelector('[data-testid="show-moderation-popup"]');
              if (moreBtn) {
                moreBtn.click();
                return 'kick-open';
              }
            }

            admitBtn.click();
            return 'admitted';
          })()
        `).then((r) => {
          if (r === 'kick-open') {
            setTimeout(() => this._clickRemove(frame), 500);
          } else if (r === 'admitted') {
            console.log('[auto-accept] guest admitted');
          }
        }).catch(function() {});
      });
    } catch(e) {}
  }

  _clickRemove(frame) {
    frame.executeJavaScript(`
      (function() {
        var el = document.querySelector('[title="Удалить со встречи"]');
        if (el) { el.click(); return true; }
        return false;
      })()
    `).then((r) => {
      if (r) setTimeout(() => this._confirmRemove(frame), 500);
    }).catch(function() {});
  }

  _confirmRemove(frame) {
    frame.executeJavaScript(`
      (function() {
        var modal = document.querySelector('[data-testid="orb-modal2"]');
        if (!modal) return false;
        var btns = modal.querySelectorAll('button');
        for (var i = 0; i < btns.length; i++) {
          var txt = btns[i].textContent.replace(/\\s+/g, ' ').trim();
          if (txt === 'Удалить') {
            btns[i].click();
            return true;
          }
        }
        return false;
      })()
    `).then((r) => {
      if (r) console.log('[auto-accept] kicked previous participant');
    }).catch(function() {});
  }
}

module.exports = TelemostAutoclick;

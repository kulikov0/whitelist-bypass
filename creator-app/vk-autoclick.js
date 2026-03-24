class VkAutoclick {
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
            var admitBtn = document.querySelector('[data-testid="calls_waiting_hall_promote"]');
            if (!admitBtn) return 'idle';

            // Kick current participant first if present
            var menuBtn = document.querySelector('[data-testid="calls_participant_list_item_menu_button"]');
            if (menuBtn) {
              menuBtn.click();
              return 'kick-open';
            }

            admitBtn.click();
            return 'admitted';
          })()
        `).then((r) => {
          if (r === 'kick-open') {
            setTimeout(() => this._clickKick(frame), 500);
          } else if (r === 'admitted') {
            console.log('[auto-accept] VK guest admitted');
          }
        }).catch(function() {});
      });
    } catch(e) {}
  }

  _clickKick(frame) {
    frame.executeJavaScript(`
      (function() {
        var btn = document.querySelector('[data-testid="calls_participant_actions_kick"]');
        if (btn) { btn.click(); return true; }
        return false;
      })()
    `).then((r) => {
      if (r) setTimeout(() => this._confirmKick(frame), 500);
    }).catch(function() {});
  }

  _confirmKick(frame) {
    frame.executeJavaScript(`
      (function() {
        var btn = document.querySelector('[data-testid="calls_call_kick_submit"]');
        if (btn) { btn.click(); return true; }
        return false;
      })()
    `).then((r) => {
      if (r) console.log('[auto-accept] VK kicked previous participant');
    }).catch(function() {});
  }
}

module.exports = VkAutoclick;

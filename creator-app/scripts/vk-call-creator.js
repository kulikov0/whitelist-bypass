(function() {
  if (window.__callCreatorStarted) return;
  window.__callCreatorStarted = true;

  var CALL_MENU_TRIGGER_ID = 'call-menu-trigger';
  var CALL_MENU_ID = 'call-menu';
  var CALL_IN_PROGRESS_KEY = 'call_in_progress';
  var VK_CALL_BASE = 'https://vk.com/call/join/';

  var start = function() {
    console.log("[BOT] VKCalls: DOM ready...");

    var waitAndClick = function(fn) {
      if (fn()) return;
      var observer = new MutationObserver(function() {
        if (fn()) observer.disconnect();
      });
      observer.observe(document.documentElement, { childList: true, subtree: true });
    };

    waitAndClick(function() {
      var trigger = document.getElementById(CALL_MENU_TRIGGER_ID);
      if (!trigger) return false;

      trigger.click();
      console.log("[BOT] VKCalls: opened call menu");

      waitAndClick(function() {
        var menu = document.getElementById(CALL_MENU_ID);
        var btn = menu ? menu.querySelector('button') : null;
        if (!btn) return false;

        btn.click();
        console.log("[BOT] VKCalls: created call");

        var captureLink = function(link) {
          if (window.__CALL_LINK_CAPTURED__) return;
          console.log("[BOT] VKCalls: call link:", link);
          window.__CALL_LINK__ = link;
          window.__CALL_LINK_CAPTURED__ = true;
        };

        var origFetch = window.fetch;
        window.fetch = function() {
          var args = arguments;
          return origFetch.apply(window, args).then(function(res) {
            if (window.__CALL_LINK_CAPTURED__) return res;
            try {
              var clone = res.clone();
              clone.text().then(function(text) {
                if (text.indexOf(CALL_IN_PROGRESS_KEY) === -1) return;
                var match = text.match(/"join_link"\s*:\s*"([^"]+)"/);
                if (match) captureLink(VK_CALL_BASE + match[1]);
              });
            } catch (e) {}
            return res;
          });
        };

        var pollLink = setInterval(function() {
          if (window.__CALL_LINK_CAPTURED__) { clearInterval(pollLink); return; }
          var linkBtn = document.querySelector('[data-testid="call_management_toolbar_button_link"]');
          if (!linkBtn) return;
          linkBtn.click();
          setTimeout(function() {
            var inputs = document.querySelectorAll('input[readonly], input[type="text"]');
            for (var i = 0; i < inputs.length; i++) {
              var val = inputs[i].value || '';
              if (val.indexOf('vk.com/call/join/') !== -1) {
                captureLink(val);
                clearInterval(pollLink);
                return;
              }
            }
          }, 500);
        }, 3000);

        return true;
      });
      return true;
    });
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();

(function() {
  if (window.__callCreatorStarted) return;
  window.__callCreatorStarted = true;

  var CALL_MENU_TRIGGER_ID = 'call-menu-trigger';
  var CREATE_CALL_TEXT = '\u0421\u043e\u0437\u0434\u0430\u0442\u044c \u0437\u0432\u043e\u043d\u043e\u043a \u043f\u043e';
  var CALL_IN_PROGRESS_KEY = 'call_in_progress';
  var VK_CALL_BASE = 'https://vk.com/call/join/';

  var start = function() {
    console.log("[BOT] VKCalls: DOM ready...");

    var waitAndClick = function(fn) {
      if (fn()) return;
      var observer = new MutationObserver(function() {
        if (fn()) observer.disconnect();
      });
      observer.observe(document.body, { childList: true, subtree: true });
    };

    waitAndClick(function() {
      var trigger = document.getElementById(CALL_MENU_TRIGGER_ID);
      if (!trigger) return false;

      trigger.click();
      console.log("[BOT] VKCalls: opened call menu");

      waitAndClick(function() {
        var spans = document.querySelectorAll('span');
        var el = null;
        for (var i = 0; i < spans.length; i++) {
          if (spans[i].textContent.indexOf(CREATE_CALL_TEXT) !== -1) {
            el = spans[i];
            break;
          }
        }
        var btn = el ? (el.closest('button') || el.closest('div')) : null;
        if (!btn) return false;

        btn.click();
        console.log("[BOT] VKCalls: created call");

        var origFetch = window.fetch;
        window.fetch = function() {
          var args = arguments;
          return origFetch.apply(window, args).then(function(res) {
            if (window.__CALL_LINK_CAPTURED__) return res;
            try {
              var clone = res.clone();
              clone.text().then(function(text) {
                if (text.indexOf(CALL_IN_PROGRESS_KEY) === -1) return;
                var json = JSON.parse(text);
                var items = json && json.response && json.response[1] && json.response[1].items;
                if (!items) return;
                for (var j = 0; j < items.length; j++) {
                  var join = items[j] && items[j].call_in_progress && items[j].call_in_progress.join_link;
                  if (join) {
                    var link = VK_CALL_BASE + join;
                    console.log("[BOT] VKCalls: call link:", link);
                    window.__CALL_LINK__ = link;
                    window.__CALL_LINK_CAPTURED__ = true;
                    break;
                  }
                }
              });
            } catch (e) {}
            return res;
          });
        };
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

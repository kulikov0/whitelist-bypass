(function() {
  if (window.__tmCallCreatorStarted) return;
  window.__tmCallCreatorStarted = true;

  var CREATE_BUTTON_SELECTOR = '[data-testid="create-call-button"]';
  var CALL_PATH_MARKER = '/j/';
  var CHECK_INTERVAL = 300;

  var start = function() {
    console.log("[BOT] Telemost: DOM ready...");

    var waitAndClick = function(fn) {
      if (fn()) return;
      var observer = new MutationObserver(function() {
        if (fn()) observer.disconnect();
      });
      observer.observe(document.body, { childList: true, subtree: true });
    };

    waitAndClick(function() {
      var btn = document.querySelector(CREATE_BUTTON_SELECTOR);
      if (!btn) return false;

      btn.click();
      console.log("[BOT] Telemost: creating call...");

      var check = setInterval(function() {
        var url = location.href;
        if (url.indexOf(CALL_PATH_MARKER) !== -1) {
          console.log("[BOT] Telemost: call link:", url);
          window.__CALL_LINK__ = url;
          clearInterval(check);
        }
      }, CHECK_INTERVAL);

      return true;
    });
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();

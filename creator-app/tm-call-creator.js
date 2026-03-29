(function() {
  if (window.__tmCallCreatorStarted) return;
  window.__tmCallCreatorStarted = true;

  const start = () => {
    console.log("[BOT] Telemost: DOM ready...");

    const waitAndClick = (fn) => {
      if (fn()) return;

      const observer = new MutationObserver(() => {
        if (fn()) observer.disconnect();
      });

      observer.observe(document.body, { childList: true, subtree: true });
    };

    waitAndClick(() => {
      const btn = document.querySelector('[data-testid="create-call-button"]');

      if (btn) {
        btn.click();
        console.log("[BOT] Telemost: creating call...");

        const check = setInterval(() => {
          const url = location.href;

          if (url.includes('/j/')) {
            console.log("[BOT] Telemost: call link:", url);
            window.__CALL_LINK__ = url;
            clearInterval(check);
          }
        }, 300);

        return true;
      }

      return false;
    });
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }

})();

(function() {
  if (window.__callCreatorStarted) return;
  window.__callCreatorStarted = true;

  const start = () => {
    console.log("[BOT] VKCalls: DOM ready...");

    const waitAndClick = (fn) => {
      if (fn()) return;

      const observer = new MutationObserver(() => {
        if (fn()) observer.disconnect();
      });

      observer.observe(document.body, { childList: true, subtree: true });
    };

    waitAndClick(() => {
      const trigger = document.getElementById('call-menu-trigger');

      if (trigger) {
        trigger.click();
        console.log("[BOT] VKCalls: opened call menu");

        waitAndClick(() => {
          const el = [...document.querySelectorAll('span')]
            .find(e => e.textContent.includes('Создать звонок по'));

          const btn = el?.closest('button, div');

          if (btn) {
            btn.click();
            console.log("[BOT] VKCalls: created call");

            const origFetch = window.fetch;
            window.fetch = async (...args) => {
              const res = await origFetch(...args);

              if (window.__CALL_LINK_CAPTURED__) return res;

              try {
                const clone = res.clone();
                const text = await clone.text();

                if (text.includes("call_in_progress")) {
                  const json = JSON.parse(text);
                  const items = json?.response?.[1]?.items;

                  if (items) {
                    for (const item of items) {
                      const join = item?.call_in_progress?.join_link;
                      if (join) {
                        const link = "https://vk.com/call/join/" + join;

                        console.log("[BOT] VKCalls: call link:", link);
                        window.__CALL_LINK__ = link;

                        window.__CALL_LINK_CAPTURED__ = true;
                        break;
                      }
                    }
                  }
                }
              } catch (e) {}

              return res;
            };

            return true;
          }
          return false;
        });

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
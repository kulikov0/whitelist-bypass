(function() {
  if (window.__autoclickInstalled) return;
  window.__autoclickInstalled = true;

  var log = function() {
    var args = ['[HOOK] [autoclick]'];
    for (var i = 0; i < arguments.length; i++) args.push(arguments[i]);
    console.log.apply(console, args);
  };

  log('Autoclick installed');

  function scan() {
    var all = document.querySelectorAll('[data-testid="orb-button"]');
    log('scan: ' + all.length + ' orb-buttons found');
    for (var i = 0; i < all.length; i++) {
      var txt = (all[i].textContent || '').trim();
      log('  btn[' + i + ']: "' + txt + '"');
      if (txt.indexOf('Продолжить в браузере') !== -1) {
        log('Clicking: Продолжить в браузере');
        all[i].click();
        return;
      }
    }

    var joinBtn = document.querySelector('[data-testid="enter-conference-button"]');
    if (joinBtn) {
      log('Clicking: Подключиться');
      joinBtn.click();
      clearInterval(iv);
      log('Autoclick done');
      return;
    }
  }

  var iv = setInterval(scan, 1500);
})();

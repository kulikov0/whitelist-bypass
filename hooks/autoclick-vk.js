(function() {
  if (window.__autoclickInstalled) return;
  window.__autoclickInstalled = true;
  var wasCaptchaDetected = false;

  var log = function() {
    var args = ['[HOOK] [autoclick]'];
    for (var i = 0; i < arguments.length; i++) args.push(arguments[i]);
    console.log.apply(console, args);
  };

  log('Autoclick installed');

  function findNameInput() {
    var inputs = document.querySelectorAll('input[type="text"]');
    for (var i = 0; i < inputs.length; i++) {
      var ph = (inputs[i].placeholder || '').toLowerCase();
      if (ph.indexOf('name') !== -1 || ph.indexOf('имя') !== -1) return inputs[i];
    }
    return null;
  }

  function scan() {
    var inp = findNameInput();
    if (inp && !inp.value) {
      log('Filling name');
      var set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
      set.call(inp, 'Hello');
      inp.dispatchEvent(new Event('input', { bubbles: true }));
      inp.dispatchEvent(new Event('change', { bubbles: true }));
      return;
    }

    var joinBtn = document.querySelector('[data-testid="calls_preview_join_button_anonym"]');
    if (joinBtn && inp && inp.value) {
      log('Clicking: Join');
      joinBtn.click();
      clearInterval(iv);
      log('Autoclick done');
      return;
    }
  }

  function scanForCaptcha() {
    var iframes = document.querySelectorAll("iframe");
    for (var i = 0; i < iframes.length; i++) {
      if (iframes[i].src.startsWith("https://id.vk.com/not_robot_captcha")) {
        if(!wasCaptchaDetected) {
          log('Captcha detected, user action required');
          AndroidBridge.onCaptchaDetected(false);
          wasCaptchaDetected = true;
        }
        return;
      }
    }
    if(wasCaptchaDetected) { // assuming captcha iframe wasn't found
      log('Captcha closed or solved');
      AndroidBridge.onCaptchaDetected(true);
      wasCaptchaDetected = false;
      clearInterval(captcha_iv);
      return;
    }
  }

  var iv = setInterval(scan, 1500);
  var captcha_iv = setInterval(scanForCaptcha, 1500);
})();

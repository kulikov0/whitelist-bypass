(function() {
  if (window.__autoclickInstalled) return;
  window.__autoclickInstalled = true;

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

  var iv = setInterval(scan, 1500);
})();

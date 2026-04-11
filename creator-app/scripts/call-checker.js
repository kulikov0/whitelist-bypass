(function() {
  if (window.__callCheckerStarted) return;
  window.__callCheckerStarted = true;

  var tabId = window.__CALL_CHECKER_TAB_ID || 'unknown';
  console.log('[CALL_STATUS] Checker started for ' + tabId);

  var CHECK_INTERVAL = 10000;
  var INITIAL_DELAY = 10000;

  var VK_LEAVE_SELECTOR = '[data-testid="calls_call_footer_button_leave_call"]';
  var TM_END_SELECTOR = '[data-testid="end-call-alt-button"]';

  var checkCallStatus = function() {
    var vkBtn = document.querySelector(VK_LEAVE_SELECTOR);
    var tmBtn = document.querySelector(TM_END_SELECTOR);
    var status = (vkBtn || tmBtn) ? 'active' : 'inactive';
    console.log('[CALL_STATUS] ' + tabId + ':' + status);
  };

  setTimeout(function() {
    checkCallStatus();
    setInterval(checkCallStatus, CHECK_INTERVAL);
  }, INITIAL_DELAY);
})();

(function() {
  if (window.__callCheckerStarted) return;
  window.__callCheckerStarted = true;

  var tabId = window.__CALL_CHECKER_TAB_ID || 'unknown';

  console.log('[CALL_STATUS] Checker started for ' + tabId);

  const checkCallStatus = () => {
    var vkBtn = document.querySelector('button[data-testid="calls_call_footer_button_leave_call"]') ||
                document.querySelector('[data-testid="calls_call_footer_button_leave_call"]');
    
    var tmBtn = document.querySelector('[data-testid="end-call-alt-button"]') ||
                document.querySelector('button[data-testid="end-call-alt-button"]');

    var status = (vkBtn || tmBtn) ? 'active' : 'inactive';
    
    console.log('[CALL_STATUS] ' + tabId + ':' + status);
  };
  setTimeout(function() {
    checkCallStatus();
    setInterval(checkCallStatus, 10000);
  }, 10000);
})();

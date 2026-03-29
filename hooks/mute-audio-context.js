(function() {
  var oac = window.AudioContext || window.webkitAudioContext;
  if (oac) {
    var nac = function() {
      var c = new oac();
      c.suspend();
      c.resume = function() { return Promise.resolve(); };
      return c;
    };
    nac.prototype = oac.prototype;
    window.AudioContext = nac;
    if (window.webkitAudioContext) window.webkitAudioContext = nac;
  }
})();

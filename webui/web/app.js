(function() {
  var params = new URLSearchParams(window.location.search);
  var token = params.get('token') || '';
  var es = new EventSource('/v1/subscribe?token=' + encodeURIComponent(token));
  es.onmessage = function(e) { console.log(e.data); };
})();

// Connect landing page: turns https://<server>/join.html#k=<key>&r=<rendezvous>
// into an app deep link (agentmesh://join?...). The key stays in the URL
// fragment, so it is never sent to the server in a page request.
(function () {
  var params = new URLSearchParams(location.hash.slice(1));
  var key = params.get('k');
  var rendezvous = params.get('r') || '';
  var open = document.getElementById('open');
  var code = document.getElementById('code');
  if (!key) {
    document.getElementById('error').hidden = false;
    open.hidden = true;
    return;
  }
  var q = new URLSearchParams({ u: location.origin, k: key });
  if (rendezvous) q.set('r', rendezvous);
  var deep = 'agentmesh://join?' + q.toString();
  var apk = 'https://github.com/devopshubtech/AgentMesh/releases/latest/download/agentmesh-control.apk';
  var isAndroid = /Android/i.test(navigator.userAgent);
  // Chrome on Android: intent URL falls back to the APK download if the app is missing.
  open.href = isAndroid
    ? 'intent://join?' + q.toString() + '#Intent;scheme=agentmesh;package=io.agentmesh.control;S.browser_fallback_url=' +
      encodeURIComponent(apk) + ';end'
    : deep;
  code.value = location.href;
  document.getElementById('copy').addEventListener('click', function () {
    code.select();
    if (navigator.clipboard) navigator.clipboard.writeText(code.value);
    else document.execCommand('copy');
  });
})();

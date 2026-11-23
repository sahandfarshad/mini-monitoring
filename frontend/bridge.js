// Android shell: emulate the Wails bindings (window.go / window.runtime) on top
// of the AndroidBridge JavaScript interface, so app.js runs unchanged.
(function () {
  const B = window.AndroidBridge;
  if (!B) return;
  const handlers = {}, pending = {};
  let seq = 0;
  window.__mmEmit = (name, json) => (handlers[name] || []).forEach(f => f(JSON.parse(json)));
  window.__mmResolve = (id, json) => { const p = pending[id]; delete pending[id]; if (p) p(json ? JSON.parse(json) : null); };
  window.runtime = { EventsOn: (n, f) => (handlers[n] = handlers[n] || []).push(f) };
  const call = (m, a) => new Promise(res => { const id = ++seq; pending[id] = res; B.call(id, m, JSON.stringify(a)); });
  window.go = { main: { App: new Proxy({}, { get: (_, k) => (...a) => call(k, a) }) } };
  document.documentElement.classList.add("mobile");
})();

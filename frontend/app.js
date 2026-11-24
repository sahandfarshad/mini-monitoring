// MiniMonitor UI. Talks to the Go side through window.go.main.App and
// runtime events; the display code is plain DOM so it can be reused elsewhere.
"use strict";
const $ = (s, r = document) => r.querySelector(s);
const PREVIEW = location.protocol === "file:" && !window.AndroidBridge;
const MOBILE = !!window.AndroidBridge;
const MOCK = PREVIEW ? mockApi() : null;
// resolve the Go bindings lazily: they may be injected after this script loads
const api = new Proxy({}, { get: (_, k) => (...a) => (PREVIEW ? MOCK : window.go.main.App)[k](...a) });
const on = (ev, fn) => (PREVIEW ? mockOn(ev, fn) : window.runtime.EventsOn(ev, fn));

let ui = {}, servers = [], statuses = {}, hist = {}, last = {}, tiles = {}, peak = {};
let editing = null, auth = "password";

/* ---------- formatting ---------- */
function fmtSpeed(bps) {
  if (ui.bits) {
    let v = bps * 8, u = ["b", "Kb", "Mb", "Gb"], i = 0;
    while (v >= 1000 && i < 3) { v /= 1000; i++; }
    return (v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)) + " " + u[i];
  }
  let v = bps, u = ["B", "KB", "MB", "GB"], i = 0;
  while (v >= 1024 && i < 3) { v /= 1024; i++; }
  return (v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)) + " " + u[i];
}
const fmtGB = b => (b / 1073741824).toFixed(b >= 10737418240 ? 0 : 1);

/* ---------- theme ---------- */
function applyUI() {
  const r = document.documentElement.style;
  r.setProperty("--bg", ui.bgColor); r.setProperty("--op", ui.opacity);
  r.setProperty("--rx", ui.rxColor); r.setProperty("--tx", ui.txColor);
  r.setProperty("--cpu", ui.cpuColor); r.setProperty("--ram", ui.ramColor);
  document.body.classList.toggle("notitle", !ui.showTitle);
  $("#bPin").classList.toggle("on", ui.onTop);
  $("#bPin").title = ui.onTop ? "Always on top: ON (click to let other windows cover it)" : "Always on top: OFF (click to keep it above other windows)";
  $("#bSizeV").textContent = (ui.size || "m").toUpperCase();
}
const sizeOf = s => s.size || ui.size || "m";
const scaleOf = z => ({ s: ui.scaleS, m: ui.scaleM, l: ui.scaleL }[z] || 1);

/* ---------- tiles ---------- */
function render() {
  const list = $("#list");
  list.innerHTML = ""; tiles = {};
  for (const s of servers) {
    const z = sizeOf(s);
    const el = document.createElement("div");
    el.className = "tile " + z;
    el.style.setProperty("--k", scaleOf(z));
    el.innerHTML = `
      <div class="hd"><span class="dot"></span><span class="name"></span><span class="msg"></span></div>
      <div class="body">
        <div class="chart"><div class="cv"><canvas></canvas></div><div class="spd"><span class="d">–</span><span class="u">–</span></div></div>
        <div class="sq cpu"><i class="fill"></i><span class="v">–</span><span class="l">cpu</span></div>
        <div class="sq ram"><i class="fill"></i><span class="v">–</span><span class="l">ram</span><span class="x"></span></div>
      </div>`;
    $(".name", el).textContent = s.name;
    el.title = s.host;
    el.addEventListener("contextmenu", e => { e.preventDefault(); openMenu(s, e.clientX, e.clientY); });
    el.addEventListener("dblclick", () => openDialog(s));
    list.appendChild(el);
    tiles[s.id] = { el, cv: $("canvas", el), d: $(".spd .d", el), u: $(".spd .u", el),
      cpuV: $(".cpu .v", el), cpuF: $(".cpu .fill", el), ramV: $(".ram .v", el), ramF: $(".ram .fill", el),
      ramX: $(".ram .x", el), msg: $(".msg", el) };
    paintStatus(s.id); paintStat(s.id); draw(s.id);
  }
  $("#empty").hidden = servers.length > 0;
  fit();
}

function paintStatus(id) {
  const t = tiles[id]; if (!t) return;
  const st = statuses[id] || { state: "connecting" };
  t.el.classList.remove("on", "connecting", "error", "off");
  t.el.classList.add(st.state === "online" ? "on" : st.state);
  t.msg.textContent = "";
  if (st.state === "error") {
    if (st.msg === "HOSTKEY_CHANGED") {
      t.msg.textContent = "host key changed";
      const b = document.createElement("button"); b.textContent = "trust";
      b.onclick = () => api.TrustNewHostKey(id); t.msg.appendChild(b);
    } else t.msg.textContent = shortErr(st.msg);
    t.msg.title = st.msg || "";
    t.el.title = st.msg || "";
  }
}
function shortErr(m = "") {
  if (/unable to authenticate|no supported methods/i.test(m)) return "auth failed";
  if (/timeout|i\/o timeout/i.test(m)) return "timeout";
  if (/refused/i.test(m)) return "refused";
  if (/no such host/i.test(m)) return "bad host";
  return m.length > 40 ? m.slice(0, 40) + "…" : m;
}

function paintStat(id) {
  const t = tiles[id], s = last[id]; if (!t || !s) return;
  t.d.textContent = fmtSpeed(s.rx); t.u.textContent = fmtSpeed(s.tx);
  t.cpuV.innerHTML = Math.round(s.cpu) + "<small>%</small>"; t.cpuF.style.height = Math.min(100, s.cpu) + "%";
  t.ramV.innerHTML = Math.round(s.ram) + "<small>%</small>"; t.ramF.style.height = Math.min(100, s.ram) + "%";
  t.ramX.textContent = fmtGB(s.memUsed) + "/" + fmtGB(s.memTotal) + "G";
}

function draw(id) {
  const t = tiles[id]; if (!t) return;
  const cv = t.cv, dpr = window.devicePixelRatio || 1;
  const w = cv.clientWidth, h = cv.clientHeight; if (!w || !h) return;
  if (cv.width !== Math.round(w * dpr) || cv.height !== Math.round(h * dpr)) { cv.width = Math.round(w * dpr); cv.height = Math.round(h * dpr); }
  const g = cv.getContext("2d"); g.setTransform(dpr, 0, 0, dpr, 0, 0); g.clearRect(0, 0, w, h);
  const data = hist[id] || [], n = Math.max(2, Math.ceil(ui.history / ui.interval));
  g.fillStyle = "rgba(255,255,255,0.035)"; g.fillRect(0, 0, w, h);
  if (data.length < 2) return;
  let mx = 1024; for (const p of data) mx = Math.max(mx, p.rx, p.tx);
  peak[id] = peak[id] ? peak[id] + (mx * 1.15 - peak[id]) * 0.25 : mx * 1.15;
  const top = peak[id], step = w / (n - 1), x0 = w - (data.length - 1) * step;
  const Y = v => h - 1 - (v / top) * (h - 3);
  const path = key => { g.beginPath(); data.forEach((p, i) => { const x = x0 + i * step, y = Y(p[key]); i ? g.lineTo(x, y) : g.moveTo(x, y); }); };
  const css = getComputedStyle(document.documentElement);
  const rx = css.getPropertyValue("--rx").trim(), tx = css.getPropertyValue("--tx").trim();
  // download: soft area
  path("rx"); g.lineTo(w, h); g.lineTo(x0, h); g.closePath();
  const gr = g.createLinearGradient(0, 0, 0, h); gr.addColorStop(0, rx + "55"); gr.addColorStop(1, rx + "00");
  g.fillStyle = gr; g.fill();
  path("rx"); g.strokeStyle = rx; g.lineWidth = 1.3; g.lineJoin = "round"; g.stroke();
  // upload: thin line
  path("tx"); g.strokeStyle = tx; g.lineWidth = 1.1; g.stroke();
  axes(g, w, h, top, Y, n);
}

/* ---------- chart axes ---------- */
function niceStep(raw) {
  const p = Math.pow(10, Math.floor(Math.log10(raw))), f = raw / p;
  return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * p;
}
function axisLabel(v, i) {
  const u = ui.bits ? ["b", "K", "M", "G"] : ["B", "K", "M", "G"];
  return (v >= 10 || v === 0 ? Math.round(v) : +v.toFixed(1)) + u[i];
}
function axes(g, w, h, top, Y, n) {
  const mul = ui.bits ? 8 : 1, base = ui.bits ? 1000 : 1024;
  let i = 0, v = top * mul; while (v >= base && i < 3) { v /= base; i++; }
  const lines = h >= 40 ? 3 : 2, step = niceStep(v / lines), unit = Math.pow(base, i) / mul;
  const labels = h >= 30, fs = 8;
  g.font = `600 ${fs}px "Segoe UI", system-ui, sans-serif`;
  // horizontal grid + value labels (right edge)
  g.textAlign = "right"; g.textBaseline = "bottom";
  for (let k = 1; k * step < v * 0.97; k++) {
    const y = Math.round(Y(k * step * unit)) + 0.5;
    g.strokeStyle = "rgba(255,255,255,0.07)"; g.lineWidth = 1; g.setLineDash([2, 3]);
    g.beginPath(); g.moveTo(0, y); g.lineTo(w, y); g.stroke(); g.setLineDash([]);
    if (labels && y > fs + 2) { g.fillStyle = "rgba(255,255,255,0.38)"; g.fillText(axisLabel(k * step, i), w - 3, y - 1); }
  }
  // vertical time ticks (bottom), every quarter of the span
  const span = ui.history, tstep = niceStep(span / 4);
  g.textAlign = "center"; g.textBaseline = "bottom";
  for (let s = tstep; s < span; s += tstep) {
    const x = Math.round(w - (s / span) * w) + 0.5;
    g.strokeStyle = "rgba(255,255,255,0.05)"; g.beginPath(); g.moveTo(x, 0); g.lineTo(x, h); g.stroke();
    if (labels && x > 14) { g.fillStyle = "rgba(255,255,255,0.3)"; g.fillText((s >= 60 && s % 60 === 0 ? s / 60 + "m" : s + "s"), x, h - 1); }
  }
}

/* ---------- window fitting ---------- */
let fitT, dpiK = 1, fixedK = false;
function fit() {
  if (MOBILE) { Object.keys(tiles).forEach(draw); return; }
  clearTimeout(fitT);
  fitT = setTimeout(() => {
    const els = [$("#root")]; if (!$("#menu").hidden) els.push($("#menu"));
    let w = 0, h = 0;
    for (const e of els) { const r = e.getBoundingClientRect(); w = Math.max(w, r.right); h = Math.max(h, r.bottom); }
    w = Math.ceil(w); h = Math.ceil(h);
    api.FitWindow(Math.round(w * dpiK), Math.round(h * dpiK));
    Object.keys(tiles).forEach(draw);
    // if the OS scaled the window differently than asked, correct once
    setTimeout(() => {
      const iw = window.innerWidth;
      if (iw > 0 && Math.abs(iw - w) > 3 && !fixedK) {
        fixedK = true; dpiK = dpiK * w / iw; fit();
      }
    }, 200);
  }, 30);
}

/* ---------- context menu ---------- */
function openMenu(s, x, y) {
  const m = $("#menu"), z = s.size || "";
  const item = (txt, fn, cls = "") => `<button class="${cls}" data-a="${fn}">${txt}</button>`;
  m.innerHTML =
    item("Edit", "edit") +
    "<hr>" +
    item("Size: default", "z:", z === "" ? "on" : "") +
    item("Size: small", "z:s", z === "s" ? "on" : "") +
    item("Size: medium", "z:m", z === "m" ? "on" : "") +
    item("Size: large", "z:l", z === "l" ? "on" : "") +
    "<hr>" +
    item("Move up", "up") + item("Move down", "down") +
    item(s.disabled ? "Resume" : "Pause", "toggle") +
    "<hr>" + item("Delete", "del", "danger");
  m.hidden = false;
  m.style.left = "0px"; m.style.top = "0px";
  const r = m.getBoundingClientRect();
  m.style.left = Math.max(4, Math.min(x, $("#root").getBoundingClientRect().right - r.width - 4, x)) + "px";
  m.style.top = y + "px";
  m.onclick = async e => {
    const a = e.target.closest("button")?.dataset.a; if (!a) return;
    closeMenu();
    if (a === "edit") return openDialog(s);
    if (a.startsWith("z:")) await api.SetServerSize(s.id, a.slice(2));
    if (a === "up") await api.MoveServer(s.id, -1);
    if (a === "down") await api.MoveServer(s.id, 1);
    if (a === "toggle") await api.ToggleServer(s.id);
    if (a === "del") { if (!confirm("Delete " + s.name + "?")) return; await api.DeleteServer(s.id); delete hist[s.id]; }
    await reload();
  };
  fit();
}
function closeMenu() { if (!$("#menu").hidden) { $("#menu").hidden = true; fit(); } }
document.addEventListener("mousedown", e => { if (!e.target.closest("#menu")) closeMenu(); });
document.addEventListener("keydown", e => { if (e.key === "Escape") { closeMenu(); closeSheets(); } });

/* ---------- server dialog ---------- */
function setAuth(v) {
  auth = v;
  $("#fAuth").querySelectorAll("button").forEach(b => b.classList.toggle("on", b.dataset.v === v));
  $("#lPass").hidden = v !== "password"; $("#lKey").hidden = v !== "key";
}
$("#fAuth").onclick = e => { const b = e.target.closest("button"); if (b) setAuth(b.dataset.v); };
function openDialog(s) {
  closeSheets(); editing = s || null;
  $("#dlgTitle").textContent = s ? "Edit server" : "New server";
  $("#fName").value = s?.name || ""; $("#fHost").value = s?.host || "";
  $("#fPort").value = s?.port || 22; $("#fUser").value = s?.user || "root";
  $("#fPass").value = ""; $("#fKey").value = ""; $("#fKeyPass").value = ""; $("#fIface").value = s?.iface || "";
  $("#fPass").placeholder = $("#fKey").placeholder = s?.hasSecret ? "unchanged" : "";
  setAuth(s?.auth || "password"); msg("");
  $("#dlg").hidden = false; sheeting(true); if (!MOBILE) $("#fHost").focus();
}
function input() {
  return { id: editing?.id || "", name: $("#fName").value, host: $("#fHost").value, port: +$("#fPort").value || 22,
    user: $("#fUser").value, auth, secret: auth === "key" ? $("#fKey").value : $("#fPass").value,
    pass: auth === "key" ? $("#fKeyPass").value : "", iface: $("#fIface").value };
}
function msg(t, cls = "") { const m = $("#dlgMsg"); m.textContent = t; m.className = cls; fit(); }
$("#fKeyFile").onclick = async () => { const k = await api.PickKeyFile(); if (k) $("#fKey").value = k; };
$("#dTest").onclick = async () => {
  msg("Connecting…");
  const r = await api.TestServer(input());
  r.startsWith("OK") ? msg("✓ Connected  " + r.slice(3), "good") : msg(r, "bad");
};
$("#dSave").onclick = async () => {
  const r = await api.SaveServer(input());
  if (r) return msg(r, "bad");
  closeSheets(); await reload();
};
$("#dCancel").onclick = () => closeSheets();

/* ---------- settings ---------- */
const S = { sScaleS: "scaleS", sScaleM: "scaleM", sScaleL: "scaleL", sOpacity: "opacity", sHistory: "history",
  sInterval: "interval", sRx: "rxColor", sTx: "txColor", sCpu: "cpuColor", sRam: "ramColor", sBg: "bgColor" };
function openSettings() {
  closeSheets();
  for (const [k, f] of Object.entries(S)) $("#" + k).value = ui[f];
  $("#sOnTop").checked = ui.onTop; $("#sShowTitle").checked = ui.showTitle;
  segSet("#sSize", ui.size); segSet("#sBits", ui.bits ? "1" : "0");
  $("#set").hidden = false; sheeting(true);
}
function segSet(sel, v) { $(sel).querySelectorAll("button").forEach(b => b.classList.toggle("on", b.dataset.v === v)); }
let saveT;
function saveUI(rerender) {
  applyUI(); if (rerender) render(); else fit();
  clearTimeout(saveT); saveT = setTimeout(() => api.SaveUI(ui), 250);
}
for (const [k, f] of Object.entries(S)) $("#" + k).addEventListener("input", e => {
  const el = e.target; ui[f] = el.type === "range" || el.type === "number" ? +el.value : el.value;
  if (f === "interval") ui.interval = Math.max(1, Math.min(60, ui.interval || 1));
  if (f === "history") ui.history = Math.max(10, Math.min(600, ui.history || 60));
  saveUI(f.startsWith("scale"));
});
$("#sOnTop").onchange = e => { ui.onTop = e.target.checked; saveUI(); };
$("#sShowTitle").onchange = e => { ui.showTitle = e.target.checked; saveUI(); };
$("#sSize").onclick = e => { const b = e.target.closest("button"); if (!b) return; ui.size = b.dataset.v; segSet("#sSize", ui.size); saveUI(true); };
$("#sBits").onclick = e => { const b = e.target.closest("button"); if (!b) return; ui.bits = b.dataset.v === "1"; segSet("#sBits", b.dataset.v); saveUI(true); };
$("#sDone").onclick = () => closeSheets();
function sheeting(v) { document.body.classList.toggle("sheeting", v); window.scrollTo(0, 0); fit(); }
function closeSheets() { $("#set").hidden = true; $("#dlg").hidden = true; sheeting(false); }

/* ---------- top bar ---------- */
$("#bAdd").onclick = $("#bAdd2").onclick = () => openDialog(null);
$("#bSet").onclick = () => ($("#set").hidden ? openSettings() : closeSheets());
$("#bSize").onclick = () => { ui.size = { s: "m", m: "l", l: "s" }[ui.size] || "m"; saveUI(true); };
$("#bPin").onclick = () => { ui.onTop = !ui.onTop; saveUI(); };
$("#bMin").onclick = () => api.Minimise();
$("#bClose").onclick = () => api.Quit();

/* ---------- data ---------- */
async function reload() {
  const st = await api.GetState();
  ui = st.ui; servers = st.servers || [];
  for (const x of st.statuses || []) statuses[x.id] = x;
  $("#ver").textContent = "v" + st.version;
  applyUI(); render();
}
window.addEventListener("resize", () => Object.keys(tiles).forEach(draw));
function boot() {
  if (!(PREVIEW || (window.go && window.runtime))) return setTimeout(boot, 50);
  reload().then(() => {
on("stat", s => {
  last[s.id] = s;
  const h = hist[s.id] || (hist[s.id] = []);
  h.push({ rx: s.rx, tx: s.tx });
  const n = Math.max(2, Math.ceil(ui.history / ui.interval));
  while (h.length > n) h.shift();
  paintStat(s.id); draw(s.id);
});
on("status", s => { statuses[s.id] = s; paintStatus(s.id); });
  });
}
boot();

/* ---------- browser preview (no Go backend) ---------- */
function mockApi() {
  const st = { ui: { size: "m", scaleS: 1, scaleM: 1, scaleL: 1, opacity: .92, history: 60, interval: 1, bits: true, onTop: true,
    rxColor: "#34d399", txColor: "#60a5fa", cpuColor: "#f59e0b", ramColor: "#a78bfa", bgColor: "#11151c", showTitle: true },
    servers: [{ id: "a", name: "fra-main", host: "1.1.1.1", size: "l" }, { id: "b", name: "mid-ir", host: "2.2.2.2", size: "m" },
      { id: "c", name: "nl-edge", host: "3.3.3.3", size: "s" }, { id: "d", name: "backup", host: "4.4.4.4", size: "s" }],
    statuses: [{ id: "a", state: "online" }, { id: "b", state: "online" }, { id: "c", state: "online" }, { id: "d", state: "error", msg: "dial tcp: i/o timeout" }], version: "preview" };
  const noop = async () => "";
  return { GetState: async () => JSON.parse(JSON.stringify(st)), FitWindow: noop, SaveUI: noop, SaveServer: noop, TestServer: async () => "OK eth0",
    DeleteServer: noop, MoveServer: noop, SetServerSize: noop, ToggleServer: noop, TrustNewHostKey: noop, PickKeyFile: noop, Minimise: noop, Quit: noop };
}
function mockOn(ev, fn) {
  if (ev !== "stat") return;
  const base = { a: [4e6, 6e5, 23, 41, 3.2e9, 8e9], b: [9e5, 3e5, 8, 62, 1.2e9, 2e9], c: [2e5, 8e4, 4, 30, 6e8, 2e9] };
  let t = 0;
  const tick = () => { t++;
    for (const [id, b] of Object.entries(base)) {
      const w = 1 + .6 * Math.sin(t / 5 + id.charCodeAt(0)) + .4 * Math.random();
      fn({ id, rx: b[0] * w, tx: b[1] * (2 - w * .7), cpu: Math.max(1, b[2] * w), ram: b[3] + Math.random(), memUsed: b[4], memTotal: b[5] });
    } };
  for (let i = 0; i < 60; i++) tick();
  setInterval(tick, 1000);
}

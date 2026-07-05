"use strict";

/* ==================== 工具 ==================== */
function fmtRate(bps) {
  if (!bps || bps < 1) return "0 B/s";
  if (bps < 1024) return bps.toFixed(0) + " B/s";
  if (bps < 1048576) return (bps / 1024).toFixed(1) + " KB/s";
  return (bps / 1048576).toFixed(2) + " MB/s";
}
function fmtBytes(n) {
  if (!n) return "0 B";
  if (n < 1024) return n + " B";
  if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
  if (n < 1073741824) return (n / 1048576).toFixed(1) + " MB";
  return (n / 1073741824).toFixed(2) + " GB";
}
function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}
function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}
async function postJSON(url, body) {
  return fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
}

const PROTOS = ["ftp", "sftp", "tftp"];

/* ==================== 状态渲染 ==================== */
async function loadStatus() {
  let s;
  try { s = await (await fetch("/api/status")).json(); } catch { return; }
  renderProtos(s);
  renderAuth(s.auth);
  document.getElementById("root-dir").value = s.root_config || "";
  renderWarn(s.root_warning);
  renderNics(s.nics || []);
}

function renderWarn(w) {
  const box = document.getElementById("root-warn");
  if (w) { box.textContent = "⚠ " + w; box.hidden = false; }
  else box.hidden = true;
}

function renderProtos(s) {
  const box = document.getElementById("protos");
  box.innerHTML = "";
  for (const p of PROTOS) {
    const st = s[p] || { running: false, port: 0, err: "" };
    const port = (s.ports && s.ports[p]) || st.port || 0;

    const row = el("div", "proto");
    row.appendChild(el("span", "pname", p.toUpperCase()));

    // 状态
    const state = el("span", "status " + (st.err ? "err" : st.running ? "on" : "off"));
    state.appendChild(el("i", "led"));
    state.appendChild(el("span", null, st.err ? "错误: " + st.err : st.running ? "运行中 · 端口 " + st.port : "已停止"));
    row.appendChild(state);

    // 端口输入
    const portInput = el("input", "port");
    portInput.type = "number"; portInput.value = port; portInput.title = "端口";
    portInput.onchange = async () => {
      await postJSON("/api/port", { proto: p, port: parseInt(portInput.value, 10) });
      loadStatus();
    };
    row.appendChild(portInput);

    // 开关
    const sw = el("label", "switch");
    const cb = el("input"); cb.type = "checkbox"; cb.checked = st.running;
    cb.onchange = async () => {
      cb.disabled = true;
      const action = cb.checked ? "start" : "stop";
      const r = await postJSON(`/api/proto/${p}/${action}`);
      if (!r.ok) alert(`${p} ${action} 失败: ` + (await r.text()));
      loadStatus();
    };
    sw.appendChild(cb);
    sw.appendChild(el("span", "track"));
    sw.appendChild(el("span", "thumb"));
    row.appendChild(sw);

    box.appendChild(row);
  }
}

function renderAuth(a) {
  a = a || {};
  document.getElementById("auth-user").value = a.user || "";
  document.getElementById("auth-pass").value = a.pass || "";
  document.getElementById("auth-anon").checked = !!a.anonymous;
}

function renderNics(nics) {
  const tb = document.getElementById("nics");
  tb.innerHTML = "";
  if (!nics.length) {
    const tr = el("tr"); const td = el("td", null, "未发现可用网卡"); td.colSpan = 3;
    tr.appendChild(td); tb.appendChild(tr); return;
  }
  for (const n of nics) {
    const ip = n.IP || n.ip || "";
    const tr = el("tr");
    tr.appendChild(el("td", null, n.Name || n.name || ""));
    const ipTd = el("td", null, ip);
    ipTd.title = "点击复制";
    ipTd.onclick = () => { navigator.clipboard && navigator.clipboard.writeText(ip); ipTd.textContent = ip + " ✓"; setTimeout(() => ipTd.textContent = ip, 900); };
    tr.appendChild(ipTd);
    tr.appendChild(el("td", null, n.CIDR || n.cidr || ""));
    tb.appendChild(tr);
  }
}

/* ==================== 账号 / 根目录 / 防火墙 / 全局启停 ==================== */
document.getElementById("auth-save").onclick = async () => {
  await postJSON("/api/auth", {
    user: document.getElementById("auth-user").value,
    pass: document.getElementById("auth-pass").value,
    anonymous: document.getElementById("auth-anon").checked,
  });
  loadStatus();
};
document.getElementById("root-save").onclick = async () => {
  const r = await postJSON("/api/root", { dir: document.getElementById("root-dir").value });
  if (!r.ok) alert("设置根目录失败: " + (await r.text()));
  loadStatus(); loadFiles(fmPath);
};
document.getElementById("all-start").onclick = async () => {
  for (const p of PROTOS) await postJSON(`/api/proto/${p}/start`);
  loadStatus();
};
document.getElementById("all-stop").onclick = async () => {
  for (const p of PROTOS) await postJSON(`/api/proto/${p}/stop`);
  loadStatus();
};
document.getElementById("fw-btn").onclick = async () => {
  const msg = document.getElementById("fw-msg");
  msg.hidden = false; msg.className = "msg"; msg.textContent = "正在放行...";
  const r = await postJSON("/api/firewall");
  const body = await r.json().catch(() => ({}));
  const detail = (body.detail || []).join("\n");
  if (r.ok && body.ok) { msg.className = "msg ok"; msg.textContent = "防火墙放行成功\n" + detail; }
  else { msg.className = "msg err"; msg.textContent = "放行失败（可能需以管理员运行）\n" + detail; }
};

/* ==================== 文件管理器 ==================== */
let fmPath = "";

function renderCrumbs() {
  const box = document.getElementById("fm-crumbs");
  box.innerHTML = "";
  const root = el("a", null, "根目录"); root.onclick = () => loadFiles("");
  box.appendChild(root);
  const parts = fmPath ? fmPath.split("/") : [];
  let acc = "";
  for (const part of parts) {
    acc = acc ? acc + "/" + part : part;
    box.appendChild(el("span", "sep", " / "));
    const cur = acc;
    const a = el("a", null, part); a.onclick = () => loadFiles(cur);
    box.appendChild(a);
  }
}

async function loadFiles(path) {
  fmPath = path || "";
  renderCrumbs();
  let data;
  try { data = await (await fetch("/api/files?path=" + encodeURIComponent(fmPath))).json(); }
  catch { return; }
  const tb = document.getElementById("fm-list");
  const empty = document.getElementById("fm-empty");
  tb.innerHTML = "";
  const entries = (data.entries || []).sort((a, b) => (b.is_dir - a.is_dir) || a.name.localeCompare(b.name));
  empty.hidden = entries.length > 0;
  for (const e of entries) {
    const tr = el("tr");
    const nameTd = el("td");
    const name = el("span", "fm-name" + (e.is_dir ? " dir" : ""));
    name.appendChild(el("span", "ic", e.is_dir ? "📁" : "📄"));
    name.appendChild(el("span", null, e.name));
    if (e.is_dir) name.onclick = () => loadFiles(fmPath ? fmPath + "/" + e.name : e.name);
    nameTd.appendChild(name);
    tr.appendChild(nameTd);
    tr.appendChild(el("td", "num", e.is_dir ? "—" : fmtBytes(e.size)));
    tr.appendChild(el("td", null, e.mtime || ""));

    const actTd = el("td");
    const acts = el("div", "fm-row-actions");
    const rel = fmPath ? fmPath + "/" + e.name : e.name;
    if (!e.is_dir) {
      const dl = el("button", "icon-btn", "下载"); dl.title = "下载";
      dl.onclick = () => { window.location = "/api/files/download?path=" + encodeURIComponent(rel); };
      acts.appendChild(dl);
    }
    const del = el("button", "icon-btn danger", "删除");
    del.onclick = async () => {
      if (!confirm(`确认删除 "${e.name}"？` + (e.is_dir ? "（含目录内全部内容）" : ""))) return;
      const r = await postJSON("/api/files/delete", { path: rel });
      if (!r.ok) alert("删除失败: " + (await r.text()));
      loadFiles(fmPath);
    };
    acts.appendChild(del);
    actTd.appendChild(acts);
    tr.appendChild(actTd);
    tb.appendChild(tr);
  }
}

async function uploadFiles(fileList) {
  if (!fileList || !fileList.length) return;
  const fd = new FormData();
  for (const f of fileList) fd.append("file", f, f.name);
  const r = await fetch("/api/files/upload?path=" + encodeURIComponent(fmPath), { method: "POST", body: fd });
  if (!r.ok) alert("上传失败: " + (await r.text()));
  loadFiles(fmPath);
}

document.getElementById("fm-upload-btn").onclick = () => document.getElementById("fm-file").click();
document.getElementById("fm-file").onchange = (e) => { uploadFiles(e.target.files); e.target.value = ""; };
document.getElementById("fm-mkdir").onclick = async () => {
  const name = prompt("新建文件夹名称：");
  if (!name) return;
  const rel = fmPath ? fmPath + "/" + name : name;
  const r = await postJSON("/api/files/mkdir", { path: rel });
  if (!r.ok) alert("创建失败: " + (await r.text()));
  loadFiles(fmPath);
};

const drop = document.getElementById("fm-drop");
["dragenter", "dragover"].forEach(ev => drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.add("drag"); }));
["dragleave", "drop"].forEach(ev => drop.addEventListener(ev, (e) => { e.preventDefault(); if (ev === "drop" || e.target === drop) drop.classList.remove("drag"); }));
drop.addEventListener("drop", (e) => { if (e.dataTransfer && e.dataTransfer.files) uploadFiles(e.dataTransfer.files); });

/* ==================== 当前连接 ==================== */
async function loadSessions() {
  let list;
  try { list = await (await fetch("/api/sessions")).json(); } catch { return; }
  const tb = document.getElementById("sessions");
  const empty = document.getElementById("sess-empty");
  document.getElementById("sess-count").textContent = list.length;
  tb.innerHTML = "";
  empty.hidden = list.length > 0;
  for (const s of list) {
    const tr = el("tr");
    const pt = el("td"); pt.appendChild(el("span", "pill proto-" + s.proto, s.proto.toUpperCase())); tr.appendChild(pt);
    tr.appendChild(el("td", null, s.remote || "—"));
    tr.appendChild(el("td", null, s.user || "—"));
    tr.appendChild(el("td", null, s.action || "—"));
    tr.appendChild(el("td", null, s.file || "—"));
    tr.appendChild(el("td", "num", s.bytes ? fmtBytes(s.bytes) : "—"));
    tr.appendChild(el("td", null, s.since || ""));
    tb.appendChild(tr);
  }
}

/* ==================== 日志 SSE ==================== */
function startLog() {
  const box = document.getElementById("log");
  const es = new EventSource("/api/events");
  es.onmessage = (ev) => {
    let e; try { e = JSON.parse(ev.data); } catch { return; }
    const row = el("div", "row");
    row.appendChild(el("span", "t", "[" + (e.time || "") + "] "));
    const parts = [(e.proto || "").toUpperCase(), e.user, e.action, e.path].filter(Boolean).join(" ");
    row.appendChild(document.createTextNode(parts + " "));
    row.appendChild(el("span", e.ok ? "ok" : "bad", e.ok ? "OK" : "FAIL"));
    if (e.msg) row.appendChild(document.createTextNode(" " + e.msg));
    box.appendChild(row);
    while (box.childNodes.length > 500) box.removeChild(box.firstChild);
    box.scrollTop = box.scrollHeight;
  };
}
document.getElementById("log-clear").onclick = () => { document.getElementById("log").innerHTML = ""; };

/* ==================== 性能曲线（dataviz 规范：双序列折线 + 十字准星） ==================== */
const MAXP = 60;
let upSeries = [], downSeries = [];
let hoverIdx = -1;
const canvas = document.getElementById("chart");
const ctx = canvas.getContext("2d");

function startMetrics() {
  const es = new EventSource("/api/metrics");
  es.onmessage = (ev) => {
    let m; try { m = JSON.parse(ev.data); } catch { return; }
    const hist = m.history || [];
    upSeries = hist.map(h => h.UpBps || 0);
    downSeries = hist.map(h => h.DownBps || 0);
    const snap = m.snapshot || {};
    document.getElementById("rate-up").textContent = fmtRate(snap.UpBps || 0);
    document.getElementById("rate-down").textContent = fmtRate(snap.DownBps || 0);
    drawChart();
  };
}

function chartMetrics() {
  const dpr = window.devicePixelRatio || 1;
  const cssW = canvas.clientWidth || 600, cssH = 200;
  if (canvas.width !== Math.round(cssW * dpr) || canvas.height !== Math.round(cssH * dpr)) {
    canvas.width = Math.round(cssW * dpr); canvas.height = Math.round(cssH * dpr);
  }
  return { dpr, W: cssW, H: cssH, padL: 8, padR: 16, padT: 12, padB: 8 };
}

function drawChart() {
  const { dpr, W, H, padL, padR, padT, padB } = chartMetrics();
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, W, H);
  const plotW = W - padL - padR, plotH = H - padT - padB;

  // 网格（recessive）
  ctx.strokeStyle = cssVar("--grid"); ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const y = padT + (plotH / 4) * i;
    ctx.beginPath(); ctx.moveTo(padL, y + .5); ctx.lineTo(padL + plotW, y + .5); ctx.stroke();
  }
  // 基线
  ctx.strokeStyle = cssVar("--baseline");
  ctx.beginPath(); ctx.moveTo(padL, padT + plotH + .5); ctx.lineTo(padL + plotW, padT + plotH + .5); ctx.stroke();

  const peak = Math.max(1, ...upSeries, ...downSeries);
  const xAt = (i) => padL + (plotW) * (i / (MAXP - 1));
  const yAt = (v) => padT + plotH - (v / peak) * (plotH - 4);

  drawSeries(upSeries, cssVar("--series-up"), xAt, yAt);
  drawSeries(downSeries, cssVar("--series-down"), xAt, yAt);

  // 直接标注（线端圆点，作为身份锚点；实时数值由图例显示，避免重复与裁切）
  labelEnd(upSeries, cssVar("--series-up"), xAt, yAt);
  labelEnd(downSeries, cssVar("--series-down"), xAt, yAt);

  // 十字准星
  if (hoverIdx >= 0) {
    const off = MAXP - upSeries.length;
    const idx = hoverIdx - off;
    if (idx >= 0 && idx < upSeries.length) {
      const x = xAt(hoverIdx);
      ctx.strokeStyle = cssVar("--muted"); ctx.setLineDash([3, 3]);
      ctx.beginPath(); ctx.moveTo(x + .5, padT); ctx.lineTo(x + .5, padT + plotH); ctx.stroke();
      ctx.setLineDash([]);
      dot(x, yAt(upSeries[idx]), cssVar("--series-up"));
      dot(x, yAt(downSeries[idx]), cssVar("--series-down"));
      showTip(x, idx);
    }
  } else {
    document.getElementById("chart-tip").hidden = true;
  }
}

function drawSeries(series, color, xAt, yAt) {
  if (!series.length) return;
  const off = MAXP - series.length;
  ctx.strokeStyle = color; ctx.lineWidth = 2; ctx.lineJoin = "round";
  ctx.beginPath();
  for (let i = 0; i < series.length; i++) {
    const x = xAt(off + i), y = yAt(series[i]);
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  }
  ctx.stroke();
}

function labelEnd(series, color, xAt, yAt) {
  if (!series.length) return;
  const i = series.length - 1;
  dot(xAt(MAXP - 1), yAt(series[i]), color);
}

function dot(x, y, color) {
  ctx.fillStyle = color; ctx.beginPath(); ctx.arc(x, y, 3, 0, Math.PI * 2); ctx.fill();
}

function showTip(x, idx) {
  const tip = document.getElementById("chart-tip");
  tip.hidden = false;
  const secAgo = (upSeries.length - 1 - idx);
  tip.innerHTML =
    `<div class="t">${secAgo === 0 ? "现在" : secAgo + " 秒前"}</div>` +
    `<div class="r"><span><i style="background:${cssVar("--series-up")}"></i>上行</span><b>${fmtRate(upSeries[idx])}</b></div>` +
    `<div class="r"><span><i style="background:${cssVar("--series-down")}"></i>下行</span><b>${fmtRate(downSeries[idx])}</b></div>`;
  const wrap = canvas.parentElement.getBoundingClientRect();
  let left = x + 12;
  if (left + 140 > wrap.width) left = x - 150;
  tip.style.left = Math.max(0, left) + "px";
  tip.style.top = "6px";
}

canvas.addEventListener("mousemove", (e) => {
  const rect = canvas.getBoundingClientRect();
  const { W, padL, padR } = chartMetrics();
  const plotW = W - padL - padR;
  const rx = e.clientX - rect.left - padL;
  hoverIdx = Math.round((rx / plotW) * (MAXP - 1));
  hoverIdx = Math.max(0, Math.min(MAXP - 1, hoverIdx));
  drawChart();
});
canvas.addEventListener("mouseleave", () => { hoverIdx = -1; drawChart(); });
window.addEventListener("resize", drawChart);

/* ==================== 启动 ==================== */
loadStatus();
loadFiles("");
loadSessions();
startLog();
startMetrics();
setInterval(loadStatus, 3000);
setInterval(loadSessions, 1500);
setInterval(() => loadFiles(fmPath), 5000);

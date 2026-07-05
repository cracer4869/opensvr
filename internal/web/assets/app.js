"use strict";

// ---- 工具 ----
function fmtRate(bps) {
  // bps 为字节/秒；自动 B/KB/MB 单位
  if (bps < 1024) return bps.toFixed(0) + " B/s";
  if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + " KB/s";
  return (bps / 1024 / 1024).toFixed(2) + " MB/s";
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

async function postJSON(url, body) {
  const r = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  return r;
}

const PROTOS = ["ftp", "sftp", "tftp"];

// ---- 状态渲染 ----
async function loadStatus() {
  const r = await fetch("/api/status");
  const s = await r.json();
  renderProtos(s);
  renderAuth(s.auth);
  renderRoot(s.root);
  renderNics(s.nics || []);
}

function renderProtos(s) {
  const box = document.getElementById("protos");
  box.innerHTML = "";
  for (const p of PROTOS) {
    const st = s[p] || { running: false, port: 0, err: "" };
    const port = (s.ports && s.ports[p]) || st.port || 0;

    const row = el("div", "proto");
    row.appendChild(el("span", "name", p.toUpperCase()));

    const portInput = el("input", "port");
    portInput.type = "number";
    portInput.value = port;
    portInput.title = "端口";
    portInput.onchange = async () => {
      await postJSON("/api/port", { proto: p, port: parseInt(portInput.value, 10) });
      loadStatus();
    };
    row.appendChild(portInput);

    const state = el("span", "state " + (st.err ? "err" : st.running ? "on" : "off"),
      st.err ? "错误" : st.running ? "运行 :" + st.port : "已停止");
    row.appendChild(state);

    const btn = el("button", st.running ? "on" : "", st.running ? "停止" : "启动");
    btn.onclick = async () => {
      btn.disabled = true;
      const action = st.running ? "stop" : "start";
      const resp = await postJSON(`/api/proto/${p}/${action}`);
      if (!resp.ok) {
        const t = await resp.text();
        alert(p + " " + action + " 失败: " + t);
      }
      loadStatus();
    };
    row.appendChild(btn);

    box.appendChild(row);
  }
}

function renderAuth(a) {
  a = a || {};
  document.getElementById("auth-user").value = a.user || "";
  document.getElementById("auth-pass").value = a.pass || "";
  document.getElementById("auth-anon").checked = !!a.anonymous;
}

function renderRoot(root) {
  document.getElementById("root-dir").value = root || "";
}

function renderNics(nics) {
  const tb = document.querySelector("#nics tbody");
  tb.innerHTML = "";
  for (const n of nics) {
    const tr = el("tr");
    tr.appendChild(el("td", null, n.Name || n.name || ""));
    tr.appendChild(el("td", null, n.IP || n.ip || ""));
    tr.appendChild(el("td", null, n.CIDR || n.cidr || ""));
    tb.appendChild(tr);
  }
  if (!nics.length) {
    const tr = el("tr");
    const td = el("td", null, "未发现可用网卡");
    td.colSpan = 3;
    tr.appendChild(td);
    tb.appendChild(tr);
  }
}

// ---- 账号/根目录/防火墙 ----
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
  loadStatus();
};

document.getElementById("fw-btn").onclick = async () => {
  const msg = document.getElementById("fw-msg");
  msg.textContent = "正在放行...";
  msg.className = "msg";
  const r = await postJSON("/api/firewall");
  const body = await r.json().catch(() => ({}));
  const detail = (body.detail || []).join("\n");
  if (r.ok && body.ok) {
    msg.className = "msg ok";
    msg.textContent = "防火墙放行成功\n" + detail;
  } else {
    msg.className = "msg err";
    msg.textContent = "放行失败（可能需要以管理员运行）\n" + detail;
  }
};

// ---- 日志 SSE ----
function startLog() {
  const box = document.getElementById("log");
  const es = new EventSource("/api/events");
  es.onmessage = (ev) => {
    let e;
    try { e = JSON.parse(ev.data); } catch { return; }
    const row = el("div", "row");
    row.appendChild(el("span", "t", "[" + (e.time || "") + "] "));
    row.appendChild(el("span", "p", (e.proto || "").toUpperCase() + " "));
    const parts = [e.user, e.action, e.path].filter(Boolean).join(" ");
    row.appendChild(document.createTextNode(parts + " "));
    row.appendChild(el("span", e.ok ? "ok" : "bad", e.ok ? "OK" : "FAIL"));
    if (e.msg) row.appendChild(document.createTextNode(" " + e.msg));
    box.appendChild(row);
    while (box.childNodes.length > 500) box.removeChild(box.firstChild);
    box.scrollTop = box.scrollHeight;
  };
}

// ---- 性能曲线 SSE + Canvas ----
const MAXP = 60;
let upSeries = [];
let downSeries = [];

function startMetrics() {
  const es = new EventSource("/api/metrics");
  es.onmessage = (ev) => {
    let m;
    try { m = JSON.parse(ev.data); } catch { return; }
    const hist = m.history || [];
    upSeries = hist.map((h) => h.UpBps || 0);
    downSeries = hist.map((h) => h.DownBps || 0);
    const snap = m.snapshot || {};
    document.getElementById("rate-up").textContent = fmtRate(snap.UpBps || 0);
    document.getElementById("rate-down").textContent = fmtRate(snap.DownBps || 0);
    drawChart();
  };
}

function drawChart() {
  const canvas = document.getElementById("chart");
  const ctx = canvas.getContext("2d");
  const W = canvas.width, H = canvas.height;
  ctx.clearRect(0, 0, W, H);

  // 背景网格（任务管理器风格）
  ctx.strokeStyle = "rgba(255,255,255,0.06)";
  ctx.lineWidth = 1;
  for (let i = 1; i < 5; i++) {
    const y = (H / 5) * i;
    ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(W, y); ctx.stroke();
  }
  for (let i = 1; i < 10; i++) {
    const x = (W / 10) * i;
    ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, H); ctx.stroke();
  }

  const peak = Math.max(1, ...upSeries, ...downSeries);

  drawLine(ctx, upSeries, peak, W, H, "#4da3ff");
  drawLine(ctx, downSeries, peak, W, H, "#47d17c");
}

function drawLine(ctx, series, peak, W, H, color) {
  if (!series.length) return;
  const n = MAXP;
  const step = W / (n - 1);
  ctx.strokeStyle = color;
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  // 右对齐：最新点在最右侧，向左滚动
  const offset = n - series.length;
  for (let i = 0; i < series.length; i++) {
    const x = (offset + i) * step;
    const y = H - (series[i] / peak) * (H - 6) - 3;
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  }
  ctx.stroke();
}

// ---- 启动 ----
loadStatus();
setInterval(loadStatus, 3000);
startLog();
startMetrics();

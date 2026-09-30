// Острова графиков. Страница уже свёрстана сборкой: текст, таблицы, легенды и переключатели на месте,
// у контейнеров задана высота. Скрипт только рисует SVG в [data-chart] и вешает обработчики.
// Подписи и данные вшиты в страницу сборкой: <script id="gb-labels"> и <script id="gb-data">, запросов за ними нет.

const L = JSON.parse(document.getElementById("gb-labels")?.textContent || "{}");
const D = JSON.parse(document.getElementById("gb-data")?.textContent || "{\"configs\":[]}");
const t = k => L[k] ?? k;
const LANG = document.documentElement.lang || "ru";
const ORDER = ["a", "p", "b2", "b"];
const NS = "http://www.w3.org/2000/svg";
const COLOR = v => `var(--s-${v})`;
const NAME = v => t(`variant.${v}`);
const BUILD = b => t(`build.${b}`);

const fmtNum = (v, d = 1) => v.toLocaleString(LANG, { maximumFractionDigits: d });
const fmtMs = v => v >= 1000 ? (v / 1000).toLocaleString(LANG, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + " s"
  : v >= 10 ? fmtNum(v, 0) + " ms" : v >= 1 ? fmtNum(v, 1) + " ms" : fmtNum(v, 2) + " ms";
const fmtCount = v => LANG === "ru"
  ? (v >= 1e6 ? fmtNum(v / 1e6, 1) + " млн" : v >= 1e3 ? fmtNum(v / 1e3, 0) + " тыс." : fmtNum(v, 0))
  : (v >= 1e6 ? fmtNum(v / 1e6, 1) + "M" : v >= 1e3 ? fmtNum(v / 1e3, 0) + "K" : fmtNum(v, 0));

// ---------- подсказка ----------
const tip = Object.assign(document.createElement("div"), { className: "tip", role: "tooltip" });
document.body.append(tip);
function showTip(e, html) {
  tip.innerHTML = html;
  tip.style.opacity = 1;
  const r = tip.getBoundingClientRect();
  let x = e.clientX + 14, y = e.clientY + 14;
  if (x + r.width > innerWidth - 8) x = e.clientX - r.width - 14;
  if (y + r.height > innerHeight - 8) y = e.clientY - r.height - 14;
  tip.style.left = x + "px";
  tip.style.top = y + "px";
}
const hideTip = () => { tip.style.opacity = 0; };

// ---------- svg ----------
function el(tag, attrs = {}, parent) {
  const n = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v);
  if (parent) parent.appendChild(n);
  return n;
}
function scale(kind, [d0, d1], [r0, r1]) {
  if (kind === "log") {
    const l0 = Math.log10(d0), l1 = Math.log10(d1);
    const f = v => r0 + (Math.log10(v) - l0) / (l1 - l0) * (r1 - r0);
    f.ticks = () => { const out = []; for (let e = Math.ceil(l0); e <= Math.floor(l1); e++) out.push(10 ** e); return out; };
    return f;
  }
  const f = v => r0 + (v - d0) / (d1 - d0) * (r1 - r0);
  f.ticks = () => {
    const span = d1 - d0, mag = 10 ** Math.floor(Math.log10(span / 5));
    const step = [1, 2, 2.5, 5, 10].map(m => m * mag).find(s => span / s <= 6);
    const out = []; for (let v = Math.ceil(d0 / step) * step; v <= d1 + 1e-9; v += step) out.push(+v.toFixed(10));
    return out;
  };
  return f;
}
const logDomain = (lo, hi) => [10 ** Math.floor(Math.log10(lo)), 10 ** Math.ceil(Math.log10(hi))];

// Высота берётся у контейнера: её задала сборка, график в неё вписывается и ничего не сдвигает.
function frame(host, { left = 64, right = 24, top = 16, bottom = 48 } = {}) {
  host.querySelector("svg")?.remove();
  const W = Math.max(300, host.clientWidth), H = host.clientHeight;
  const svg = el("svg", { viewBox: `0 0 ${W} ${H}`, role: "img" }, host);
  return { svg, W, H, x0: left, x1: W - right, y0: H - bottom, y1: top };
}
function axes(f, sx, sy, { xTicks, yTicks, xFmt, yFmt, xTitle, yTitle }) {
  const g = el("g", { class: "axis" }, f.svg);
  for (const v of yTicks) {
    const y = sy(v);
    el("line", { x1: f.x0, x2: f.x1, y1: y, y2: y, class: "gridl" }, g);
    el("text", { x: f.x0 - 8, y: y + 4, "text-anchor": "end" }, g).textContent = yFmt(v);
  }
  for (const v of xTicks) {
    const x = sx(v);
    el("line", { x1: x, x2: x, y1: f.y0, y2: f.y1, class: "gridl" }, g);
    el("text", { x, y: f.y0 + 18, "text-anchor": "middle" }, g).textContent = xFmt(v);
  }
  el("line", { x1: f.x0, x2: f.x1, y1: f.y0, y2: f.y0, class: "base" }, g);
  if (xTitle) el("text", { x: (f.x0 + f.x1) / 2, y: f.H - 8, "text-anchor": "middle", class: "axis-title" }, g).textContent = xTitle;
  if (yTitle) el("text", { transform: `translate(14 ${(f.y0 + f.y1) / 2}) rotate(-90)`, "text-anchor": "middle", class: "axis-title" }, g).textContent = yTitle;
}
function dot(parent, x, y, color, r, html, hollow = false) {
  const g = el("g", {}, parent);
  el("circle", { cx: x, cy: y, r: r + 2, style: "fill:var(--surface)" }, g);
  el("circle", { cx: x, cy: y, r, style: hollow ? `fill:var(--surface);stroke:${color};stroke-width:2` : `fill:${color}` }, g);
  const hit = el("circle", { cx: x, cy: y, r: Math.max(12, r + 6), style: "fill:transparent" }, g);
  if (html) {
    hit.addEventListener("pointermove", e => showTip(e, html));
    hit.addEventListener("pointerleave", hideTip);
  }
}

// ---------- данные ----------
const find = (D, mode, v, build, n = 10_000_000) => D.configs.find(c => c.mode === mode && c.variant === v && c.build === build && c.n === n);
const tipHtml = c => `<b>${NAME(c.variant)}</b><br><span class="mono">${t(`ptrs.${c.variant}`)}</span><br>${BUILD(c.build)} · ${fmtCount(c.n)} ${t("ui.records")}<br>
  mark CPU: <span class="mono">${fmtMs(c.mark_cpu_ms.median)}</span> (${fmtMs(c.mark_cpu_ms.min)}…${fmtMs(c.mark_cpu_ms.max)})<br>
  ${t("chart.mark_clock").replace(", ms", "")}: <span class="mono">${fmtMs(c.mark_clock_ms.median)}</span><br>
  ${t("chart.heap_objects")}: <span class="mono">${fmtCount(c.objects)}</span> · ${t("ui.heap")} ${fmtNum(c.live_mb.median, 0)} MB`;

// ---------- графики ----------
const charts = {
  hero(host, D, st) {
    const rows = ORDER.map(v => find(D, "timer", v, "green")).filter(c => c && c.gcs);
    const narrow = host.clientWidth < 560;
    const f = frame(host, { left: narrow ? 12 : 220, right: narrow ? 20 : 310, top: narrow ? 20 : 8, bottom: 44 });
    const max = Math.max(...rows.map(r => r.mark_clock_ms.median));
    const log = st.scale !== "lin";
    const sx = log ? scale("log", logDomain(0.5, max), [f.x0, f.x1]) : scale("lin", [0, max * 1.05], [f.x0, f.x1]);
    const band = (f.y0 - f.y1) / rows.length;
    axes(f, sx, () => 0, { xTicks: sx.ticks(), yTicks: [], xFmt: v => fmtNum(v, log ? 1 : 0), yFmt: () => "", xTitle: t("chart.hero_axis") });
    rows.forEach((c, i) => {
      const yc = f.y1 + band * i + band / 2 + (narrow ? 8 : 0);
      const v = c.mark_clock_ms.median;
      const x0 = log ? sx(logDomain(0.5, max)[0]) : sx(0);
      const w = Math.max(2, sx(v) - x0), h = 22, r = Math.min(4, w / 2);
      el("path", { d: `M${x0},${yc - h / 2} h${w - r} a${r},${r} 0 0 1 ${r},${r} v${h - 2 * r} a${r},${r} 0 0 1 ${-r},${r} h${-(w - r)} z`, style: `fill:${COLOR(c.variant)}`, "aria-label": `${NAME(c.variant)}: ${fmtMs(v)}` }, f.svg);
      const hit = el("rect", { x: x0, y: yc - band / 2, width: Math.max(w, 60), height: band, style: "fill:transparent" }, f.svg);
      hit.addEventListener("pointermove", e => showTip(e, tipHtml(c)));
      hit.addEventListener("pointerleave", hideTip);
      el("text", { x: narrow ? f.x0 : f.x0 - 12, y: narrow ? yc - h / 2 - 6 : yc + 4, "text-anchor": narrow ? "start" : "end", class: "lab-2" }, f.svg).textContent = NAME(c.variant);
      el("text", { x: x0 + w + 8, y: yc + 4, class: "lab" }, f.svg).textContent = fmtMs(v) +
        (narrow ? "" : ` (${fmtMs(c.mark_clock_ms.min)}…${fmtMs(c.mark_clock_ms.max)}) · ${fmtCount(c.objects)} ${t("ui.objects")}`);
    });
  },

  mb(host, D) { predictor(host, D, c => Math.max(1, c.live_mb.median), v => fmtNum(v, 0), t("chart.live_mb")); },
  obj(host, D) { predictor(host, D, c => c.objects, fmtCount, t("chart.heap_objects")); },

  scaling(host, D, st) {
    const all = D.configs.filter(c => c.mode === "forced");
    const f = frame(host);
    const xs = all.map(c => c.n), ys = all.map(c => c.mark_cpu_ms.median);
    const sx = scale("log", logDomain(Math.min(...xs), Math.max(...xs)), [f.x0, f.x1]);
    const sy = scale("log", logDomain(Math.min(...ys), Math.max(...ys)), [f.y0, f.y1]);
    axes(f, sx, sy, { xTicks: sx.ticks(), yTicks: sy.ticks(), xFmt: fmtCount, yFmt: v => fmtNum(v, 1), xTitle: t("chart.entries"), yTitle: t("chart.mark_cpu") });
    const build = st.build || "green";
    for (const v of ORDER) {
      const pts = all.filter(c => c.variant === v && c.build === build).sort((a, b) => a.n - b.n);
      if (!pts.length) continue;
      el("path", { d: pts.map((c, i) => `${i ? "L" : "M"}${sx(c.n)},${sy(c.mark_cpu_ms.median)}`).join(""), style: `fill:none;stroke:${COLOR(v)};stroke-width:1.25;stroke-opacity:.45;stroke-linejoin:round;stroke-linecap:round` }, f.svg);
      for (const c of pts) dot(f.svg, sx(c.n), sy(c.mark_cpu_ms.median), COLOR(v), 4, tipHtml(c));
      const last = pts[pts.length - 1];
      if (f.W > 640 && (v === "a" || v === "b")) el("text", { x: sx(last.n) - 10, y: sy(last.mark_cpu_ms.median) - 10, "text-anchor": "end", class: "lab" }, f.svg).textContent = fmtMs(last.mark_cpu_ms.median);
    }
  },

  timeline(host, D, st) {
    const metric = st.metric || "mark";
    const timer = D.configs.filter(c => c.mode === "timer" && c.gcs > 0);
    const pts = timer.flatMap(c => c.raw.flatMap((r, run) => r.gcs.map((g, i) => ({ c, run: run + 1, g, warm: i === 0 }))));
    const f = frame(host);
    const sx = scale("lin", [0, 720], [f.x0, f.x1]);
    const vals = pts.map(p => Math.max(p.g[metric], 0.01));
    const sy = scale("log", logDomain(Math.min(...vals), Math.max(...vals)), [f.y0, f.y1]);
    axes(f, sx, sy, { xTicks: [0, 120, 240, 360, 480, 600, 720], yTicks: sy.ticks(), xFmt: v => v + " s", yFmt: v => fmtNum(v, 2),
      xTitle: t("chart.seconds"), yTitle: metric === "mark" ? t("chart.mark_clock") : t("chart.mark_cpu") });
    for (const x of [120, 240, 360, 480, 600]) el("line", { x1: sx(x), x2: sx(x), y1: f.y0, y2: f.y1, style: "stroke:var(--axis);stroke-dasharray:2 4" }, f.svg);
    for (const { c, run, g, warm } of pts) {
      const html = `<b>${NAME(c.variant)}</b><br>${BUILD(c.build)} · ${t("chart.run")} ${run} · gc ${g.num}${warm ? ` (${t("chart.warmup")})` : ""}<br>
        @<span class="mono">${fmtNum(g.at, 1)} s</span><br>${t("chart.mark_clock").replace(", ms", "")}: <span class="mono">${fmtMs(g.mark)}</span><br>
        mark CPU: <span class="mono">${fmtMs(g.mark_cpu)}</span> (${t("chart.bg")} ${fmtMs(g.bg)}, idle ${fmtMs(g.idle)})`;
      dot(f.svg, sx(Math.min(g.at, 720)), sy(Math.max(g[metric], 0.01)), COLOR(c.variant), 4.5, html, c.build === "nogreen");
    }
  },

  green(host, D) {
    const groups = [];
    for (const v of ["a", "b"]) {
      const g = find(D, "timer", v, "green"), n = find(D, "timer", v, "nogreen");
      if (!g || !n || !g.gcs || !n.gcs) continue;
      groups.push([v, t("chart.by_clock"), "mark_clock_ms", g, n], [v, t("chart.cpu"), "mark_cpu_ms", g, n]);
    }
    const f = frame(host, { bottom: 56 });
    const all = groups.flatMap(([, , k, g, n]) => [g[k].min, g[k].max, n[k].min, n[k].max]);
    const sy = scale("log", logDomain(Math.min(...all), Math.max(...all)), [f.y0, f.y1]);
    const band = (f.x1 - f.x0) / groups.length;
    axes(f, () => 0, sy, { xTicks: [], yTicks: sy.ticks(), xFmt: String, yFmt: v => fmtNum(v, 1), yTitle: t("chart.mark") });
    groups.forEach(([v, label, k, g, n], i) => {
      const cx = f.x0 + band * i + band / 2;
      el("text", { x: cx, y: f.y0 + 20, "text-anchor": "middle", class: "lab-2" }, f.svg).textContent = `${NAME(v).split(" · ")[f.W > 620 ? 1 : 0]}, ${label}`;
      [g, n].forEach((c, j) => {
        const s = c[k], x = cx + (j ? 16 : -16), col = COLOR(v);
        el("line", { x1: x, x2: x, y1: sy(s.min), y2: sy(s.max), style: `stroke:${col};stroke-width:3;stroke-linecap:round;opacity:${j ? .55 : 1}` }, f.svg);
        const html = `<b>${NAME(v)}</b><br>${BUILD(c.build)} · ${t("chart.timer_gcs")} · ${c.runs} ${t("chart.runs")}, ${c.gcs} ${t("chart.gcs")}<br>
          mark ${label}: <span class="mono">${fmtMs(s.median)}</span> (${fmtMs(s.min)}…${fmtMs(s.max)})`;
        dot(f.svg, x, sy(s.median), col, 5, html, j === 1);
        if (f.W > 620) el("text", { x: x + (j ? 12 : -12), y: sy(s.median) + 4, "text-anchor": j ? "start" : "end", class: "lab" }, f.svg).textContent = fmtMs(s.median);
      });
    });
  },
};

function predictor(host, D, key, fmt, title) {
  const rows = D.configs.filter(c => c.mode === "forced" && c.build === "green" && c.objects > 0);
  const f = frame(host, { left: 56 });
  const sx = scale("log", logDomain(Math.min(...rows.map(key)), Math.max(...rows.map(key))), [f.x0, f.x1]);
  const ys = rows.map(c => c.mark_cpu_ms.median);
  const sy = scale("log", logDomain(Math.min(...ys), Math.max(...ys)), [f.y0, f.y1]);
  axes(f, sx, sy, { xTicks: sx.ticks(), yTicks: sy.ticks(), xFmt: fmt, yFmt: v => fmtNum(v, 1), xTitle: title, yTitle: t("chart.mark_cpu") });
  for (const c of rows) dot(f.svg, sx(key(c)), sy(c.mark_cpu_ms.median), COLOR(c.variant), 4.5, tipHtml(c));
}

// ---------- запуск ----------
// Состояние переключателей у каждого острова своё: data-scale, data-metric, data-build на кнопках.
const hosts = [...document.querySelectorAll("[data-chart]")];
const state = new Map(hosts.map(h => [h, {}]));

function draw(host) {
  if (!host.offsetParent) return; // внутри закрытого <details>: ширины нет, нарисуем при раскрытии
  charts[host.dataset.chart](host, D, state.get(host));
}
const drawAll = () => hosts.forEach(h => { try { draw(h); } catch (err) { console.error(h.dataset.chart, err); } });

for (const island of document.querySelectorAll("[data-island]")) {
  const inner = [...island.querySelectorAll("[data-chart]")];
  island.querySelectorAll(".seg button").forEach(b => b.addEventListener("click", () => {
    b.parentElement.querySelectorAll("button").forEach(x => x.setAttribute("aria-pressed", x === b));
    const [k, v] = Object.entries(b.dataset)[0];
    for (const h of inner) { state.get(h)[k] = v; draw(h); }
  }));
}
let resizeTimer;
addEventListener("resize", () => { clearTimeout(resizeTimer); resizeTimer = setTimeout(drawAll, 150); });
document.querySelectorAll("details").forEach(d => d.addEventListener("toggle", drawAll));
drawAll();

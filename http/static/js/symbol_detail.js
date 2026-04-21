// Per-symbol detail page. Two live panels driven by NATS:
//
//   1. Time-series — rolling 60s window of bid/ask over time.
//   2. Distribution — histogram of recent mid prices, with vertical
//      rules for the *current* bid and ask, and any analytic posterior
//      overlays published to `godxfeed.analytics.<type>.<SYMBOL>`.
//
// Subjects subscribed:
//   godxfeed.quote.<SYMBOL>        → Quote events (raw dxFeed JSON)
//   godxfeed.analytics.*.<SYMBOL>  → {type, symbol, xs, ys, at}
//
// Both subscriptions are feature-independent: if no analytics sidecar
// is running, the distribution panel still renders the empirical
// histogram + bid/ask rules. The posterior overlay simply appears
// once a sidecar starts publishing.
//
// Nothing here mutates service-side state — per the two-layer
// subscription model in the README, this page is a pure NATS client.

import {
  StringCodec,
  connect,
  tokenAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";
import {
  Observable,
  filter,
  map,
} from "https://esm.sh/rxjs@7.8.1";
import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";

// ─────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────

const TS_WINDOW_MS  = 60_000;    // 60s scrolling window for time series
const DIST_BUFFER   = 150;       // number of recent mids kept for histogram
const BIN_COUNT     = 24;
const RENDER_MS     = 200;
const MARGIN        = { top: 10, right: 14, bottom: 26, left: 52 };

// ─────────────────────────────────────────────────────────────────────
// NATS → Observable bridge (same shape as admin_plots.js)
// ─────────────────────────────────────────────────────────────────────

function natsToObservable(nc, subject, decoder) {
  return new Observable((observer) => {
    let cancelled = false;
    const sub = nc.subscribe(subject);
    (async () => {
      for await (const m of sub) {
        if (cancelled) return;
        try {
          observer.next(JSON.parse(decoder.decode(m.data)));
        } catch { /* drop malformed */ }
      }
      observer.complete();
    })();
    return () => { cancelled = true; sub.unsubscribe(); };
  });
}

// ─────────────────────────────────────────────────────────────────────
// Time-series panel
// ─────────────────────────────────────────────────────────────────────

function createTimeSeriesPanel(container) {
  const svg = d3.select(container).append("svg");
  const resize = () => {
    const w = container.clientWidth || 800;
    const h = 260; // match dist panel so the two charts align vertically
    svg.attr("viewBox", `0 0 ${w} ${h}`).attr("width", w).attr("height", h);
    return { w, h };
  };
  let { w, h } = resize();
  window.addEventListener("resize", () => ({ w, h } = resize()));

  const g = svg.append("g").attr("transform", `translate(${MARGIN.left},${MARGIN.top})`);
  const gx = g.append("g").attr("class", "xaxis");
  const gy = g.append("g").attr("class", "yaxis");
  const spreadArea = g.append("path").attr("class", "ts-spread-area");
  const bidLine = g.append("path").attr("class", "ts-bid-line");
  const askLine = g.append("path").attr("class", "ts-ask-line");

  function update(points) {
    if (points.length < 2) return;
    const innerW = Math.max(50, w - MARGIN.left - MARGIN.right);
    const innerH = Math.max(50, h - MARGIN.top - MARGIN.bottom);

    gx.attr("transform", `translate(0,${innerH})`);

    const now = Date.now();
    const x = d3.scaleTime().domain([now - TS_WINDOW_MS, now]).range([0, innerW]);
    const all = points.flatMap((d) => [d.bid, d.ask]);
    const lo = d3.min(all), hi = d3.max(all);
    const pad = hi === lo ? Math.max(1e-6, Math.abs(hi) * 1e-6) : (hi - lo) * 0.08;
    const y = d3.scaleLinear().domain([lo - pad, hi + pad]).range([innerH, 0]);

    gx.call(d3.axisBottom(x).ticks(6).tickSizeOuter(0));
    gy.call(d3.axisLeft(y).ticks(5).tickSizeOuter(0));

    const mkLine = (key) => d3.line().x((d) => x(d.ts)).y((d) => y(d[key])).curve(d3.curveStepAfter);
    const mkArea = d3.area()
      .x((d) => x(d.ts))
      .y0((d) => y(d.bid))
      .y1((d) => y(d.ask))
      .curve(d3.curveStepAfter);

    bidLine.datum(points).attr("d", mkLine("bid"));
    askLine.datum(points).attr("d", mkLine("ask"));
    spreadArea.datum(points).attr("d", mkArea);
  }

  return { update };
}

// ─────────────────────────────────────────────────────────────────────
// Distribution panel — histogram of mids + bid/ask rules + overlays
// ─────────────────────────────────────────────────────────────────────

function createDistPanel(container, symbol) {
  const svg = d3.select(container).append("svg");
  const resize = () => {
    const w = container.clientWidth || 800;
    const h = 260;
    svg.attr("viewBox", `0 0 ${w} ${h}`).attr("width", w).attr("height", h);
    return { w, h };
  };
  let { w, h } = resize();
  window.addEventListener("resize", () => ({ w, h } = resize()));

  const g = svg.append("g").attr("transform", `translate(${MARGIN.left},${MARGIN.top})`);
  const gx = g.append("g").attr("class", "xaxis");
  const gy = g.append("g").attr("class", "yaxis");

  const clipId = `dist-clip-${symbol.replace(/[^A-Za-z0-9_-]/g, "_")}`;
  svg.append("defs").append("clipPath").attr("id", clipId)
    .append("rect").attr("class", "clip-rect");
  const plot = g.append("g").attr("clip-path", `url(#${clipId})`);

  const areaPath  = plot.append("path").attr("class", "fillarea");
  const linePath  = plot.append("path").attr("class", "dens-line");
  const barsG     = plot.append("g").attr("class", "bars");
  const overlaysG = plot.append("g").attr("class", "overlays");
  const rulesG    = plot.append("g").attr("class", "quote-rules");

  function update(state) {
    const { mids, latest, overlays } = state;
    if (mids.length < 2) return;

    const innerW = Math.max(50, w - MARGIN.left - MARGIN.right);
    const innerH = Math.max(50, h - MARGIN.top - MARGIN.bottom);
    svg.select(`#${clipId} rect.clip-rect`).attr("width", innerW).attr("height", innerH);

    // x-domain covers observed mids, the current bid/ask, AND the
    // support of any analytic overlay. Unlike admin_plots.js, which
    // deliberately ignores overlay support (so a far-off posterior
    // can't balloon bin widths), this page is the user's primary view
    // of the posterior — it's worse if the posterior is mostly clipped
    // off-screen than if the histogram bins get a bit wider. The 5%
    // outlier cap on the fitter's log-returns and the tight
    // HalfNormal σ prior already keep the posterior's xs close to p₀.
    const pool = mids.slice();
    if (latest) { pool.push(latest.bid, latest.ask); }
    for (const o of overlays.values()) {
      if (o.xs.length > 0) {
        pool.push(o.xs[0], o.xs[o.xs.length - 1]);
      }
    }
    const lo = d3.min(pool);
    const hi = d3.max(pool);
    const pad = hi === lo ? Math.max(1e-6, Math.abs(hi) * 1e-6) : (hi - lo) * 0.05;
    const x = d3.scaleLinear().domain([lo - pad, hi + pad]).range([0, innerW]);

    const thresholds = x.ticks(BIN_COUNT);
    const hist = d3.histogram().domain(x.domain()).thresholds(thresholds);
    const bins = hist(mids);
    const N = mids.length;
    const dx = bins.length > 0 ? bins[0].x1 - bins[0].x0 : 0;

    // Y-axis: histogram peak OR overlay peak (density · N · dx).
    const histPeak = d3.max(bins, (b) => b.length) || 1;
    let overlayPeak = 0;
    for (const o of overlays.values()) {
      const m = d3.max(o.ys);
      if (Number.isFinite(m)) overlayPeak = Math.max(overlayPeak, m * N * dx);
    }
    const y = d3.scaleLinear().domain([0, Math.max(histPeak, overlayPeak) || 1]).range([innerH, 0]);

    gx.attr("transform", `translate(0,${innerH})`)
      .transition().duration(RENDER_MS)
      .call(d3.axisBottom(x).ticks(6).tickSizeOuter(0));
    gy.transition().duration(RENDER_MS)
      .call(d3.axisLeft(y).ticks(4).tickSizeOuter(0));

    // Histogram bars.
    barsG.selectAll("rect").data(bins).join("rect")
      .transition().duration(RENDER_MS)
      .attr("x", (d) => x(d.x0) + 1)
      .attr("y", (d) => y(d.length))
      .attr("width", (d) => Math.max(0, x(d.x1) - x(d.x0) - 2))
      .attr("height", (d) => innerH - y(d.length));

    // Empirical density (bar envelope as an area + line).
    if (bins.length > 0) {
      const bw = bins[0].x1 - bins[0].x0;
      const padded = [
        { x0: bins[0].x0 - bw / 2, x1: bins[0].x0, length: bins[0].length },
        ...bins,
        { x0: bins[bins.length - 1].x1, x1: bins[bins.length - 1].x1 + bw / 2, length: bins[bins.length - 1].length },
      ];
      const area = d3.area().curve(d3.curveMonotoneX)
        .x((d) => x((d.x0 + d.x1) / 2)).y0(y(0)).y1((d) => y(d.length));
      const line = d3.line().curve(d3.curveMonotoneX)
        .x((d) => x((d.x0 + d.x1) / 2)).y((d) => y(d.length));
      areaPath.datum(padded).transition().duration(RENDER_MS).attr("d", area);
      linePath.datum(padded).transition().duration(RENDER_MS).attr("d", line);
    }

    // Analytic overlays as filled areas (density · N · dx → counts-per-bin).
    // No transitions: new posteriors arrive every ~2s with potentially
    // very different xs, and interpolating between two different-shape
    // filled paths produces visual noise. A snap looks cleaner.
    const overlayArea = d3.area().curve(d3.curveMonotoneX)
      .x((p) => x(p[0]))
      .y0(y(0))
      .y1((p) => y(p[1] * N * dx));
    const overlayData = Array.from(overlays.entries()).map(([type, o]) => ({
      type, points: o.xs.map((xv, i) => [xv, o.ys[i]]),
    }));
    const paths = overlaysG.selectAll("path.overlay-density").data(overlayData, (d) => d.type);
    paths.exit().remove();
    paths.enter().append("path")
      .attr("class", (d) => `overlay-density overlay-${d.type}`)
      .merge(paths)
      .attr("d", (d) => overlayArea(d.points));

    // Current bid/ask vertical rules + labels.
    rulesG.selectAll("*").remove();
    if (latest) {
      const xb = x(latest.bid), xa = x(latest.ask);
      rulesG.append("line").attr("class", "bid-rule")
        .attr("x1", xb).attr("x2", xb).attr("y1", 0).attr("y2", innerH);
      rulesG.append("line").attr("class", "ask-rule")
        .attr("x1", xa).attr("x2", xa).attr("y1", 0).attr("y2", innerH);
      rulesG.append("text").attr("x", xb + 3).attr("y", 10).text(`bid ${latest.bid.toFixed(4)}`);
      rulesG.append("text").attr("x", xa + 3).attr("y", 22).text(`ask ${latest.ask.toFixed(4)}`);
    }
  }

  return { update };
}

// ─────────────────────────────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────────────────────────────

function setStatus(id, text, cls) {
  const el = document.getElementById(id);
  if (!el) return;
  el.textContent = text;
  el.className = `pill ${cls || ""}`.trim();
}

function updateQuoteCards(q) {
  if (!q) return;
  document.getElementById("bid-val").textContent = q.bid.toFixed(4);
  document.getElementById("ask-val").textContent = q.ask.toFixed(4);
  document.getElementById("mid-val").textContent = ((q.bid + q.ask) / 2).toFixed(4);
  if (Number.isFinite(q.bidSize)) document.getElementById("bid-size").textContent = `size ${q.bidSize}`;
  if (Number.isFinite(q.askSize)) document.getElementById("ask-size").textContent = `size ${q.askSize}`;
  setStatus("status-spread", `spread: ${(q.ask - q.bid).toFixed(4)}`, "ok");
}

function updatePosteriorCard(overlays) {
  // If a "posterior" overlay is present, compute its posterior mean
  // (∫ x·y dx) and show it under the mid price as a quick readout.
  const post = overlays.get("posterior");
  if (!post) return;
  let mean = 0, mass = 0;
  for (let i = 0; i < post.xs.length - 1; i++) {
    const dx = post.xs[i + 1] - post.xs[i];
    const y  = 0.5 * (post.ys[i] + post.ys[i + 1]);
    const xm = 0.5 * (post.xs[i] + post.xs[i + 1]);
    mean += xm * y * dx;
    mass += y * dx;
  }
  if (mass > 0) mean /= mass;
  if (Number.isFinite(mean)) {
    document.getElementById("mid-post").textContent = `posterior mean: ${mean.toFixed(4)}`;
  }
}

async function runSymbolDetail() {
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);
  if (!token) { showLoginModal(runSymbolDetail); return; }

  setStatus("status-nats", "nats: connecting…", "loading");
  let nc;
  try {
    nc = await connect({ servers: [NATS_URL], authenticator: tokenAuthenticator(token) });
  } catch (e) {
    console.error("NATS connect failed:", e);
    setStatus("status-nats", "nats: error", "bad");
    return;
  }
  setStatus("status-nats", "nats: connected", "ok");
  setStatus("status-ticks", "ticks: waiting…", "loading");

  const decoder = new StringCodec();
  const tsPanel   = createTimeSeriesPanel(document.getElementById("timeseries-panel"));
  const distPanel = createDistPanel(document.getElementById("dist-panel"), SYMBOL);

  const quoteSubject    = `godxfeed.quote.${SYMBOL}`;
  const overlaySubject  = `godxfeed.analytics.*.${SYMBOL}`;

  // Shared state — one rolling buffer per panel's needs.
  const state = {
    tsPoints: [],        // [{ts, bid, ask}] for TS panel (60s)
    mids: [],            // recent mids for distribution
    latest: null,        // latest {bid, ask, bidSize, askSize}
    overlays: new Map(), // type -> {xs, ys, at}
    tickCount: 0,
  };

  // Quote stream.
  const quotes$ = natsToObservable(nc, quoteSubject, decoder).pipe(
    filter((m) => m && m.eventType === "Quote"),
    map((m) => ({
      ts: Date.now(),
      bid: Number(m.bidPrice),
      ask: Number(m.askPrice),
      bidSize: Number(m.bidSize),
      askSize: Number(m.askSize),
    })),
    filter((q) => Number.isFinite(q.bid) && Number.isFinite(q.ask)),
  );

  quotes$.subscribe((q) => {
    state.latest = q;
    state.tickCount += 1;
    // Trim to 60s window.
    const cutoff = q.ts - TS_WINDOW_MS;
    state.tsPoints.push(q);
    while (state.tsPoints.length && state.tsPoints[0].ts < cutoff) state.tsPoints.shift();
    // Mid buffer.
    state.mids.push((q.bid + q.ask) / 2);
    if (state.mids.length > DIST_BUFFER) state.mids.shift();
    updateQuoteCards(q);
    setStatus("status-ticks", `ticks: ${state.tickCount}`, "ok");
  });

  // Overlay stream (sidecars publish these — see tools/analytics).
  const overlays$ = natsToObservable(nc, overlaySubject, decoder).pipe(
    filter((m) =>
      m &&
      typeof m.type === "string" &&
      Array.isArray(m.xs) &&
      Array.isArray(m.ys) &&
      m.xs.length === m.ys.length &&
      m.xs.length >= 2,
    ),
  );
  overlays$.subscribe((o) => {
    state.overlays.set(o.type, { xs: o.xs, ys: o.ys, at: o.at });
    updatePosteriorCard(state.overlays);
  });

  // Throttled renders. Ticked by time, not by events, so rendering
  // doesn't block on bursts. The time-series panel also needs to
  // advance the x-window even when there's no new tick, so it runs
  // on a pure setInterval.
  setInterval(() => tsPanel.update(state.tsPoints), RENDER_MS);
  setInterval(() => distPanel.update({
    mids: state.mids,
    latest: state.latest,
    overlays: state.overlays,
  }), RENDER_MS);
}

document.addEventListener("DOMContentLoaded", async () => {
  // Accept ?token=... once (matches admin.js / line_chart.js pattern).
  const urlParams = new URLSearchParams(window.location.search);
  const urlToken = urlParams.get("token");
  if (urlToken) {
    localStorage.setItem(AUTH_CONFIG.tokenKey, urlToken);
    urlParams.delete("token");
    const newUrl = `${window.location.pathname}${
      urlParams.toString() ? "?" + urlParams.toString() : ""
    }`;
    window.history.replaceState({}, document.title, newUrl);
  }

  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);
  if (!token) { showLoginModal(runSymbolDetail); return; }
  try {
    const r = await fetch(AUTH_CONFIG.endpoints.testToken, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!r.ok) throw new Error(`test-token ${r.status}`);
    await runSymbolDetail();
  } catch (e) {
    console.error("Token validation error:", e);
    localStorage.removeItem(AUTH_CONFIG.tokenKey);
    showLoginModal(runSymbolDetail);
  }
});

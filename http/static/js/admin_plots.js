// Per-symbol live distribution plots with optional analytic overlays.
//
// Pipeline:
//   NATS godxfeed.> subscription  (multi-token wildcard: catches
//     godxfeed.quote.<SYMBOL>, godxfeed.greeks.<SYMBOL>,
//     godxfeed.analytics.<type>.<SYMBOL>, …)
//     → Observable<decoded JSON>
//     → map: discriminate Quote vs analytic (posterior, ...)
//     → filter: drop unknown shapes (the null path collapses here so
//                the null-key group never reaches scan)
//     → groupBy(symbol) → mergeMap → scan → throttleTime(RENDER_MS)
//     → renderPanel(symbol, state)
//
// Wire contracts:
//   Quote  (subject: godxfeed.quote.<SYMBOL>):
//     { eventType: "Quote", eventSymbol, bidPrice, ... }
//
//   Analytic overlay (subject: godxfeed.analytics.<type>.<SYMBOL>):
//     { type, symbol, xs: [...], ys: [...], at }
//     `ys` is a normalized density (∫y·dx ≈ 1). It is rendered as
//     expected counts per bin (density · N · dx of the observed
//     histogram) so a well-fit posterior traces the tops of the bars.
//
// The discriminator is payload shape, not subject, so new event types
// (Greeks, TheoPrice, …) flow through without touching this file. Adding
// a new analytic type = one branch in the classifier map + one render
// branch in `update`. No new subscription, no server change: sidecars
// publish directly to NATS.

import {
  Observable,
  filter,
  groupBy,
  mergeMap,
  scan,
  throttleTime,
  map,
} from "https://esm.sh/rxjs@7.8.1";

const BUFFER_SIZE = 100;
const RENDER_MS = 200;
const BIN_COUNT = 24;
const LAST_N = 8;
const PANEL_W = 320;
const PANEL_H = 180;
const MARGIN = { top: 12, right: 12, bottom: 26, left: 36 };

// ---------------------------------------------------------------------------
// rxjs entry point
// ---------------------------------------------------------------------------

// natsToObservable wraps a nats.ws async-iterable subscription in an
// Observable. Each emission is the decoded JSON payload.
export function natsToObservable(nc, subject, decoder) {
  return new Observable((observer) => {
    let cancelled = false;
    const sub = nc.subscribe(subject);
    (async () => {
      for await (const m of sub) {
        if (cancelled) return;
        try {
          observer.next(JSON.parse(decoder.decode(m.data)));
        } catch {
          /* drop malformed */
        }
      }
      observer.complete();
    })();
    return () => {
      cancelled = true;
      sub.unsubscribe();
    };
  });
}

// startSymbolPlots wires the pipeline and returns an unsubscribe fn.
export function startSymbolPlots(messages$, container) {
  const panels = new Map(); // symbol -> { update(state) }

  const sub = messages$
    .pipe(
      map((m) => {
        if (!m) return null;
        if (m.eventType === "Quote") {
          const p = Number(m.bidPrice);
          if (!Number.isFinite(p) || !m.eventSymbol) return null;
          return { symbol: m.eventSymbol, kind: "quote", price: p };
        }
        if (
          typeof m.type === "string" &&
          typeof m.symbol === "string" &&
          Array.isArray(m.xs) &&
          Array.isArray(m.ys) &&
          m.xs.length === m.ys.length &&
          m.xs.length >= 2
        ) {
          return {
            symbol: m.symbol,
            kind: "analytic",
            overlay: { type: m.type, xs: m.xs, ys: m.ys, at: m.at },
          };
        }
        return null;
      }),
      filter((m) => m != null),
      groupBy((m) => m.symbol),
      mergeMap((g) =>
        g.pipe(
          scan(
            (state, m) => {
              if (m.kind === "quote") {
                const prices =
                  state.prices.length >= BUFFER_SIZE
                    ? state.prices.slice(1)
                    : state.prices.slice();
                prices.push(m.price);
                return { prices, overlays: state.overlays };
              }
              const overlays = new Map(state.overlays);
              overlays.set(m.overlay.type, m.overlay);
              return { prices: state.prices, overlays };
            },
            { prices: [], overlays: new Map() },
          ),
          throttleTime(RENDER_MS, undefined, {
            leading: false,
            trailing: true,
          }),
          map((state) => ({ symbol: g.key, state })),
        ),
      ),
    )
    .subscribe(({ symbol, state }) => {
      // Wait until at least one quote has arrived before spawning a
      // panel. Prevents blank panels for symbols that are only seeing
      // analytic overlays (e.g. an analytics sidecar publishing ahead
      // of the quote stream).
      if (state.prices.length < 2) return;
      let panel = panels.get(symbol);
      if (!panel) {
        panel = createPanel(container, symbol);
        panels.set(symbol, panel);
      }
      panel.update(state);
    });

  return () => sub.unsubscribe();
}

// ---------------------------------------------------------------------------
// D3 panel
// ---------------------------------------------------------------------------

function createPanel(container, symbol) {
  const wrap = document.createElement("div");
  wrap.className = "sym-plot";
  wrap.innerHTML = `
    <div class="sym-plot-head">
      <span class="sym-name">${symbol}</span>
      <span class="sym-last">—</span>
    </div>
    <svg width="${PANEL_W}" height="${PANEL_H}"></svg>
  `;
  container.appendChild(wrap);

  const svg = d3.select(wrap).select("svg");
  const inner = svg
    .append("g")
    .attr("transform", `translate(${MARGIN.left},${MARGIN.top})`);

  const innerW = PANEL_W - MARGIN.left - MARGIN.right;
  const innerH = PANEL_H - MARGIN.top - MARGIN.bottom;

  const x = d3.scaleLinear().range([0, innerW]);
  const y = d3.scaleLinear().range([innerH, 0]);

  const gx = inner
    .append("g")
    .attr("class", "xaxis")
    .attr("transform", `translate(0,${innerH})`);
  const gy = inner.append("g").attr("class", "yaxis");

  // Data marks live inside a clipped sub-group so overlays that extend
  // beyond the observed x-domain (a model predicting outside where data
  // lives) don't paint spurious line segments over the axes or margins.
  const clipId = `sym-plot-clip-${symbol.replace(/[^A-Za-z0-9_-]/g, "_")}`;
  svg
    .append("defs")
    .append("clipPath")
    .attr("id", clipId)
    .append("rect")
    .attr("width", innerW)
    .attr("height", innerH);
  const plot = inner.append("g").attr("clip-path", `url(#${clipId})`);

  // z-order (bottom → top): empirical fill, empirical density line,
  // bars, analytic overlays, recent-tick rules. Overlays sit above
  // bars so the modeled curve stays legible; recent ticks stay on top.
  const areaPath = plot.append("path").attr("class", "fillarea");
  const linePath = plot.append("path").attr("class", "dens-line");
  const barsG = plot.append("g").attr("class", "bars");
  const overlaysG = plot.append("g").attr("class", "overlays");
  const linesG = plot.append("g").attr("class", "recent");

  const lastEl = wrap.querySelector(".sym-last");

  function update(state) {
    const { prices, overlays } = state;
    if (prices.length < 2) return;

    // x-domain tracks observed prices only. Stretching it to cover a
    // far-off overlay would balloon the histogram bin width, which in
    // turn balloons `overlayPeak = density · N · dx` and pushes the
    // y-axis to absurd values. Overlay points outside the observed
    // range are clipped by the plot group's clip-path.
    const lo = d3.min(prices);
    const hi = d3.max(prices);
    const pad =
      hi === lo ? Math.max(1e-6, Math.abs(hi) * 1e-6) : (hi - lo) * 0.02;
    x.domain([lo - pad, hi + pad]);

    const thresholds = x.ticks(BIN_COUNT);
    const hist = d3.histogram().domain(x.domain()).thresholds(thresholds);
    const bins = hist(prices);
    const N = prices.length;
    const dx = bins.length > 0 ? bins[0].x1 - bins[0].x0 : 0;

    // y-domain covers histogram peak and overlay peaks (density · N · dx)
    // so neither view squashes the other.
    const histPeak = d3.max(bins, (b) => b.length) || 1;
    let overlayPeak = 0;
    for (const o of overlays.values()) {
      const m = d3.max(o.ys);
      if (Number.isFinite(m)) overlayPeak = Math.max(overlayPeak, m * N * dx);
    }
    y.domain([0, Math.max(histPeak, overlayPeak) || 1]);

    gx.transition()
      .duration(RENDER_MS)
      .call(d3.axisBottom(x).ticks(5).tickSizeOuter(0));
    gy.transition()
      .duration(RENDER_MS)
      .call(d3.axisLeft(y).ticks(4).tickSizeOuter(0));

    // Bars.
    barsG
      .selectAll("rect")
      .data(bins)
      .join("rect")
      .transition()
      .duration(RENDER_MS)
      .attr("x", (d) => x(d.x0) + 1)
      .attr("y", (d) => y(d.length))
      .attr("width", (d) => Math.max(0, x(d.x1) - x(d.x0) - 2))
      .attr("height", (d) => innerH - y(d.length));

    // Empirical density area + curve (padded endpoints so the curve
    // reaches the edges).
    if (bins.length > 0) {
      const bw = bins[0].x1 - bins[0].x0;
      const padded = [
        { x0: bins[0].x0 - bw / 2, x1: bins[0].x0, length: bins[0].length },
        ...bins,
        {
          x0: bins[bins.length - 1].x1,
          x1: bins[bins.length - 1].x1 + bw / 2,
          length: bins[bins.length - 1].length,
        },
      ];
      const area = d3
        .area()
        .curve(d3.curveMonotoneX)
        .x((d) => x((d.x0 + d.x1) / 2))
        .y0(y(0))
        .y1((d) => y(d.length));
      const line = d3
        .line()
        .curve(d3.curveMonotoneX)
        .x((d) => x((d.x0 + d.x1) / 2))
        .y((d) => y(d.length));
      areaPath.datum(padded).transition().duration(RENDER_MS).attr("d", area);
      linePath.datum(padded).transition().duration(RENDER_MS).attr("d", line);
    }

    // Analytic overlays. Each published density ∫y·dx ≈ 1 is scaled to
    // N·dx to read as expected counts-per-bin on the same y-axis.
    const overlayLine = d3
      .line()
      .curve(d3.curveMonotoneX)
      .x((p) => x(p[0]))
      .y((p) => y(p[1] * N * dx));
    const overlayData = Array.from(overlays.entries()).map(([type, o]) => ({
      type,
      points: o.xs.map((xv, i) => [xv, o.ys[i]]),
    }));
    const paths = overlaysG
      .selectAll("path.overlay-density")
      .data(overlayData, (d) => d.type);
    paths.exit().remove();
    paths
      .enter()
      .append("path")
      .attr("class", (d) => `overlay-density overlay-${d.type}`)
      .merge(paths)
      .transition()
      .duration(RENDER_MS)
      .attr("d", (d) => overlayLine(d.points));

    // Last-N recent ticks as vertical rules, opacity ramp toward latest.
    const recent = prices.slice(-LAST_N);
    linesG
      .selectAll("line")
      .data(recent, (_d, i) => i + (prices.length - recent.length))
      .join("line")
      .attr("x1", (d) => x(d))
      .attr("x2", (d) => x(d))
      .attr("y1", 0)
      .attr("y2", innerH)
      .attr(
        "opacity",
        (_d, i) => 0.15 + 0.75 * (i / Math.max(1, recent.length - 1)),
      );

    const latest = prices[prices.length - 1];
    lastEl.textContent = latest.toFixed(4);
  }

  return { update };
}

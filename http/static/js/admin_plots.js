// Per-symbol live distribution plots.
//
// Pipeline:
//   NATS godxfeed.> subscription
//     → Observable<{symbol, bid, ts}>
//     → groupBy(symbol)                    — one inner stream per symbol
//     → scan into a rolling buffer of size BUFFER_SIZE
//     → throttleTime(RENDER_MS, trailing)  — coalesce re-renders
//     → renderPanel(symbol, buffer)        — D3 histogram + area + recent-tick overlay
//
// Each unseen symbol gets a fresh panel appended to the provided container.
// Once a panel exists it stays; if the symbol goes silent the last frame
// just freezes. Re-subscribing picks up where we left off — the rolling
// buffer keeps its state because groupBy's inner Observable is kept alive.

import {
  Observable,
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
// Observable. Each emission is the decoded JSON payload. Invalid JSON or
// non-Quote messages are dropped.
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
  const panels = new Map(); // symbol -> { update(prices) }

  const sub = messages$
    .pipe(
      // Drop anything that isn't a Quote with a numeric bidPrice.
      map((q) => {
        if (!q || q.eventType !== "Quote") return null;
        const p = Number(q.bidPrice);
        if (!Number.isFinite(p) || !q.eventSymbol) return null;
        return { symbol: q.eventSymbol, price: p };
      }),
      groupBy((m) => (m ? m.symbol : null)),
      mergeMap((g) =>
        g.pipe(
          // Skip the null-group (filtered-out messages collapse here).
          ...(g.key == null ? [] : []),
          scan(
            (buf, m) => {
              const next = buf.length >= BUFFER_SIZE ? buf.slice(1) : buf.slice();
              next.push(m.price);
              return next;
            },
            /* seed: */ [],
          ),
          throttleTime(RENDER_MS, undefined, {
            leading: false,
            trailing: true,
          }),
          map((prices) => ({ symbol: g.key, prices })),
        ),
      ),
    )
    .subscribe(({ symbol, prices }) => {
      if (symbol == null) return;
      let panel = panels.get(symbol);
      if (!panel) {
        panel = createPanel(container, symbol);
        panels.set(symbol, panel);
      }
      panel.update(prices);
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

  const areaPath = inner.append("path").attr("class", "fillarea");
  const linePath = inner.append("path").attr("class", "dens-line");
  const barsG = inner.append("g").attr("class", "bars");
  const linesG = inner.append("g").attr("class", "recent");

  const lastEl = wrap.querySelector(".sym-last");

  function update(prices) {
    if (prices.length < 2) return;

    const [lo, hi] = d3.extent(prices);
    // Guard against flat data (all identical prices) — would produce NaN bins.
    const pad = hi === lo ? Math.max(1e-6, Math.abs(hi) * 1e-6) : (hi - lo) * 0.02;
    x.domain([lo - pad, hi + pad]);

    const thresholds = x.ticks(BIN_COUNT);
    const hist = d3.histogram().domain(x.domain()).thresholds(thresholds);
    const bins = hist(prices);
    y.domain([0, d3.max(bins, (b) => b.length) || 1]);

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

    // Area + density curve (padded endpoints so the curve reaches the edges).
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
      areaPath
        .datum(padded)
        .transition()
        .duration(RENDER_MS)
        .attr("d", area);
      linePath
        .datum(padded)
        .transition()
        .duration(RENDER_MS)
        .attr("d", line);
    }

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
      .attr("opacity", (_d, i) => 0.15 + 0.75 * (i / Math.max(1, recent.length - 1)));

    const latest = prices[prices.length - 1];
    lastEl.textContent = latest.toFixed(4);
  }

  return { update };
}

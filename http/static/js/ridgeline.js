import * as Plot from "https://cdn.jsdelivr.net/npm/@observablehq/plot@0.6/+esm";
import { ensureValidToken } from "./auth.js";

async function runRidglinePlot() {
  const basicAuth = `Basic ${btoa(
    `${BASIC_AUTH_EMAIL}:${BASIC_AUTH_PASSWORD}`
  )}`;
  const token = await ensureValidToken(ENDPOINT, LSATK, basicAuth);
  // Read the data
  let data = await d3.json(
    `${ENDPOINT}/plot-dummy-data?${new URLSearchParams({
      symbol: SYMBOL,
      group: "expiry",
      expiry: "241001",
    }).toString()}`,
    {
      headers: new Headers({
        Authorization: `Bearer ${token}`,
      }),
    }
  );

  data = data
    .map((d) => {
      d.ts = d3.isoParse(d.ts);
      return d;
    })
    .filter((d) => {
      return d.symbol !== "SPY";
    });

  // get unique ticker symbols
  const rowsBySym = {};
  for (const d of data) {
    if (Object.hasOwn(rowsBySym, d.symbol)) {
      rowsBySym[d.symbol].push(d);
    } else {
      rowsBySym[d.symbol] = [d];
    }
  }
  const rows = [];
  // histogram equity prices
  const equityBins = d3
    .bin()
    .domain(
      d3.extent(data.filter((d) => d.symbol === SYMBOL).map((d) => d.bid_price))
    )
    .value((d) => d.bid_price)(data);

  const equityRows = equityBins.map((d) => {
    return {
      symbol: SYMBOL,
      price: (d.x0 + d.x1) / 2,
      count: Object.keys(d).length - 2,
    };
  });
  rows.push(...equityRows);

  // Histogram option prices. Loop over each option symbol amd
  // create bins for each collection of prices.
  for (const [sym, optData] of Object.entries(rowsBySym)) {
    const optBins = d3
      .bin()
      .domain(
        d3.extent(optData.map((d) => (d.bid_price > 0 ? d.bid_price : 1)))
      )
      .value((d) => d.bid_price)(optData);

    const optRows = optBins.map((d) => {
      return {
        symbol: sym,
        price: (d.x0 + d.x1) / 2,
        count: Object.keys(d).length - 2,
      };
    });
    rows.push(...optRows);
  }

  // We have the data in row format, now we can do whatever we want with it.
  // Ultimately we'll want to make a d3 plot, but now, just do it with Plot.
  const options = {
    marks: [
      Plot.frame(),
      Plot.lineY(rows, {
        x: "price",
        y: "count",
        stroke: "symbol",
        strokeWidth: 1,
        clip: true,
        fy: "symbol",
      }),
    ],
    grid: true,
    inset: 10,
    facet: { marginRight: 100 },
    x: {
      tickSpacing: 80,
      label: "Contract Price",
    },
    y: {
      label: "Frequency",
      tickFormat: d3.format("~s"),
    },
    legend: false,
    title: `Ridgeline Plot for ${SYMBOL} Options`,
    width: 1000,
    height: 2000,
  };
  const plot = Plot.plot(options);
  const div = document.querySelector("#plot");
  div.append(plot);
  $("#loading").toggle();
}

// This is the main entry point for the ridgeline plot.
// It will run when the page loads.
$(document).ready(async () => await runRidglinePlot());

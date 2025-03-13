import {
  StringCodec,
  connect,
  millis,
  tokenAuthenticator,
  usernamePasswordAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";
import { ensureValidToken, authenticatedFetch } from "./auth.js";
import { MaxLengthQueue } from "./max_length_queue.js";
import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";

// this will define a blob of data, then incrementally push the data into a queue.
async function runDynamicDistribution() {
  const qSize = 100;
  const chartParams = setupChart(qSize);
  const data = new MaxLengthQueue(qSize);

  // Parse symbol from URL
  const urlParams = new URLSearchParams(window.location.search);
  const symbol = urlParams.get("symbol") || "SPY"; // Default to SPY if no symbol provided

  // Connect to NATS and subscribe to updates
  try {
    // Get and validate/refresh JWT token
    const token = await ensureValidToken();

    console.log("Connecting to NATS server", NATS_URL);
    const nc = await connect({
      servers: [NATS_URL],
      authenticator: tokenAuthenticator(token),
    });

    // Request the server to start streaming data for this symbol
    const response = await authenticatedFetch(
      `${AUTH_CONFIG.endpoints.stream}?symbol=${symbol}`,
      null,
      {
        method: "POST",
      }
    );

    if (!response.ok) {
      throw new Error("Failed to request data stream");
    }
    const streamData = await response.json();
    const streamSubject = streamData.subject;

    // Subscribe to the specific subject for this symbol
    // FIXME: eventually we should subscribe to the specific subject for this symbol
    const sub = nc.subscribe("godxfeed");
    const decoder = new StringCodec();

    // Process incoming messages
    for await (const msg of sub) {
      const parsed = JSON.parse(decoder.decode(msg.data));
      data.enqueue({ value: parsed }); // Using bid_price as the value
      updateChart(chartParams, data);
    }
  } catch (error) {
    console.error("NATS connection error:", error);
  }
}

function setupChart(qSize) {
  const svg = d3.select("svg");
  const width = +svg.attr("width");
  const height = +svg.attr("height");
  const margin = { top: 20, right: 20, bottom: 70, left: 40 };
  const innerWidth = width - margin.left - margin.right;
  const innerHeight = height - margin.top - margin.bottom;

  // Create initial scales
  const [min, max] = [75, 125];
  const x = d3
    .scaleLinear()
    .domain([min, max])
    .range([margin.left, width - margin.right]);
  const y = d3
    .scaleLinear()
    .domain([0, 1])
    .range([height - margin.bottom, margin.top]);

  const g = svg
    .append("g")
    .attr("transform", `translate(${margin.left},${margin.top})`);

  // Append axes
  const gx = g
    .append("g")
    .attr("class", "xaxis")
    .attr("transform", `translate(0,${height - margin.bottom})`)
    .call(d3.axisBottom(x));

  svg
    .append("text")
    .attr("class", "x-label")
    .attr("text-anchor", "end")
    .attr("x", width - margin.right)
    .attr("y", height - 10)
    .text("bid price (dollars)");

  const gy = g
    .append("g")
    .attr("class", "yaxis")
    .attr("transform", `translate(${margin.left},0)`)
    .call(d3.axisLeft(y));

  svg
    .append("text")
    .attr("class", "y-label")
    .attr("text-anchor", "end")
    .attr("x", margin.left / 2)
    .attr("y", margin.top)
    .attr("transform", `rotate(-90,${margin.left / 2},${margin.top})`)
    .text("relative frequency (counts)");

  return { width, height, margin, innerWidth, innerHeight, g, gx, gy };
}

function updateChart(chartParams, data) {
  // Skip cases with 0 or 1 element in data since it'll result in NaNs or a bin
  // with x0 == x1 (i.e., zero width) which throws an error
  if (data.queue.length < 2) {
    return;
  }
  // Create new scales
  const [min, max] = d3.extent(data.queue, (d) => d.value);
  const binCount = 40;
  const thresholds = d3.range(min, max, (max - min) / binCount);

  const x = d3
    .scaleLinear()
    .domain([min, max])
    .range([
      chartParams.margin.left,
      chartParams.margin.left + chartParams.innerWidth,
    ]);

  // recompute the histogram
  const histogram = d3
    .histogram()
    .domain(x.domain())
    .value((d) => d.value)
    .thresholds(thresholds);

  const bins = histogram(data.queue);

  // recompute the line
  const line = d3
    .line()
    .curve(d3.curveMonotoneX)
    .x((d) => x((d.x0 + d.x1) / 2))
    .y((d) => y(d.length));

  const y = d3
    .scaleLinear()
    .domain([
      0,
      d3.max(bins, (d) => {
        return d.length;
      }),
    ])
    .range([
      chartParams.margin.top + chartParams.innerHeight,
      chartParams.margin.top,
    ]);

  // Create an area generator
  const area = d3
    .area()
    .curve(d3.curveMonotoneX)
    .x((d) => x((d.x0 + d.x1) / 2))
    .y0(y(0))
    .y1((d) => y(d.length));

  // update axes
  chartParams.gx.transition().duration(10).call(d3.axisBottom(x));
  chartParams.gy.transition().duration(10).call(d3.axisLeft(y));

  // // Draw bars
  chartParams.g
    .selectAll(".bar")
    .data(bins)
    .join(
      (enter) => enter.append("rect"),
      (update) => update,
      (exit) => exit.remove()
    )
    .attr("class", "bar")
    .transition()
    .duration(10)
    .attr("x", (d) => x(d.x0))
    .attr("y", (d) => y(d.length))
    .attr("width", (d) => x(d.x1) - x(d.x0) - 1)
    .attr("height", (d) => y(0) - y(d.length));

  // Create a "padded" version of the bins so that the area and line endpoints
  // call on the ends of the range. This requires adding a half bin on the
  // endpoints of the original bin data.
  const bw = bins[0].x1 - bins[0].x0;
  const paddedBins = [
    {
      x0: bins[0].x0 - bw / 2,
      x1: bins[0].x1 - bw / 2,
      length: bins[0].length,
    },
    ...bins,
    {
      x0: bins[bins.length - 1].x0 + bw / 2,
      x1: bins[bins.length - 1].x1 + bw / 2,
      length: bins[bins.length - 1].length,
    },
  ];

  // Draw curve
  chartParams.g
    .selectAll(".line")
    .data([paddedBins])
    .join(
      (enter) => enter.append("path").attr("class", "line"),
      (update) => update,
      (exit) => exit.remove()
    )
    .transition()
    .duration(10)
    .attr("d", line)
    .attr("fill", "none")
    .attr("stroke", "green")
    .attr("stroke-width", 2.5);

  // Draw the area
  chartParams.g
    .selectAll(".fillarea")
    .data([paddedBins])
    .join(
      (enter) => enter.append("path").attr("class", "fillarea"),
      (update) => update,
      (exit) => exit.remove()
    )
    .transition()
    .duration(10)
    .attr("d", area)
    .attr("fill", "green")
    .attr("fill-opacity", 0.2);

  // Draw last N prices with increasing opacity
  const lastN = 10;
  chartParams.g
    .selectAll(".vline")
    .data(data.queue.slice(-lastN), (d) => {
      return d.value;
    })
    .join(
      (enter) =>
        // enter selection starts vertically over the destination so the lines
        // flow straight down.
        enter
          .append("line")
          .attr("class", "vline")
          .attr("x1", (d) => x(d.value))
          .attr("x2", (d) => x(d.value))
          .attr("y1", (d) => 0)
          .attr("y2", (d) => 0),
      (update) => update,
      (exit) => exit.remove()
    )
    .transition()
    .duration(80)
    .attr("x1", (d) => x(d.value))
    .attr("y1", (d) => y(0))
    .attr("x2", (d) => x(d.value))
    .attr("y2", (d) => chartParams.margin.top + chartParams.innerHeight - 25)
    .attr("fill", "none")
    .attr("stroke", "red")
    .attr("stroke-width", 5)
    .attr("opacity", (d, i) => i / lastN);
}

// This is the main entry point for the dynamic distribution plot.
// It will run when the page loads.
document.addEventListener("DOMContentLoaded", async () => {
  // Check if token exists in localStorage
  let token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  // Check if token is in URL parameters
  const urlParams = new URLSearchParams(window.location.search);
  const urlToken = urlParams.get("token");

  // If token is in URL, save it to localStorage and remove from URL
  if (urlToken) {
    localStorage.setItem(AUTH_CONFIG.tokenKey, urlToken);
    token = urlToken;

    // Remove token from URL without refreshing the page
    const newUrl = new URL(window.location.href);
    newUrl.searchParams.delete("token");
    window.history.replaceState({}, document.title, newUrl.toString());
  }

  if (!token) {
    // No token found, show login modal
    showLoginModal(runDynamicDistribution);
  } else {
    // Token exists, verify it
    try {
      const response = await authenticatedFetch(
        AUTH_CONFIG.endpoints.testToken
      );

      if (!response.ok) {
        // Token is invalid, show login modal
        throw new Error("Invalid token");
      }

      // Token is valid, run the dynamic distribution
      await runDynamicDistribution();
    } catch (error) {
      console.error("Token validation error:", error);
      // Clear invalid token
      localStorage.removeItem(AUTH_CONFIG.tokenKey);
      // Show login modal
      showLoginModal(runDynamicDistribution);
    }
  }
});

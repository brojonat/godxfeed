import * as Plot from "https://cdn.jsdelivr.net/npm/@observablehq/plot@0.6/+esm";
import * as d3 from "https://cdn.jsdelivr.net/npm/d3@7/+esm";
import {
  StringCodec,
  connect,
  tokenAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";
import { ensureValidToken } from "./auth.js";
import { MaxLengthQueue } from "./max_length_queue.js";
import { createMarginalHistogram } from "./marginal_histogram.js";
import { authenticatedFetch } from "./auth.js";
import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";

async function runLineChart() {
  const PLOT_WIDTH = 800;
  const PLOT_HEIGHT = 600;
  const MARGINAL_WIDTH = 200;
  const MAX_DATA_POINTS = 1000; // Maximum number of data points to keep

  // Parse symbol from URL
  const urlParams = new URLSearchParams(window.location.search);
  const symbol = urlParams.get("symbol") || "SPY"; // Default to SPY if no symbol provided

  // Create queue for data management
  const dataQueue = new MaxLengthQueue(MAX_DATA_POINTS);

  // Get token from localStorage
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  // Create NATS connection
  const nc = await connect({
    servers: [NATS_URL],
    token: token,
  });

  const data = await authenticatedFetch(
    `${AUTH_CONFIG.endpoints.timeseries}/symbol-regexp?symbol=${symbol}&symbolType=stock&symbol_only=true`
  );

  if (!data.ok) {
    throw new Error("Failed to fetch historical data");
  }

  const plotData = (await data.json())
    .filter((d) => d.symbol === symbol)
    .map((d) => ({
      ts: new Date(d.ts),
      bid_price: d.bid_price,
      ask_price: d.ask_price,
    }));

  // Initialize queue with historical data
  for (const point of plotData.sort((a, b) => a.ts - b.ts)) {
    dataQueue.enqueue(point);
  }

  // Create controls container
  const controlsContainer = document.createElement("div");
  controlsContainer.className = "chart-controls";
  controlsContainer.innerHTML = `
    <div class="time-presets">
      <div class="preset-group">
        <span class="preset-label">Minutes:</span>
        <button data-minutes="1">1m</button>
        <button data-minutes="2">2m</button>
        <button data-minutes="5">5m</button>
        <button data-minutes="10">10m</button>
        <button data-minutes="30">30m</button>
      </div>
      <div class="preset-group">
        <span class="preset-label">Hours:</span>
        <button data-minutes="60">1h</button>
        <button data-minutes="120">2h</button>
        <button data-minutes="240">4h</button>
        <button data-minutes="720">12h</button>
        <button data-minutes="1440">1d</button>
        <button data-minutes="2880">2d</button>
        <button data-minutes="60480">1w</button>
      </div>
    </div>
    <div class="datetime-container">
      <label for="startTime">Start Time: </label>
      <input type="datetime-local" id="startTime" step="1">
      <label for="endTime">End Time: </label>
      <input type="datetime-local" id="endTime" step="1">
    </div>
    <div class="auto-update-control">
      <label class="switch">
        <input type="checkbox" id="autoUpdateToggle">
        <span class="slider round"></span>
        <span class="toggle-label">Auto-update range</span>
      </label>
    </div>
  `;

  // Create quote info container
  const quoteInfoContainer = document.createElement("div");
  quoteInfoContainer.className = "quote-info-container";
  quoteInfoContainer.innerHTML = `
    <div class="quote-info">
      <div class="price-display">
        <div class="price-row horizontal">
          <div class="price-group">
            <span class="price-label">Bid</span>
            <span class="price bid-price">-</span>
            <span class="size bid-size">-</span>
          </div>
          <div class="price-group">
            <span class="price-label">Mid</span>
            <span class="price mid-price">-</span>
          </div>
          <div class="price-group">
            <span class="price-label">Ask</span>
            <span class="price ask-price">-</span>
            <span class="size ask-size">-</span>
          </div>
        </div>
      </div>
    </div>
  `;

  // Add both containers before the plot
  document.querySelector("#plot").before(controlsContainer);
  document.querySelector("#plot").before(quoteInfoContainer);

  let isDragging = false;
  let dragStart = null;
  let currentViewStart = null;

  // Add document-level mouseup handler to ensure dragging always stops
  document.addEventListener("mouseup", () => {
    isDragging = false;
    dragStart = null;
  });

  // Function to update the plot
  function updatePlot(timeRangeMinutes, startTime = null) {
    // Calculate time window
    const now = new Date(Math.max(...dataQueue.queue.map((d) => d.ts)));
    const windowStart =
      startTime || new Date(now - timeRangeMinutes * 60 * 1000);
    const windowEnd = new Date(
      windowStart.getTime() + timeRangeMinutes * 60 * 1000
    );

    // Convert to local timezone for datetime pickers
    const startLocal = new Date(
      windowStart.getTime() - windowStart.getTimezoneOffset() * 60000
    );
    const endLocal = new Date(
      windowEnd.getTime() - windowEnd.getTimezoneOffset() * 60000
    );

    // Update datetime pickers to match the actual window being displayed
    startTimePicker.value = startLocal.toISOString().slice(0, 19);
    endTimePicker.value = endLocal.toISOString().slice(0, 19);

    // Helper function for formatting time ticks
    function formatTick(date) {
      // Calculate actual time range in minutes from the visible data
      const timeRangeMinutes = (windowEnd - windowStart) / (60 * 1000);

      // For ranges less than an hour, show HH:MM:SS
      if (timeRangeMinutes <= 60) {
        return date.toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
          hour12: false,
        });
      }

      // For ranges less than a day, show HH:MM
      if (timeRangeMinutes <= 1440) {
        return date.toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
          hour12: false,
        });
      }

      // For longer ranges, show MM/DD HH:MM
      return date
        .toLocaleString([], {
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
          hour12: false,
          timeZone: "UTC",
        })
        .replace(",", ""); // Remove comma between date and time
    }

    // Filter data for time window using queue data
    const visibleData = dataQueue.queue.filter(
      (d) => d.ts >= windowStart && d.ts <= windowEnd
    );

    // Calculate y-axis domain with some padding
    const allPrices = visibleData.flatMap((d) => [d.bid_price, d.ask_price]);
    const minPrice = Math.min(...allPrices);
    const maxPrice = Math.max(...allPrices);
    const padding = (maxPrice - minPrice) * 0.1;
    const yDomain = [minPrice - padding, maxPrice + padding];

    // Create new plot
    const plot = Plot.plot({
      width: PLOT_WIDTH,
      height: PLOT_HEIGHT,
      y: {
        type: "linear",
        domain: yDomain,
        grid: true,
        label: "Price ($)",
        labelOffset: 45,
        tickSize: 0,
      },
      x: {
        domain: [windowStart, windowEnd],
        grid: true,
        label: "Time",
        tickFormat: formatTick,
        ticks: 10,
        tickSize: 0,
        labelOffset: 40,
      },
      marks: [
        Plot.gridX({
          strokeOpacity: 0.1,
        }),
        Plot.gridY({
          strokeOpacity: 0.1,
        }),
        // Bid price line
        Plot.line(visibleData, {
          x: "ts",
          y: "bid_price",
          stroke: "#ef4444",
          strokeWidth: 2,
        }),
        // Ask price line
        Plot.line(visibleData, {
          x: "ts",
          y: "ask_price",
          stroke: "#22c55e",
          strokeWidth: 2,
        }),
        // Bid price dots
        Plot.dot(visibleData, {
          x: "ts",
          y: "bid_price",
          fill: "#ef4444",
          r: 4,
          title: (d) =>
            `Time: ${d.ts.toLocaleString()}\nBid: $${d.bid_price.toFixed(
              2
            )}\nAsk: $${d.ask_price.toFixed(2)}`,
        }),
        // Ask price dots
        Plot.dot(visibleData, {
          x: "ts",
          y: "ask_price",
          fill: "#22c55e",
          r: 4,
          title: (d) =>
            `Time: ${d.ts.toLocaleString()}\nBid: $${d.bid_price.toFixed(
              2
            )}\nAsk: $${d.ask_price.toFixed(2)}`,
        }),
      ],
      style: {
        background: "transparent",
        color: "#9ca3af",
        fontSize: 12,
        fontFamily: "system-ui, -apple-system, sans-serif",
      },
      marginBottom: 60,
      marginLeft: 60,
    });

    // Create and append containers
    const plotDiv = document.querySelector("#plot");
    plotDiv.innerHTML = ""; // Clear existing content
    plotDiv.style.display = "flex";
    plotDiv.style.alignItems = "flex-start";
    plotDiv.style.gap = "10px";

    const mainPlotContainer = document.createElement("div");
    mainPlotContainer.id = "main-plot";

    const marginalPlotContainer = document.createElement("div");
    marginalPlotContainer.id = "marginal-plot";
    marginalPlotContainer.style.width = `${MARGINAL_WIDTH}px`;
    marginalPlotContainer.style.height = `${PLOT_HEIGHT}px`;
    marginalPlotContainer.style.padding = "0";
    marginalPlotContainer.style.boxSizing = "border-box";
    marginalPlotContainer.style.border = "1px solid #374151";
    marginalPlotContainer.style.borderRadius = "4px";
    marginalPlotContainer.style.backgroundColor = "#1f2937";

    // Add containers to the plot div
    plotDiv.appendChild(mainPlotContainer);
    plotDiv.appendChild(marginalPlotContainer);

    mainPlotContainer.innerHTML = "";
    mainPlotContainer.append(plot);

    // Create and add marginal histogram
    if (!marginalPlotContainer.firstChild) {
      // Only create new histogram if it doesn't exist
      const marginalPlot = createMarginalHistogram(visibleData, {
        width: MARGINAL_WIDTH - 20,
        height: PLOT_HEIGHT - 20,
        yDomain: yDomain,
      });
      marginalPlotContainer.appendChild(marginalPlot);
    } else {
      // Update existing histogram with new data point
      const latestPoint = visibleData[visibleData.length - 1];
      marginalPlotContainer.firstChild.addDataPoint(latestPoint);
    }

    // Add mousedown event listener to mainPlotContainer
    mainPlotContainer.addEventListener("mousedown", (e) => {
      isDragging = true;
      dragStart = { x: e.clientX, time: currentViewStart };

      // Turn off auto-update when user starts dragging
      const autoUpdateToggle = document.querySelector("#autoUpdateToggle");
      autoUpdateToggle.checked = false;
      isAutoUpdateEnabled = false;
    });

    currentViewStart = windowStart;
  }

  // Replace slider event listeners with datetime picker listeners
  const startTimePicker = document.querySelector("#startTime");
  const endTimePicker = document.querySelector("#endTime");

  // Initialize datetime pickers with default range (60 minutes)
  const now = new Date(Math.max(...dataQueue.queue.map((d) => d.ts)));
  const defaultStart = new Date(now - 60 * 60 * 1000); // 60 minutes ago

  startTimePicker.value = defaultStart.toISOString().slice(0, 19);
  endTimePicker.value = now.toISOString().slice(0, 19);

  // Update plot when datetime inputs change
  function handleDateTimeChange(e) {
    const start = new Date(startTimePicker.value);
    const end = new Date(endTimePicker.value);

    // Validate the dates
    if (e.target === startTimePicker && start > new Date(endTimePicker.value)) {
      startTimePicker.setCustomValidity("Start time must be before end time");
      startTimePicker.reportValidity();
      return;
    }
    if (e.target === endTimePicker && end < new Date(startTimePicker.value)) {
      endTimePicker.setCustomValidity("End time must be after start time");
      endTimePicker.reportValidity();
      return;
    }

    // Clear any previous validation messages
    startTimePicker.setCustomValidity("");
    endTimePicker.setCustomValidity("");

    if (start && end && start < end) {
      // Use the local times directly without UTC conversion
      const timeRangeMinutes = (end - start) / (60 * 1000);
      updatePlot(timeRangeMinutes, start);
    }
  }

  startTimePicker.addEventListener("input", handleDateTimeChange);
  endTimePicker.addEventListener("input", handleDateTimeChange);
  startTimePicker.addEventListener("change", handleDateTimeChange);
  endTimePicker.addEventListener("change", handleDateTimeChange);

  // Update findNearestDataPoint to use queue data
  function findNearestDataPoint(targetTime) {
    if (dataQueue.isEmpty()) return null;

    // Ensure targetTime is a valid Date object
    if (!(targetTime instanceof Date) || Number.isNaN(targetTime)) {
      console.warn("Invalid target time provided to findNearestDataPoint");
      return dataQueue.front();
    }

    return dataQueue.queue.reduce((nearest, current) => {
      const currentDiff = Math.abs(current.ts.getTime() - targetTime.getTime());
      const nearestDiff = Math.abs(nearest.ts.getTime() - targetTime.getTime());
      return currentDiff < nearestDiff ? current : nearest;
    }, dataQueue.front());
  }

  // Update the preset button listeners
  for (const button of document.querySelectorAll(".time-presets button")) {
    button.addEventListener("click", () => {
      // Add active class to clicked button and remove from others
      for (const btn of document.querySelectorAll(".time-presets button")) {
        btn.classList.remove("active");
      }
      button.classList.add("active");

      const timeRangeMinutes = Number.parseInt(button.dataset.minutes);

      // Get the current state of datetime pickers when the callback executes
      const start = new Date(startTimePicker.value);
      const end = new Date(endTimePicker.value);

      // Ensure we have valid dates
      if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) {
        console.warn("Invalid datetime picker values");
        return;
      }

      // Calculate the current center point
      const currentCenter = new Date((start.getTime() + end.getTime()) / 2);
      const nearestPoint = findNearestDataPoint(currentCenter);

      if (!nearestPoint) {
        console.warn("No nearest point found");
        return;
      }

      // Calculate new start and end times centered around nearest data point
      const halfRange = (timeRangeMinutes * 60 * 1000) / 2;
      const newStart = new Date(nearestPoint.ts.getTime() - halfRange);
      const newEnd = new Date(nearestPoint.ts.getTime() + halfRange);

      // Update datetime pickers
      startTimePicker.value = newStart.toISOString().slice(0, 19);
      endTimePicker.value = newEnd.toISOString().slice(0, 19);

      updatePlot(timeRangeMinutes, newStart);
    });
  }

  document.addEventListener("mousemove", (e) => {
    if (!isDragging || !dragStart) return;

    const dx = e.clientX - dragStart.x;
    const start = new Date(startTimePicker.value);
    const end = new Date(endTimePicker.value);

    const startUTC = new Date(
      start.getTime() + start.getTimezoneOffset() * 60000
    );
    const endUTC = new Date(end.getTime() + end.getTimezoneOffset() * 60000);

    const timeRange = endUTC - startUTC;
    // Make sure we're using PLOT_WIDTH for calculations
    const timeDelta = (dx / PLOT_WIDTH) * timeRange;

    const newStart = new Date(dragStart.time.getTime() - timeDelta);
    const newEnd = new Date(newStart.getTime() + timeRange);

    // Convert to local for datetime pickers
    const newStartLocal = new Date(
      newStart.getTime() - newStart.getTimezoneOffset() * 60000
    );
    const newEndLocal = new Date(
      newEnd.getTime() - newEnd.getTimezoneOffset() * 60000
    );

    startTimePicker.value = newStartLocal.toISOString().slice(0, 19);
    endTimePicker.value = newEndLocal.toISOString().slice(0, 19);

    const timeRangeMinutes = timeRange / (60 * 1000);
    updatePlot(timeRangeMinutes, newStart);
  });

  // Initial plot
  updatePlot(60);
  $("#loading").toggle();

  // Add auto-update toggle to controls container
  const autoUpdateToggle = document.querySelector("#autoUpdateToggle");

  // Initialize auto-update state
  let isAutoUpdateEnabled = autoUpdateToggle.checked;
  autoUpdateToggle.addEventListener("change", (e) => {
    isAutoUpdateEnabled = e.target.checked;
  });

  // DEBUGGING: for debugging purposes, push random data into the queue
  const debug = true;
  if (debug) {
    setInterval(() => {
      const midNormal = d3.randomNormal.source(d3.randomLcg(Date.now()))(
        150,
        0.5
      );
      const mid = midNormal();
      const halfNormal = d3.randomNormal.source(d3.randomLcg(Date.now()))(
        0,
        0.1
      );
      const variation = Math.abs(halfNormal());
      const newPoint = {
        ts: new Date(),
        bid_price: mid - variation,
        bid_size: 100 + Math.random() * 100,
        ask_price: mid + variation,
        ask_size: 100 + Math.random() * 100,
      };
      dataQueue.enqueue(newPoint);
      // Calculate current time range from datetime pickers
      const start = new Date(startTimePicker.value);
      const end = new Date(endTimePicker.value);
      const timeRangeMinutes = (end - start) / (60 * 1000);
      // Update quote info display
      document.querySelector(".bid-price").textContent =
        newPoint.bid_price.toFixed(2);
      document.querySelector(".ask-price").textContent =
        newPoint.ask_price.toFixed(2);
      document.querySelector(".mid-price").textContent = (
        (newPoint.bid_price + newPoint.ask_price) /
        2
      ).toFixed(2);
      document.querySelector(".bid-size").textContent =
        Math.round(newPoint.bid_size) || "-";
      document.querySelector(".ask-size").textContent =
        Math.round(newPoint.ask_size) || "-";
      if (isAutoUpdateEnabled) {
        const now = new Date();
        const newStart = new Date(now - timeRangeMinutes * 60 * 1000);
        startTimePicker.value = newStart.toISOString().slice(0, 19);
        endTimePicker.value = now.toISOString().slice(0, 19);
        updatePlot(timeRangeMinutes, newStart);
      }
    }, 200);
  }

  // Request the server to start streaming data for this symbol
  const response = await authenticatedFetch(
    `${AUTH_CONFIG.endpoints.stream}?symbol=${symbol}`,
    null,
    {
      method: "POST",
    }
  );

  if (!response.ok) {
    throw new Error("Failed to request historical data stream");
  }
  const d = await response.json();
  const streamSubject = d.subject;

  // Subscribe to the stream
  try {
    const sub = nc.subscribe(`godxfeed.${streamSubject}`);
    const decoder = new StringCodec();
    for await (const msg of sub) {
      const data = JSON.parse(decoder.decode(msg.data));
      dataQueue.enqueue({
        ts: new Date(data.ts),
        bid_price: data.bid_price,
        ask_price: data.ask_price,
      });

      // Update quote info display
      document.querySelector(".bid-price").textContent =
        data.bid_price.toFixed(2);
      document.querySelector(".ask-price").textContent =
        data.ask_price.toFixed(2);
      document.querySelector(".mid-price").textContent = (
        (data.bid_price + data.ask_price) /
        2
      ).toFixed(2);
      document.querySelector(".bid-size").textContent =
        Math.round(data.bid_size) || "-";
      document.querySelector(".ask-size").textContent =
        Math.round(data.ask_size) || "-";

      // Calculate current time range from datetime pickers
      const start = new Date(startTimePicker.value);
      const end = new Date(endTimePicker.value);
      const timeRangeMinutes = (end - start) / (60 * 1000);

      // If auto-update is enabled, adjust the time window to follow the latest data
      if (isAutoUpdateEnabled) {
        const now = new Date();
        const newStart = new Date(now - timeRangeMinutes * 60 * 1000);
        startTimePicker.value = newStart.toISOString().slice(0, 19);
        endTimePicker.value = now.toISOString().slice(0, 19);
        updatePlot(timeRangeMinutes, newStart);
      }
    }
  } catch (error) {
    console.error("Error in NATS subscription:", error);
    // Maybe show an error to the user or try to reconnect
  }
}

// This is the main entry point for the line chart plot.
// It will run when the page loads.
$(document).ready(async () => {
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
    showLoginModal(runLineChart);
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

      // Token is valid, run the line chart
      runLineChart();
    } catch (error) {
      console.error("Token validation error:", error);
      // Clear invalid token
      localStorage.removeItem(AUTH_CONFIG.tokenKey);
      // Show login modal
      showLoginModal(runLineChart);
    }
  }
});

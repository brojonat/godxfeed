import * as Plot from "https://cdn.jsdelivr.net/npm/@observablehq/plot@0.6/+esm";
import * as d3 from "https://cdn.jsdelivr.net/npm/d3@7/+esm";
import {
  StringCodec,
  connect,
  millis,
  tokenAuthenticator,
  usernamePasswordAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";
import { authenticatedFetch } from "./auth.js";
import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";
import { generateData } from "./generate_data.js";

// Variables to track current filter settings
let currentStrikeRange = 20; // Default to ±20% strike range
let currentStartDate = new Date(); // Default to today
let currentEndDate = new Date(); // Default to today
currentEndDate.setMonth(currentEndDate.getMonth() + 3); // Default to 3 months out
let isAutoUpdateEnabled = true;

// Add these variables at the top level to store data and track the grid state
let optionsData = new Map(); // Map to store the latest quotes by symbol
let callsContainer = null; // Reference to the calls grid container
let putsContainer = null; // Reference to the puts grid container
let xScale = null; // X scale for expiration dates
let yScale = null; // Y scale for strike prices
let expirationDates = new Set(); // Set of unique expiration dates
let strikePrices = new Set(); // Set of unique strike prices
let underlyingSymbol = "SPY"; // Default to SPY as the underlying symbol

async function runOptionsGrid() {
  // Constants for plot dimensions
  const PLOT_WIDTH = 600;
  const PLOT_HEIGHT = 400;

  // Parse symbol from URL
  const urlParams = new URLSearchParams(window.location.search);
  const symbol = urlParams.get("symbol") || "SPY"; // Default to SPY if no symbol provided
  underlyingSymbol = symbol; // Set the global variable

  const response = await authenticatedFetch(
    `${AUTH_CONFIG.endpoints.optionChain}?symbol=${symbol}`
  );
  const data = await response.json();
  // Extract all streamer symbols from the API response
  const streamerSymbols = [];

  if (data?.data?.items) {
    for (const optType of data.data.items) {
      if (optType["streamer-symbols"]) {
        streamerSymbols.push(...optType["streamer-symbols"]);
      }
    }
  }

  // Create controls container
  createControlsContainer();

  // Set up event listeners for controls
  setupEventListeners();

  // Initialize the options grid with streamer symbols
  await initializeOptionsGrid(streamerSymbols);
}

// This is the main entry point for the options grid.
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
    showLoginModal(runOptionsGrid);
  } else {
    // Token exists, verify it
    try {
      const response = await fetch(AUTH_CONFIG.endpoints.testToken, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        // Token is invalid, show login modal
        throw new Error("Invalid token");
      }

      // Token is valid, run the options grid
      runOptionsGrid();
    } catch (error) {
      console.error("Token validation error:", error);
      // Clear invalid token
      localStorage.removeItem(AUTH_CONFIG.tokenKey);
      // Show login modal
      showLoginModal(runOptionsGrid);
    }
  }
});

async function initializeOptionsGrid(streamerSymbols) {
  // Fallback to hardcoded symbols if still no symbols
  if (!streamerSymbols || streamerSymbols.length === 0) {
    console.error("No streamer symbols found; maybe API is down?");
    return;
  }

  const interval = 1000; // 1 second interval

  // Start the data stream with the fetched symbols
  const dataStream = generateData(streamerSymbols, interval);

  // Clear loading indicator
  $("#loading").hide();

  // Process the stream
  (async () => {
    for await (const quote of dataStream) {
      // Only update if auto-update is enabled
      if (isAutoUpdateEnabled) {
        updateGridWithQuote(quote);
      }
    }
  })();
}

function updateGridWithQuote(quote) {
  if (!quote || !quote.symbol) return;

  console.log("Received quote:", quote);

  // Store the quote in our data map
  optionsData.set(quote.symbol, quote);

  // Parse the option symbol to extract components
  const symbolInfo = parseOptionSymbol(quote.symbol);
  if (!symbolInfo) return;

  // Extract the underlying symbol if not already set
  if (symbolInfo.underlying) {
    underlyingSymbol = symbolInfo.underlying;
  }

  // Add expiration date and strike to our sets
  expirationDates.add(symbolInfo.expirationDate);
  strikePrices.add(symbolInfo.strike);

  // Update the grids
  updateGrids();
}

function parseOptionSymbol(symbol) {
  // Example: "SPY250321C705"
  // This regex extracts: underlying, year, month, day, type (C/P), and strike
  const regex = /^([A-Z]+)(\d{2})(\d{2})(\d{2})([CP])(\d+(?:\.\d+)?)$/;
  const match = symbol.match(regex);

  if (!match) return null;

  const [_, underlying, year, month, day, optionType, strike] = match;
  const fullYear = Number.parseInt(`20${year}`); // Convert YY to YYYY

  return {
    underlying,
    expirationDate: `${fullYear}-${month}-${day}`,
    optionType, // 'C' for call, 'P' for put
    strike: Number.parseFloat(strike),
    fullSymbol: symbol,
  };
}

function updateGrids() {
  // Convert the Map to an array for processing
  const allData = Array.from(optionsData.values());

  const callsContainer = document.getElementById("calls-grid-container");
  const putsContainer = document.getElementById("puts-grid-container");

  // Separate calls and puts
  const callsData = allData.filter((quote) => {
    const info = parseOptionSymbol(quote.symbol);
    return info && info.optionType === "C";
  });

  const putsData = allData.filter((quote) => {
    const info = parseOptionSymbol(quote.symbol);
    return info && info.optionType === "P";
  });

  console.log(
    `Data: ${allData.length} quotes (${callsData.length} calls, ${putsData.length} puts)`
  );

  // Get all expiration dates within the selected date range
  const availableDates = Array.from(expirationDates)
    .filter((date) => {
      const expDate = new Date(date);
      return expDate >= currentStartDate && expDate <= currentEndDate;
    })
    .sort();

  // Get all strikes within the selected strike range
  const underlyingPrice = 500; // Match the value used elsewhere
  const minStrike =
    currentStrikeRange === 0
      ? 0
      : underlyingPrice * (1 - currentStrikeRange / 100);
  const maxStrike =
    currentStrikeRange === 0
      ? Number.POSITIVE_INFINITY
      : underlyingPrice * (1 + currentStrikeRange / 100);

  const availableStrikes = Array.from(strikePrices)
    .filter((strike) => strike >= minStrike && strike <= maxStrike)
    .sort((a, b) => b - a); // Descending order for strikes

  // Use fixed width and height if containers aren't properly sized
  const gridWidth = callsContainer.clientWidth || 800;
  const gridHeight = callsContainer.clientHeight || 400;

  // Create scales using the filtered dates and strikes
  xScale = d3
    .scaleBand()
    .domain(availableDates)
    .range([0, gridWidth - 100])
    .padding(0.1);

  yScale = d3
    .scaleBand()
    .domain(availableStrikes)
    .range([0, gridHeight - 100])
    .padding(0.1);

  // Update calls grid
  updateSingleGrid(callsContainer, callsData, "C", gridWidth, gridHeight);

  // Update puts grid
  updateSingleGrid(putsContainer, putsData, "P", gridWidth, gridHeight);
}

function updateSingleGrid(container, data, optionType, width, height) {
  // Clear previous grid
  d3.select(container).selectAll("svg").remove();

  // Create SVG
  const svg = d3
    .select(container)
    .append("svg")
    .attr("width", width || 800)
    .attr("height", height || 400);

  // Add group for the grid
  const grid = svg.append("g").attr("transform", "translate(50, 50)");

  // Add x-axis
  grid
    .append("g")
    .attr("transform", `translate(0, ${yScale.range()[1]})`)
    .call(d3.axisBottom(xScale))
    .selectAll("text")
    .style("text-anchor", "end")
    .attr("dx", "-.8em")
    .attr("dy", ".15em")
    .attr("transform", "rotate(-45)");

  // Add y-axis
  grid.append("g").call(d3.axisLeft(yScale));

  // Add title
  svg
    .append("text")
    .attr("x", (width || 800) / 2)
    .attr("y", 20)
    .attr("text-anchor", "middle")
    .style("font-size", "16px")
    .style("font-weight", "bold")
    .text(`${optionType === "C" ? "Calls" : "Puts"}`);

  // Add cells
  const cells = [];

  data.forEach((quote) => {
    const info = parseOptionSymbol(quote.symbol);
    if (info) {
      cells.push({
        x: info.expirationDate,
        y: info.strike,
        value: quote.lastPrice || quote.price || 0,
        symbol: quote.symbol,
      });
    }
  });

  // Add rectangles for each cell
  grid
    .selectAll("rect")
    .data(cells)
    .enter()
    .append("rect")
    .attr("x", (d) => xScale(d.x))
    .attr("y", (d) => yScale(d.y))
    .attr("width", xScale.bandwidth())
    .attr("height", yScale.bandwidth())
    .attr(
      "fill",
      optionType === "C" ? "rgba(0, 128, 255, 0.7)" : "rgba(255, 128, 0, 0.7)"
    )
    .attr("stroke", "#ccc")
    .on("mouseover", function (event, d) {
      d3.select(this).attr(
        "fill",
        optionType === "C" ? "rgba(0, 128, 255, 1)" : "rgba(255, 128, 0, 1)"
      );

      // Show tooltip
      const tooltip = d3
        .select("body")
        .append("div")
        .attr("class", "tooltip")
        .style("position", "absolute")
        .style("background", "white")
        .style("padding", "5px")
        .style("border", "1px solid #ccc")
        .style("border-radius", "3px")
        .style("pointer-events", "none")
        .style("opacity", 0);

      tooltip.transition().duration(200).style("opacity", 0.9);

      tooltip
        .html(
          `
        Symbol: ${d.symbol}<br>
        Strike: ${d.y}<br>
        Expiry: ${d.x}<br>
        Price: ${d.value.toFixed(2)}
      `
        )
        .style("left", event.pageX + 10 + "px")
        .style("top", event.pageY - 28 + "px");
    })
    .on("mouseout", function () {
      d3.select(this).attr(
        "fill",
        optionType === "C" ? "rgba(0, 128, 255, 0.7)" : "rgba(255, 128, 0, 0.7)"
      );
      d3.select(".tooltip").remove();
    });

  // Add text for prices
  grid
    .selectAll("text.cell-value")
    .data(cells)
    .enter()
    .append("text")
    .attr("class", "cell-value")
    .attr("x", (d) => xScale(d.x) + xScale.bandwidth() / 2)
    .attr("y", (d) => yScale(d.y) + yScale.bandwidth() / 2)
    .attr("text-anchor", "middle")
    .attr("dominant-baseline", "middle")
    .text((d) => d.value.toFixed(2))
    .style("font-size", "12px")
    .style("fill", "white");
}

function createControlsContainer() {
  // Set default values for date inputs
  document.getElementById("expiryStartDate").valueAsDate = currentStartDate;
  document.getElementById("expiryEndDate").valueAsDate = currentEndDate;

  // Set default state for auto-update toggle
  document.getElementById("autoUpdateToggle").checked = isAutoUpdateEnabled;

  // Highlight the default strike range button
  const defaultStrikeButton = document.querySelector(
    `button[data-strikes="${currentStrikeRange}"]`
  );
  if (defaultStrikeButton) {
    defaultStrikeButton.classList.add("active");
  }
}

function setupEventListeners() {
  // Strike range buttons
  for (const button of document.querySelectorAll(".strike-presets button")) {
    button.addEventListener("click", function () {
      for (const b of document.querySelectorAll(".strike-presets button")) {
        b.classList.remove("active");
      }
      this.classList.add("active");
      currentStrikeRange = Number.parseInt(this.getAttribute("data-strikes"));
      updateGrids();
    });
  }

  // Default to ±20% button active
  document
    .querySelector('.strike-presets button[data-strikes="20"]')
    .classList.add("active");

  // Date range pickers
  document.getElementById("expiryStartDate").valueAsDate = currentStartDate;
  document.getElementById("expiryEndDate").valueAsDate = currentEndDate;

  document
    .getElementById("expiryStartDate")
    .addEventListener("change", function () {
      currentStartDate = this.valueAsDate;
      updateGrids();
    });

  document
    .getElementById("expiryEndDate")
    .addEventListener("change", function () {
      currentEndDate = this.valueAsDate;
      updateGrids();
    });

  // Auto-update toggle
  document
    .getElementById("autoUpdateToggle")
    .addEventListener("change", function () {
      isAutoUpdateEnabled = this.checked;
    });
}

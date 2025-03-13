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

async function runOptionsGrid() {
  // Constants for plot dimensions
  const PLOT_WIDTH = 600;
  const PLOT_HEIGHT = 400;

  // Parse symbol from URL
  const urlParams = new URLSearchParams(window.location.search);
  const symbol = urlParams.get("symbol") || "SPY"; // Default to SPY if no symbol provided

  // Variables to track current filter settings
  let currentStrikeRange = 10; // Default to ±10 strike range
  let currentStartDate = new Date(); // Default to today
  let currentEndDate = new Date(); // Initialize to today
  currentEndDate.setDate(currentEndDate.getDate() + 10); // Add 10 days
  let isAutoUpdateEnabled = true;

  const data = await authenticatedFetch(
    `${AUTH_CONFIG.endpoints.optionsData}?symbol=${symbol}`
  );

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

  // Function to create the controls container
  function createControlsContainer() {
    // Check if controls already exist
    if (document.querySelector(".chart-controls")) {
      return; // Don't create duplicate controls
    }

    const controlsContainer = document.createElement("div");
    controlsContainer.className = "chart-controls";
    controlsContainer.style.maxWidth = "800px";
    controlsContainer.style.margin = "20px auto";

    // Create strike range controls
    const strikeRangeRow = document.createElement("div");
    strikeRangeRow.className = "control-row";

    const strikePresetLabel = document.createElement("span");
    strikePresetLabel.className = "preset-label";
    strikePresetLabel.textContent = "Strike Range:";

    const strikePresets = document.createElement("div");
    strikePresets.className = "strike-presets";

    const strikeRanges = [5, 10, 20, 50, 100];
    for (const range of strikeRanges) {
      const button = document.createElement("button");
      button.textContent = `±$${range}`;
      button.dataset.range = range; // Use dataset property to set data attribute
      // Set active class on the default range (10)
      if (range === currentStrikeRange) {
        button.classList.add("active");
      }
      strikePresets.appendChild(button);
    }

    strikeRangeRow.appendChild(strikePresetLabel);
    strikeRangeRow.appendChild(strikePresets);

    // Create date range controls
    const dateRangeRow = document.createElement("div");
    dateRangeRow.className = "control-row";

    const dateRangeLabel = document.createElement("span");
    dateRangeLabel.className = "preset-label";
    dateRangeLabel.textContent = "Date Range:";

    const dateContainer = document.createElement("div");
    dateContainer.className = "datetime-container";

    const startDateInput = document.createElement("input");
    startDateInput.type = "date";
    startDateInput.id = "expiryStartDate";
    // Format date as YYYY-MM-DD for the input
    startDateInput.value = currentStartDate.toISOString().split("T")[0];

    const dateRangeSeparator = document.createElement("span");
    dateRangeSeparator.textContent = "to";

    const endDateInput = document.createElement("input");
    endDateInput.type = "date";
    endDateInput.id = "expiryEndDate";
    // Format date as YYYY-MM-DD for the input
    endDateInput.value = currentEndDate.toISOString().split("T")[0];

    dateContainer.appendChild(startDateInput);
    dateContainer.appendChild(dateRangeSeparator);
    dateContainer.appendChild(endDateInput);

    dateRangeRow.appendChild(dateRangeLabel);
    dateRangeRow.appendChild(dateContainer);

    // Create auto-update toggle
    const autoUpdateRow = document.createElement("div");
    autoUpdateRow.className = "control-row";

    const autoUpdateControl = document.createElement("div");
    autoUpdateControl.className = "auto-update-control";

    const autoUpdateLabel = document.createElement("span");
    autoUpdateLabel.className = "preset-label";
    autoUpdateLabel.textContent = "Auto Update:";

    // Create button-style toggle
    const autoUpdateButton = document.createElement("button");
    autoUpdateButton.id = "autoUpdateToggle";
    autoUpdateButton.className = "auto-update-button active";
    autoUpdateButton.textContent = "Auto";

    autoUpdateControl.appendChild(autoUpdateLabel);
    autoUpdateControl.appendChild(autoUpdateButton);

    autoUpdateRow.appendChild(autoUpdateControl);

    // Add all controls to container
    controlsContainer.appendChild(strikeRangeRow);
    controlsContainer.appendChild(dateRangeRow);
    controlsContainer.appendChild(autoUpdateRow);

    // Add controls to page
    document.body.insertBefore(
      controlsContainer,
      document.getElementById("grid") || document.body.firstChild
    );
  }

  // Set up event listeners for controls
  function setupEventListeners() {
    // Strike range buttons
    const strikeButtons = document.querySelectorAll(".strike-presets button");
    for (const button of strikeButtons) {
      button.addEventListener("click", () => {
        const newRange = Number.parseInt(button.dataset.range, 10);
        // Update the currentStrikeRange variable with the new value
        currentStrikeRange = newRange;

        // Remove active class from all buttons
        for (const btn of strikeButtons) {
          btn.classList.remove("active");
        }
        // Add active class to the clicked button
        button.classList.add("active");
      });
    }

    // Date pickers
    const startDatePicker = document.getElementById("expiryStartDate");
    if (startDatePicker) {
      startDatePicker.addEventListener("change", () => {
        currentStartDate = new Date(startDatePicker.value);
      });
    }

    const endDatePicker = document.getElementById("expiryEndDate");
    if (endDatePicker) {
      endDatePicker.addEventListener("change", () => {
        currentEndDate = new Date(endDatePicker.value);
      });
    }

    // Auto-update toggle
    const autoUpdateToggle = document.getElementById("autoUpdateToggle");
    if (autoUpdateToggle) {
      autoUpdateToggle.addEventListener("click", () => {
        isAutoUpdateEnabled = !isAutoUpdateEnabled;
        if (isAutoUpdateEnabled) {
          autoUpdateToggle.classList.add("active");
        } else {
          autoUpdateToggle.classList.remove("active");
        }
      });
    }
  }

  $("#loading").toggle();
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

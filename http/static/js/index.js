import { ensureValidToken, authenticatedFetch } from "./auth.js";
import { AUTH_CONFIG, PLOT_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";

// Define updateLinks function and make it globally available
window.updateLinks = () => {
  const symbolInput = document.getElementById("symbolInput");
  if (!symbolInput) {
    console.error("Symbol input element not found");
    return;
  }

  const symbol = symbolInput.value.toUpperCase();

  const links = [
    { id: "optionsGridLink", type: PLOT_CONFIG.types.optionsGrid },
    { id: "lineChartLink", type: PLOT_CONFIG.types.lineChart },
    { id: "dynamicDistLink", type: PLOT_CONFIG.types.dynamicDistribution },
  ];

  for (const link of links) {
    try {
      const linkElement = document.getElementById(link.id);
      if (!linkElement) {
        console.error(`Link element with ID ${link.id} not found`);
      } else {
        const plotUrl = `${PLOT_CONFIG.plotsEndpoint}?symbol=${symbol}&plot_kind=${link.type}`;

        // Set the data-plot-url attribute for the navigation handler
        linkElement.setAttribute("data-plot-url", plotUrl);

        // Add click handler to handle navigation with auth
        linkElement.addEventListener("click", handlePlotNavigation);

        // Set href for non-JS fallback
        linkElement.href = plotUrl;
      }
    } catch (error) {
      console.error(`Error updating link ${link.id}:`, error);
    }
  }
};

// Function to handle plot navigation with authentication
async function handlePlotNavigation(event) {
  event.preventDefault();

  const plotUrl = this.getAttribute("data-plot-url");
  if (!plotUrl) {
    console.error("No plot URL found");
    return;
  }

  // Get the token
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);
  if (!token) {
    // No token, show login modal
    showLoginModal(() => {
      // After login, try navigation again
      handlePlotNavigation.call(this, event);
    });
    return;
  }

  try {
    // First verify the token is valid
    const response = await fetch(AUTH_CONFIG.endpoints.testToken, {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }

    // If token is valid, navigate to the page with token as query param
    const url = new URL(plotUrl, window.location.origin);
    url.searchParams.append("token", token);
    window.location.href = url.toString();
  } catch (error) {
    console.error("Navigation failed:", error);
    if (error.status === 401) {
      // Handle unauthorized error - show login modal
      showLoginModal(() => handlePlotNavigation.call(this, event));
    }
  }
}

async function fetchAvailableSymbols() {
  try {
    const response = await authenticatedFetch(
      `${AUTH_CONFIG.endpoints.optionChain}?symbol=SPY`,
      {},
      showLoginModal
    );

    const data = await response.json();
    displaySymbols(data.symbols);
  } catch (error) {
    console.error("Error fetching symbols:", error);
  }
}

function displaySymbols(symbols) {
  if (!symbols || symbols.length === 0) {
    return;
  }

  // Create a datalist for symbol autocomplete
  const datalist = document.createElement("datalist");
  datalist.id = "symbolOptions";

  // Add options to datalist
  for (const symbol of symbols) {
    const option = document.createElement("option");
    option.value = symbol;
    datalist.appendChild(option);
  }

  // Add datalist to the document
  document.body.appendChild(datalist);

  // Connect input to datalist
  const symbolInput = document.getElementById("symbolInput");
  symbolInput.setAttribute("list", "symbolOptions");

  // Set first symbol as default if none is already set
  if (!symbolInput.value && symbols.length > 0) {
    symbolInput.value = symbols[0];
    updateLinks();
  }
}

// Function to handle logout
function handleLogout() {
  // Remove token from localStorage
  localStorage.removeItem(AUTH_CONFIG.tokenKey);

  // Reload the page to trigger re-authentication
  window.location.reload();
}

// Function to add logout button
function addLogoutButton() {
  const container = document.querySelector(".container");

  // Create logout button container for styling
  const logoutContainer = document.createElement("div");
  logoutContainer.className = "logout-container";

  // Create the logout button
  const logoutButton = document.createElement("button");
  logoutButton.id = "logoutButton";
  logoutButton.className = "logout-button";
  logoutButton.textContent = "Logout";
  logoutButton.addEventListener("click", handleLogout);

  // Add button to container
  logoutContainer.appendChild(logoutButton);

  // Add to the top of the main container
  container.insertBefore(logoutContainer, container.firstChild);
}

// Function to initialize the app after authentication
function initializeApp() {
  // Add logout button
  addLogoutButton();

  // Fetch available symbols from the backend
  fetchAvailableSymbols();

  // Initialize links with default symbol
  updateLinks();
}

document.addEventListener("DOMContentLoaded", async () => {
  // Check if token exists in localStorage
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  if (!token) {
    // No token found, show login modal
    showLoginModal(initializeApp);
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

      // Token is valid, initialize app
      initializeApp();
    } catch (error) {
      console.error("Token validation error:", error);
      // Clear invalid token
      localStorage.removeItem(AUTH_CONFIG.tokenKey);
      // Show login modal
      showLoginModal(initializeApp);
    }
  }
});

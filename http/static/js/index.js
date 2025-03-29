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
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  // Check if all link elements exist
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
        // Set the complete URL path with token as query param
        const plotUrl = `${
          PLOT_CONFIG.plotsEndpoint
        }?symbol=${symbol}&plot_kind=${link.type}${
          token ? `&token=${token}` : ""
        }`;

        // Set href directly now that we're using query params for auth
        linkElement.href = plotUrl;

        // Remove any previously added click handlers
        if (linkElement.hasAttribute("data-handler-attached")) {
          linkElement.removeEventListener("click", handlePlotNavigation);
          linkElement.removeAttribute("data-handler-attached");
        }

        // Reset cursor style
        linkElement.style.cursor = "";
      }
    } catch (error) {
      console.error(`Error updating link ${link.id}:`, error);
    }
  }
};

// Function to handle plot navigation with authentication
function handlePlotNavigation(event) {
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

  // Option 1: Open in new window/tab with token in URL (less secure but simpler)
  // window.open(`${plotUrl}&token=${token}`, '_blank');

  // Option 2: Use fetch with proper Authorization header and display the result
  authenticatedFetch(plotUrl, {}, showLoginModal)
    .then((response) => {
      if (response.ok) {
        // For HTML responses, you can redirect to the URL
        // The server should validate the token from the Authorization header
        window.location.href = plotUrl;
      } else {
        console.error("Failed to access plot:", response.statusText);
      }
    })
    .catch((error) => {
      console.error("Error accessing plot:", error);
    });
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

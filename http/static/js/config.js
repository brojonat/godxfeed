// Authentication and API configuration
export const AUTH_CONFIG = {
  // Key used for storing the authentication token in localStorage
  tokenKey: "godxfeed_auth_token",

  // API endpoints
  endpoints: {
    testToken: "/test-bearer-token",
    optionChain: "/option-chain",
    stream: "/stream",
    timeseries: "/timeseries",
  },
};

// Plot configuration
export const PLOT_CONFIG = {
  types: {
    optionsGrid: "options_grid",
    lineChart: "line_chart",
    ridgeline: "ridgeline",
    dynamicDistribution: "dynamic_distribution",
  },

  // Base URL for plot routes
  plotsEndpoint: "/plots",
};

// Initialize config with values from the page
export function initConfig(endpoint) {
  // Set API endpoint if provided
  if (endpoint) {
    window.API_ENDPOINT = endpoint;
  }
}

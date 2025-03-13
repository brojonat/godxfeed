import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";

// Function to ensure a valid token is available
export async function ensureValidToken(endpoint, callback) {
  // Check if token exists in localStorage
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  if (!token) {
    // No token found, try to get a new one
    try {
      const newToken = await getNewToken(endpoint);
      localStorage.setItem(AUTH_CONFIG.tokenKey, newToken);
      if (callback) callback(newToken);
      return newToken;
    } catch (error) {
      console.error("Failed to get new token:", error);
      throw error;
    }
  }

  // Token exists, verify it
  try {
    const response = await fetch(AUTH_CONFIG.endpoints.testToken, {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    if (!response.ok) {
      // Token is invalid, get a new one
      const newToken = await getNewToken(endpoint);
      localStorage.setItem(AUTH_CONFIG.tokenKey, newToken);
      if (callback) callback(newToken);
      return newToken;
    }

    // Token is valid
    if (callback) callback(token);
    return token;
  } catch (error) {
    console.error("Token validation error:", error);
    // Try to get a new token
    try {
      const newToken = await getNewToken(endpoint);
      localStorage.setItem(AUTH_CONFIG.tokenKey, newToken);
      if (callback) callback(newToken);
      return newToken;
    } catch (tokenError) {
      console.error("Failed to get new token:", tokenError);
      throw tokenError;
    }
  }
}

// Function to get a new token
// FIXME: this should just prompt the user for their auth token
async function getNewToken(endpoint) {
  const response = await fetch("/token", {
    method: "POST",
    headers: {
      Authorization: "Bearer abc123",
    },
  });

  if (!response.ok) {
    throw new Error("Failed to get new token");
  }

  const data = await response.json();
  return data.token;
}
// Create a utility function for authenticated fetch requests
export async function authenticatedFetch(url, onAuthFailure, options = {}) {
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  if (!token) {
    if (onAuthFailure) {
      onAuthFailure();
    } else {
      showLoginModal();
    }
    throw new Error("No authentication token available");
  }

  // Merge the authorization header with any existing options
  const fetchOptions = {
    ...options,
    headers: {
      ...options.headers,
      Authorization: `Bearer ${token}`,
    },
  };

  try {
    const response = await fetch(url, fetchOptions);

    if (!response.ok) {
      // If unauthorized or forbidden, show login modal
      if (response.status === 401 || response.status === 403) {
        localStorage.removeItem(AUTH_CONFIG.tokenKey);
        if (onAuthFailure) {
          onAuthFailure();
        } else {
          showLoginModal();
        }
        throw new Error(`Authentication failed: ${response.status}`);
      }
      throw new Error(`Request failed: ${response.status}`);
    }

    return response;
  } catch (error) {
    // If it's a network error, it might be due to invalid token
    if (error.name === "TypeError" && error.message.includes("network")) {
      localStorage.removeItem(AUTH_CONFIG.tokenKey);
      if (onAuthFailure) {
        onAuthFailure();
      } else {
        showLoginModal();
      }
    }
    throw error;
  }
}

import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";

// Function to ensure a valid token is available
export async function ensureValidToken(endpoint, callback) {
  // Check if token exists in localStorage
  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);

  if (!token) {
    // No token found, show login modal
    return new Promise((resolve) => {
      showLoginModal((token) => {
        if (callback) callback(token);
        resolve(token);
      });
    });
  }

  // Token exists, verify it
  try {
    const response = await fetch(AUTH_CONFIG.endpoints.testToken, {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    if (!response.ok) {
      // Token is invalid, show login modal
      return new Promise((resolve) => {
        showLoginModal((token) => {
          if (callback) callback(token);
          resolve(token);
        });
      });
    }

    // Token is valid
    if (callback) callback(token);
    return token;
  } catch (error) {
    console.error("Token validation error:", error);
    return new Promise((resolve) => {
      showLoginModal((token) => {
        if (callback) callback(token);
        resolve(token);
      });
    });
  }
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

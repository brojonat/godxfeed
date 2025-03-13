import { AUTH_CONFIG } from "./config.js";

/**
 * Shows a login modal that prompts the user for their API token
 * @param {Function} onSuccessCallback - Function to call when authentication succeeds
 */
export function showLoginModal(onSuccessCallback) {
  // Create modal container
  const modalContainer = document.createElement("div");
  modalContainer.className = "login-modal-container";

  // Create modal content
  const modalContent = document.createElement("div");
  modalContent.className = "login-modal";

  // Create modal header
  const modalHeader = document.createElement("h2");
  modalHeader.textContent = "Authentication Required";

  // Create form
  const form = document.createElement("form");
  form.id = "login-form";

  // Create token input
  const tokenLabel = document.createElement("label");
  tokenLabel.htmlFor = "api-token";
  tokenLabel.textContent = "Enter your API token:";

  const tokenInput = document.createElement("input");
  tokenInput.type = "password";
  tokenInput.id = "api-token";
  tokenInput.required = true;

  // Create submit button
  const submitButton = document.createElement("button");
  submitButton.type = "submit";
  submitButton.textContent = "Login";

  // Create error message container
  const errorMessage = document.createElement("div");
  errorMessage.id = "login-error";
  errorMessage.style.display = "none";

  // Assemble the modal
  form.appendChild(tokenLabel);
  form.appendChild(tokenInput);
  form.appendChild(submitButton);
  form.appendChild(errorMessage);

  modalContent.appendChild(modalHeader);
  modalContent.appendChild(form);
  modalContainer.appendChild(modalContent);

  // Add form submission handler
  form.addEventListener("submit", async (e) => {
    e.preventDefault();

    // Disable the submit button and show loading state
    submitButton.disabled = true;
    submitButton.textContent = "Verifying...";

    const token = tokenInput.value.trim();

    if (!token) {
      errorMessage.textContent = "Please enter a token";
      errorMessage.style.display = "block";
      submitButton.disabled = false;
      submitButton.textContent = "Login";
      return;
    }

    // Verify token
    try {
      // Store token in localStorage temporarily
      localStorage.setItem(AUTH_CONFIG.tokenKey, token);

      // Test the token
      const response = await fetch(AUTH_CONFIG.endpoints.testToken, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        throw new Error(
          `Authentication failed (${response.status}): ${response.statusText}`
        );
      }

      // Token is valid, remove modal and continue
      document.body.removeChild(modalContainer);

      // Call the success callback if provided
      if (onSuccessCallback && typeof onSuccessCallback === "function") {
        onSuccessCallback();
      }
    } catch (error) {
      console.error("Authentication error:", error);

      // Show error message
      errorMessage.textContent = `Authentication failed: ${
        error.message || "Invalid token"
      }`;
      errorMessage.style.display = "block";

      // Clear localStorage
      localStorage.removeItem(AUTH_CONFIG.tokenKey);

      // Re-enable the submit button
      submitButton.disabled = false;
      submitButton.textContent = "Login";
    }
  });

  // Add to body
  document.body.appendChild(modalContainer);

  // Focus the input field
  setTimeout(() => tokenInput.focus(), 100);
}

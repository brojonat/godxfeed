import {
  StringCodec,
  connect,
  millis,
  tokenAuthenticator,
  usernamePasswordAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";

// This is the main entry point for the NATS connection.
// It is immediately called when the page loads.
(async () => {
  try {
    // Get JWT from localStorage
    const token = localStorage.getItem(LSATK);
    if (!token) {
      throw new Error("No JWT found in localStorage");
    }
    // make a request to the server to check if the token is valid
    const response = await fetch(`${ENDPOINT}/test-bearer-token`, {
      method: "GET",
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });
    if (!response.ok) {
      // refresh the token
      const refreshResponse = await fetch(`${ENDPOINT}/token`, {
        method: "POST",
        headers: {
          Authorization: `Basic ${btoa(
            `${BASIC_AUTH_EMAIL}:${BASIC_AUTH_PASSWORD}`
          )}`,
        },
      });
      if (!refreshResponse.ok) {
        throw new Error("Failed to refresh JWT");
      }
      const refreshToken = await refreshResponse.json();
      const newToken = refreshToken.token;
      localStorage.setItem(LSATK, newToken);
    }
    console.log("Connecting to NATS server", NATS_URL);
    const nc = await connect({
      servers: [NATS_URL],
      authenticator: tokenAuthenticator(token),
    });

    console.log("Connected to NATS server");
    const sub = nc.subscribe("godxfeed");

    // Process incoming messages
    for await (const msg of sub) {
      const parsed = JSON.parse(new TextDecoder().decode(msg.data));
      console.log("received", parsed);
    }
    return nc;
  } catch (error) {
    console.error("Failed to connect to NATS server:", error);
    throw error;
  }
})();

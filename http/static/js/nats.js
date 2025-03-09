import {
  StringCodec,
  connect,
  millis,
  tokenAuthenticator,
  usernamePasswordAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";
import { ensureValidToken } from "./auth.js";

// This is the main entry point for the NATS connection.
// It is immediately called when the page loads.
async function runNATSConnection() {
  const basicAuth = `Basic ${btoa(
    `${BASIC_AUTH_EMAIL}:${BASIC_AUTH_PASSWORD}`
  )}`;
  const token = await ensureValidToken(ENDPOINT, LSATK, basicAuth);

  console.log("Connected to NATS server");
  const sub = nc.subscribe("godxfeed");

  // Process incoming messages
  for await (const msg of sub) {
    const parsed = JSON.parse(new TextDecoder().decode(msg.data));
    console.log("received", parsed);
  }
}

// This is the main entry point for the NATS connection.
// It will run when the page loads.
$(document).ready(async () => await runNATSConnection());

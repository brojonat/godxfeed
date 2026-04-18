import {
  StringCodec,
  connect,
  tokenAuthenticator,
} from "https://cdn.jsdelivr.net/npm/nats.ws@1.10.0/esm/nats.js";
import { ensureValidToken, authenticatedFetch } from "./auth.js";
import { AUTH_CONFIG } from "./config.js";
import { showLoginModal } from "./modal.js";
import { natsToObservable, startSymbolPlots } from "./admin_plots.js";

// ──────────────────────────────────────────────────────────────────────────
// Poll loop for status + subscriptions tables. Every second we re-fetch
// both endpoints and re-render. Dumb but enough — the tables rarely
// change; the interesting realtime data lives in the NATS tail below.
// ──────────────────────────────────────────────────────────────────────────

const POLL_INTERVAL_MS = 1000;
const TAIL_RING_SIZE = 100;

async function pollStatus() {
  try {
    const r = await authenticatedFetch(`/dxlink/status`);
    if (!r.ok) throw new Error(`status ${r.status}`);
    renderStatus(await r.json());
  } catch (e) {
    renderStatusError(e);
  }
}

function renderStatus(s) {
  const card = document.getElementById("status-card");
  card.classList.remove("loading", "degraded");
  card.classList.add(s.connected && s.authenticated ? "connected" : "degraded");
  document.getElementById("status-connected").textContent = s.connected ? "connected" : "disconnected";
  document.getElementById("status-connected").className = s.connected ? "ok" : "bad";
  document.getElementById("status-authed").textContent = s.authenticated ? "yes" : "no";
  document.getElementById("status-authed").className = s.authenticated ? "ok" : "bad";
  document.getElementById("status-url").textContent = s.dxlinkURL || "—";
  document.getElementById("status-refreshed").textContent = new Date().toLocaleTimeString();
}

function renderStatusError(e) {
  const card = document.getElementById("status-card");
  card.classList.remove("connected", "loading");
  card.classList.add("degraded");
  document.getElementById("status-connected").textContent = "error";
  document.getElementById("status-connected").className = "bad";
  document.getElementById("status-authed").textContent = "—";
  document.getElementById("status-url").textContent = String(e);
  document.getElementById("status-refreshed").textContent = new Date().toLocaleTimeString();
}

async function pollSubscriptions() {
  try {
    const r = await authenticatedFetch(`/dxlink/subscriptions`);
    if (!r.ok) throw new Error(`status ${r.status}`);
    renderSubscriptions(await r.json());
  } catch (e) {
    console.warn("poll /dxlink/subscriptions failed:", e);
  }
}

function renderSubscriptions(rows) {
  const body = document.getElementById("subs-body");
  if (!rows || rows.length === 0) {
    body.innerHTML = `<tr class="empty"><td colspan="7">no subscriptions yet</td></tr>`;
    return;
  }
  body.innerHTML = rows
    .map(
      (r) => `<tr>
        <td>${escapeHTML(r.event)}</td>
        <td>${escapeHTML(r.symbol)}</td>
        <td><code>${escapeHTML(r.subject)}</code></td>
        <td class="num">${r.msgCount.toLocaleString()}</td>
        <td>${fmtTime(r.firstSeenAt)}</td>
        <td>${fmtTime(r.lastSeenAt)}</td>
        <td><button class="remove-sub" data-event="${escapeAttr(r.event)}" data-symbol="${escapeAttr(r.symbol)}">Remove</button></td>
      </tr>`
    )
    .join("");
}

// Wire up the add form and delegate clicks on the remove buttons. The
// buttons are re-rendered on every poll tick, so delegation avoids having
// to re-attach listeners.
function wireSubscriptionControls() {
  const form = document.getElementById("add-sub-form");
  const statusEl = document.getElementById("add-sub-status");
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const event = document.getElementById("add-event").value.trim() || "Quote";
    const symbol = document.getElementById("add-symbol").value.trim();
    if (!symbol) return;
    statusEl.textContent = `adding ${symbol}…`;
    try {
      const r = await authenticatedFetch("/dxlink/subscriptions", null, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ event, symbol }),
      });
      if (!r.ok) throw new Error(`status ${r.status}`);
      statusEl.textContent = `added ${symbol}`;
      document.getElementById("add-symbol").value = "";
      pollSubscriptions();
    } catch (err) {
      statusEl.textContent = `error: ${err}`;
    }
  });

  document.getElementById("subs-body").addEventListener("click", async (e) => {
    const btn = e.target.closest("button.remove-sub");
    if (!btn) return;
    const event = btn.dataset.event;
    const symbol = btn.dataset.symbol;
    btn.disabled = true;
    btn.textContent = "removing…";
    try {
      const r = await authenticatedFetch("/dxlink/subscriptions", null, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ event, symbol }),
      });
      if (!r.ok) throw new Error(`status ${r.status}`);
      pollSubscriptions();
    } catch (err) {
      btn.disabled = false;
      btn.textContent = "Remove";
      console.error(`remove ${event}/${symbol} failed:`, err);
    }
  });
}

function escapeAttr(s) {
  return String(s ?? "").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

// ──────────────────────────────────────────────────────────────────────────
// Live tail of `godxfeed.>`. Plain DOM append with a ring-buffer ceiling —
// no rxjs, but the structure is deliberately simple so swapping to an
// Observable pipeline later is a drop-in.
// ──────────────────────────────────────────────────────────────────────────

const tailState = {
  paused: false,
  lines: [], // {subj, payload, ts}
  msgThisSecond: 0,
  lastRateTick: Date.now(),
};

function pushTailLine(subj, payload) {
  if (tailState.paused) return;
  tailState.msgThisSecond += 1;
  tailState.lines.push({ subj, payload, ts: Date.now() });
  if (tailState.lines.length > TAIL_RING_SIZE) tailState.lines.shift();
  renderTail();
}

function renderTail() {
  const tail = document.getElementById("tail");
  tail.innerHTML = tailState.lines
    .map(
      (l) =>
        `<div class="tail-line"><span class="ts">${new Date(l.ts).toLocaleTimeString()}</span><span class="subj">${escapeHTML(l.subj)}</span>${escapeHTML(l.payload)}</div>`
    )
    .join("");
  tail.scrollTop = tail.scrollHeight;
}

function tickRate() {
  const now = Date.now();
  const dtSec = (now - tailState.lastRateTick) / 1000;
  if (dtSec <= 0) return;
  const rate = tailState.msgThisSecond / dtSec;
  document.getElementById("tail-rate").textContent = `${rate.toFixed(1)} msg/s`;
  tailState.msgThisSecond = 0;
  tailState.lastRateTick = now;
}

function wireTailControls() {
  const pauseBtn = document.getElementById("tail-pause");
  pauseBtn.addEventListener("click", () => {
    tailState.paused = !tailState.paused;
    pauseBtn.textContent = tailState.paused ? "Resume" : "Pause";
    pauseBtn.classList.toggle("active", tailState.paused);
  });
  document.getElementById("tail-clear").addEventListener("click", () => {
    tailState.lines = [];
    renderTail();
  });
}

// ──────────────────────────────────────────────────────────────────────────
// Main bootstrap
// ──────────────────────────────────────────────────────────────────────────

async function runAdmin() {
  wireTailControls();
  wireSubscriptionControls();
  const token = await ensureValidToken();

  // Poll status / subscriptions in parallel on an interval.
  await Promise.all([pollStatus(), pollSubscriptions()]);
  setInterval(() => {
    pollStatus();
    pollSubscriptions();
    tickRate();
  }, POLL_INTERVAL_MS);

  // Tail every message flowing through the godxfeed subject tree AND feed
  // the per-symbol distribution plots. Both are driven by the same NATS
  // connection but tap it via two separate subscriptions: the tail uses the
  // classic async-iterator consumer, and the plots go through an rxjs
  // pipeline (groupBy → scan → throttleTime) so re-renders coalesce without
  // blocking the tail.
  try {
    const nc = await connect({
      servers: [NATS_URL],
      authenticator: tokenAuthenticator(token),
    });
    const decoder = new StringCodec();

    // Tail (unchanged — plain DOM append, ring buffer).
    const tailSub = nc.subscribe("godxfeed.>");
    (async () => {
      for await (const m of tailSub) {
        pushTailLine(m.subject, decoder.decode(m.data));
      }
    })();

    // Plots (rxjs-driven, per-symbol panels).
    const msgs$ = natsToObservable(nc, "godxfeed.>", decoder);
    const plotsContainer = document.getElementById("plots-grid");
    startSymbolPlots(msgs$, plotsContainer);
  } catch (e) {
    console.error("NATS connect failed:", e);
  }
}

function escapeHTML(s) {
  return String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function fmtTime(t) {
  if (!t) return "—";
  const d = new Date(t);
  // The server sends RFC 3339 timestamps; check for zero-value (Go sends "0001-01-01T…")
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return "—";
  return d.toLocaleTimeString();
}

document.addEventListener("DOMContentLoaded", async () => {
  // Extract URL-bound token (same pattern as plots.js).
  const urlParams = new URLSearchParams(window.location.search);
  const urlToken = urlParams.get("token");
  if (urlToken) {
    localStorage.setItem(AUTH_CONFIG.tokenKey, urlToken);
    urlParams.delete("token");
    const newUrl = `${window.location.pathname}${
      urlParams.toString() ? "?" + urlParams.toString() : ""
    }`;
    window.history.replaceState({}, document.title, newUrl);
  }

  const token = localStorage.getItem(AUTH_CONFIG.tokenKey);
  if (!token) {
    showLoginModal(runAdmin);
    return;
  }
  try {
    const response = await fetch(AUTH_CONFIG.endpoints.testToken, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!response.ok) throw new Error("Invalid token");
    await runAdmin();
  } catch (e) {
    console.error("Token validation error:", e);
    localStorage.removeItem(AUTH_CONFIG.tokenKey);
    showLoginModal(runAdmin);
  }
});

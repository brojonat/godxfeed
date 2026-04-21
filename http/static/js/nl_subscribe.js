// Natural-language subscription form on /admin.
//
// Two capabilities:
//   1. Form submit → POST /nl-subscribe {text, provider?}. Renders
//      the echo'd FilterSpec + resolved subs so the operator can
//      confirm the LLM's interpretation.
//   2. Voice input via the Web Speech API. Supported in Chromium-
//      based browsers; unsupported browsers get a disabled mic
//      button with a tooltip explaining why.
//
// No dependencies beyond auth.js — rxjs/D3 not needed here.

import { authenticatedFetch } from "./auth.js";

export function wireNLSubscribe() {
  const form      = document.getElementById("nl-form");
  const textEl    = document.getElementById("nl-text");
  const providerEl= document.getElementById("nl-provider");
  const micBtn    = document.getElementById("nl-mic");
  const statusEl  = document.getElementById("nl-status");
  const resultEl  = document.getElementById("nl-result");

  if (!form || !textEl) return;

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = textEl.value.trim();
    if (!text) return;
    statusEl.textContent = "thinking…";
    resultEl.hidden = true;
    try {
      const body = { text };
      if (providerEl.value) body.provider = providerEl.value;
      const r = await authenticatedFetch("/nl-subscribe", null, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!r.ok) {
        const errBody = await r.text();
        throw new Error(`HTTP ${r.status}: ${errBody.slice(0, 200)}`);
      }
      const data = await r.json();
      statusEl.textContent = `added ${data.subs.length} sub(s) via ${data.provider}`;
      renderResult(data);
      resultEl.hidden = false;
      // Nudge the subs table to refresh so the new rows show up
      // without waiting for the next 1s poll tick.
      window.dispatchEvent(new Event("nl-subscribe:applied"));
    } catch (err) {
      statusEl.textContent = `error: ${err.message}`;
    }
  });

  wireVoiceInput({ textEl, micBtn, statusEl });
}

function renderResult(data) {
  const filterEl = document.getElementById("nl-filter-echo");
  const subsEl   = document.getElementById("nl-subs-list");

  const f = data.filter || {};
  const bits = [`<code>root=${escapeHTML(f.root || "?")}</code>`];
  if (f.kind) bits.push(`<code>kind=${escapeHTML(f.kind)}</code>`);
  if (Array.isArray(f.event_types) && f.event_types.length) {
    bits.push(`<code>events=${escapeHTML(f.event_types.join(","))}</code>`);
  }
  if (f.strike_window) {
    bits.push(`<code>strikes ${f.strike_window.min}–${f.strike_window.max}</code>`);
  }
  if (f.expiry_window) {
    bits.push(`<code>expiry ${escapeHTML(f.expiry_window.min)}→${escapeHTML(f.expiry_window.max)}</code>`);
  }
  if (f.include_equity) bits.push(`<code>+equity</code>`);
  filterEl.innerHTML = `<div class="nl-filter"><span class="muted">filter:</span> ${bits.join(" ")}</div>`;

  const subs = data.subs || [];
  if (subs.length === 0) {
    subsEl.innerHTML = `<div class="muted">(no matching subscriptions — nothing dispatched)</div>`;
    return;
  }
  subsEl.innerHTML = `
    <table class="nl-subs">
      <thead><tr><th>Event</th><th>Symbol</th><th>Subject</th></tr></thead>
      <tbody>
        ${subs.map((s) => `<tr>
          <td>${escapeHTML(s.event)}</td>
          <td><code>${escapeHTML(s.symbol)}</code></td>
          <td><code>${escapeHTML(s.subject)}</code></td>
        </tr>`).join("")}
      </tbody>
    </table>`;
}

// Web Speech API wrapper. Fills the textarea with live transcription
// while the user speaks; clicking again stops early. Keeps any text
// the user had typed before — we append rather than overwrite.
function wireVoiceInput({ textEl, micBtn, statusEl }) {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SR) {
    micBtn.disabled = true;
    micBtn.title = "Web Speech API not supported in this browser (try Chrome/Edge)";
    return;
  }
  const recog = new SR();
  recog.lang = "en-US";
  recog.interimResults = true;
  recog.continuous = false;

  let listening = false;
  // Lock in whatever text was in the textarea when the user started,
  // so interim results append without clobbering typed content.
  let baseline = "";

  micBtn.addEventListener("click", () => {
    if (listening) {
      recog.stop();
      return;
    }
    baseline = textEl.value;
    if (baseline && !baseline.endsWith(" ")) baseline += " ";
    try {
      recog.start();
    } catch (e) {
      statusEl.textContent = `voice error: ${e.message}`;
    }
  });

  recog.onstart = () => {
    listening = true;
    micBtn.textContent = "Stop";
    micBtn.classList.add("listening");
    statusEl.textContent = "listening…";
  };
  recog.onend = () => {
    listening = false;
    micBtn.textContent = "Voice";
    micBtn.classList.remove("listening");
    if (statusEl.textContent === "listening…") statusEl.textContent = "";
  };
  recog.onresult = (evt) => {
    let interim = "";
    let finalized = baseline;
    for (let i = 0; i < evt.results.length; i++) {
      const r = evt.results[i];
      if (r.isFinal) finalized += r[0].transcript + " ";
      else interim += r[0].transcript;
    }
    baseline = finalized;
    textEl.value = finalized + interim;
  };
  recog.onerror = (evt) => {
    // "no-speech" and "aborted" are expected user-initiated stops;
    // don't surface them as scary errors.
    if (evt.error === "no-speech" || evt.error === "aborted") return;
    statusEl.textContent = `voice error: ${evt.error}`;
  };
}

function escapeHTML(s) {
  return String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

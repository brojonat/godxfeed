"""Periodic PyMC posterior publisher.

Subscribes to ``godxfeed.quote.>`` (quote firehose), buffers the last
~60s of mid prices per symbol, and every ``interval_s`` fits a PyMC
model on the last 30s of ticks. The posterior-predictive density over
the next mid is published to::

    godxfeed.analytics.posterior.<SYMBOL>

in the shape the frontend overlay already consumes::

    {"type": "posterior", "symbol": "SPY", "xs": [...], "ys": [...], "at": <ms>}

Design notes:
  * One ingest coroutine owns the NATS subscription. It pushes into
    per-symbol ``QuoteBuffer``s — no cross-task coordination beyond the
    buffer's single-threaded append.
  * Each fit runs in a worker thread (``asyncio.to_thread``) so NUTS
    compilation and sampling don't block the event loop. Downside: a
    slow fit delays the *next* fit for that symbol, but we keep
    ingesting throughout.
  * Fit failures are logged and skipped — the next interval gets a
    fresh attempt. A single bad window must not take the service down.
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from dataclasses import dataclass, field
from typing import Protocol

import numpy as np

from analytics.buffer import QuoteBuffer
from analytics.posterior import (
    env_float,
    env_int,
    fit_sigma_posterior,
    mids_to_log_returns,
    predictive_density,
)

log = logging.getLogger("analytics.publisher")


class NatsClient(Protocol):
    async def publish(self, subject: str, payload: bytes) -> None: ...
    async def subscribe(self, subject: str): ...  # returns a nats Subscription


class JetStreamContext(Protocol):
    async def subscribe(self, subject: str, **kwargs): ...  # returns a JetStream Subscription


class NatsSubscription(Protocol):
    @property
    def messages(self): ...  # async iterator of nats Msg
    async def unsubscribe(self) -> None: ...


# ─────────────────────────────────────────────────────────────────────
# Config
# ─────────────────────────────────────────────────────────────────────


@dataclass(frozen=True)
class PosteriorConfig:
    fit_window_s: float = 30.0
    buffer_window_s: float = 60.0
    min_samples: int = 20
    sigma_prior: float = 0.01
    draws: int = 200
    tune: int = 200
    chains: int = 2
    n_grid: int = 100
    # Hard cap on |log-return| fed into the fit. A 5% tick-to-tick log
    # return is already astronomically large for liquid equities at
    # sub-second cadence; anything above that is essentially always a
    # data artifact (replay loop wraparound, stale cross, etc.) rather
    # than a real market move. Dropping the sample keeps σ̂ honest.
    max_abs_log_return: float = 0.05

    @classmethod
    def from_env(cls) -> "PosteriorConfig":
        return cls(
            fit_window_s=env_float("ANALYTICS_FIT_WINDOW_S", 30.0),
            buffer_window_s=env_float("ANALYTICS_BUFFER_WINDOW_S", 60.0),
            min_samples=env_int("ANALYTICS_MIN_SAMPLES", 20),
            sigma_prior=env_float("ANALYTICS_SIGMA_PRIOR", 0.01),
            draws=env_int("ANALYTICS_DRAWS", 200),
            tune=env_int("ANALYTICS_TUNE", 200),
            chains=env_int("ANALYTICS_CHAINS", 2),
            n_grid=env_int("ANALYTICS_N_GRID", 100),
            max_abs_log_return=env_float("ANALYTICS_MAX_ABS_R", 0.05),
        )


# ─────────────────────────────────────────────────────────────────────
# Wire contract
# ─────────────────────────────────────────────────────────────────────


def subject_for(symbol: str) -> str:
    return f"godxfeed.analytics.posterior.{symbol}"


def build_payload(symbol: str, xs: np.ndarray, ys: np.ndarray) -> dict:
    return {
        "type": "posterior",
        "symbol": symbol,
        "xs": [float(x) for x in xs],
        "ys": [float(y) for y in ys],
        "at": int(time.time() * 1000),
    }


# ─────────────────────────────────────────────────────────────────────
# Ingest: NATS → per-symbol buffers
# ─────────────────────────────────────────────────────────────────────


def _parse_quote(raw: bytes) -> tuple[str, float] | None:
    """Extract (symbol, mid) from a raw dxLink Quote JSON message.

    Returns ``None`` for non-Quote events or malformed payloads.
    """
    try:
        data = json.loads(raw)
    except (ValueError, TypeError):
        return None
    if data.get("eventType") != "Quote":
        return None
    sym = data.get("eventSymbol")
    try:
        bid = float(data.get("bidPrice"))
        ask = float(data.get("askPrice"))
    except (TypeError, ValueError):
        return None
    if not sym or bid <= 0 or ask <= 0 or bid > ask:
        return None
    return sym, 0.5 * (bid + ask)


async def _subscribe_jetstream(
    js: JetStreamContext,
) -> NatsSubscription:
    """Try JetStream push-subscribe with DeliverAll for replay on restart."""
    from nats.js.api import DeliverPolicy

    return await js.subscribe(
        "godxfeed.quote.>",
        deliver_policy=DeliverPolicy.ALL,
        ordered_consumer=True,
    )


async def ingest_quotes(
    nc: NatsClient,
    buffers: dict[str, QuoteBuffer],
    buffer_window_s: float,
    stop_event: asyncio.Event,
    js: JetStreamContext | None = None,
) -> None:
    """Own the NATS subscription until ``stop_event`` is set.

    When a JetStream context is provided, subscribes via JetStream with
    ``DeliverPolicy.ALL`` so the sidecar replays the stream's retention
    window on restart (refilling its in-memory buffers). Falls back to
    a plain NATS core subscription if JetStream is unavailable.
    """
    use_js = False
    if js is not None:
        try:
            sub = await _subscribe_jetstream(js)
            use_js = True
            log.info("ingest: JetStream subscribe to godxfeed.quote.> (replay-capable)")
        except Exception as exc:  # noqa: BLE001
            log.warning(
                "ingest: JetStream subscribe failed, falling back to plain NATS: %s",
                exc,
            )

    if not use_js:
        sub = await nc.subscribe("godxfeed.quote.>")
        log.info("ingest: plain NATS subscribe to godxfeed.quote.>")

    try:
        async for msg in sub.messages:
            if stop_event.is_set():
                break
            parsed = _parse_quote(msg.data)
            if parsed is None:
                continue
            sym, mid = parsed
            buf = buffers.get(sym)
            if buf is None:
                buf = QuoteBuffer(window_s=buffer_window_s)
                buffers[sym] = buf
            buf.append(time.time(), mid)
    finally:
        try:
            await sub.unsubscribe()
        except Exception:  # noqa: BLE001 — best-effort teardown
            pass
        log.info("ingest: unsubscribed")


# ─────────────────────────────────────────────────────────────────────
# Fit + publish
# ─────────────────────────────────────────────────────────────────────


@dataclass
class FitResult:
    symbol: str
    n_samples: int
    sigma_mean: float
    sigma_std: float
    fit_seconds: float
    xs: np.ndarray
    ys: np.ndarray


def _fit_sync(
    mids: np.ndarray, cfg: PosteriorConfig
) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    """Blocking fit + predictive build. Run under ``asyncio.to_thread``."""
    log_returns = mids_to_log_returns(mids, max_abs_r=cfg.max_abs_log_return)
    if len(log_returns) < 2:
        raise ValueError(
            "not enough log-returns after outlier filter "
            f"(cap={cfg.max_abs_log_return})"
        )
    sigma_samples = fit_sigma_posterior(
        log_returns,
        sigma_prior=cfg.sigma_prior,
        draws=cfg.draws,
        tune=cfg.tune,
        chains=cfg.chains,
    )
    xs, ys = predictive_density(
        latest_mid=float(mids[-1]),
        sigma_samples=sigma_samples,
        n_grid=cfg.n_grid,
    )
    return sigma_samples, xs, ys


async def fit_and_publish(
    nc: NatsClient,
    symbol: str,
    buf: QuoteBuffer,
    cfg: PosteriorConfig,
) -> FitResult | None:
    now = time.time()
    samples = buf.window(now, cfg.fit_window_s)
    if len(samples) < cfg.min_samples:
        log.debug(
            "skip %s: %d < %d samples in last %.1fs",
            symbol,
            len(samples),
            cfg.min_samples,
            cfg.fit_window_s,
        )
        return None

    mids = np.asarray([m for _, m in samples], dtype=float)
    t0 = time.monotonic()
    try:
        sigma_samples, xs, ys = await asyncio.to_thread(_fit_sync, mids, cfg)
    except Exception as exc:  # noqa: BLE001 — keep the service alive
        log.exception("%s: fit failed: %s", symbol, exc)
        return None
    fit_s = time.monotonic() - t0

    payload = build_payload(symbol, xs, ys)
    try:
        await nc.publish(subject_for(symbol), json.dumps(payload).encode("utf-8"))
    except Exception as exc:  # noqa: BLE001
        log.exception("%s: publish failed: %s", symbol, exc)
        return None

    result = FitResult(
        symbol=symbol,
        n_samples=len(mids),
        sigma_mean=float(sigma_samples.mean()),
        sigma_std=float(sigma_samples.std()),
        fit_seconds=fit_s,
        xs=xs,
        ys=ys,
    )
    log.info(
        "%s: n=%d σ=%.4e ± %.4e fit=%.2fs",
        result.symbol,
        result.n_samples,
        result.sigma_mean,
        result.sigma_std,
        result.fit_seconds,
    )
    return result


# ─────────────────────────────────────────────────────────────────────
# Top-level loop (called from app.py via lifespan)
# ─────────────────────────────────────────────────────────────────────


@dataclass
class _LoopState:
    buffers: dict[str, QuoteBuffer] = field(default_factory=dict)


async def publish_loop(
    nc: NatsClient,
    symbols: tuple[str, ...],
    interval_s: float,
    stop_event: asyncio.Event,
    cfg: PosteriorConfig | None = None,
    js: JetStreamContext | None = None,
) -> None:
    """Run until ``stop_event`` is set.

    Two concurrent tasks:
      * ``ingest_quotes`` — perpetual, owns the NATS subscription.
      * the outer loop here — refits each configured symbol every
        ``interval_s``.

    When ``js`` is provided, quote ingestion uses JetStream with replay
    capability. Falls back to plain NATS core subscribe otherwise.
    """
    cfg = cfg or PosteriorConfig.from_env()
    state = _LoopState()
    for s in symbols:
        state.buffers[s] = QuoteBuffer(window_s=cfg.buffer_window_s)

    log.info(
        "publish loop starting: symbols=%s interval=%.2fs fit_window=%.1fs draws=%d tune=%d",
        list(symbols),
        interval_s,
        cfg.fit_window_s,
        cfg.draws,
        cfg.tune,
    )

    ingest = asyncio.create_task(
        ingest_quotes(nc, state.buffers, cfg.buffer_window_s, stop_event, js=js),
        name="analytics-ingest",
    )

    try:
        while not stop_event.is_set():
            for sym in symbols:
                buf = state.buffers.get(sym)
                if buf is None:
                    continue
                try:
                    await fit_and_publish(nc, sym, buf, cfg)
                except Exception:  # noqa: BLE001 — truly defensive
                    log.exception("fit_and_publish(%s)", sym)
            try:
                await asyncio.wait_for(stop_event.wait(), timeout=interval_s)
            except asyncio.TimeoutError:
                pass
    finally:
        ingest.cancel()
        try:
            await ingest
        except (asyncio.CancelledError, Exception):  # noqa: BLE001
            pass
        log.info("publish loop stopped")

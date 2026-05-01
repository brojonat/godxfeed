"""Tests for analytics.publisher.

The wire-contract and ingest/plumbing tests are here. The PyMC-driven
fit is tested in ``test_posterior.py``; here we stub it out with
monkeypatched fakes so we aren't paying NUTS compile cost on every run.
"""

from __future__ import annotations

import asyncio
import json
import time

import numpy as np
import pytest

from analytics.buffer import QuoteBuffer
from analytics.publisher import (
    PosteriorConfig,
    _parse_quote,
    build_payload,
    fit_and_publish,
    ingest_quotes,
    subject_for,
)


# ─────────────────────────────────────────────────────────────────────
# Wire-contract bits
# ─────────────────────────────────────────────────────────────────────


def test_subject_for():
    assert subject_for("SPY") == "godxfeed.analytics.posterior.SPY"


def test_build_payload_contract():
    xs = np.linspace(99.0, 101.0, 10)
    ys = np.full_like(xs, 0.5)
    p = build_payload("SPY", xs, ys)
    assert p["type"] == "posterior"
    assert p["symbol"] == "SPY"
    assert len(p["xs"]) == len(p["ys"]) == 10
    assert all(isinstance(v, float) for v in p["xs"])
    assert all(isinstance(v, float) for v in p["ys"])
    assert isinstance(p["at"], int)


# ─────────────────────────────────────────────────────────────────────
# Quote parser
# ─────────────────────────────────────────────────────────────────────


def test_parse_quote_happy_path():
    raw = json.dumps(
        {
            "eventType": "Quote",
            "eventSymbol": "SPY",
            "bidPrice": 100.0,
            "askPrice": 100.2,
        }
    ).encode()
    assert _parse_quote(raw) == ("SPY", 100.1)


@pytest.mark.parametrize(
    "payload",
    [
        # wrong event type
        {"eventType": "Greeks", "eventSymbol": "SPY", "bidPrice": 1, "askPrice": 2},
        # missing symbol
        {"eventType": "Quote", "bidPrice": 1, "askPrice": 2},
        # crossed book
        {"eventType": "Quote", "eventSymbol": "SPY", "bidPrice": 2, "askPrice": 1},
        # non-positive price
        {"eventType": "Quote", "eventSymbol": "SPY", "bidPrice": 0, "askPrice": 2},
    ],
)
def test_parse_quote_drops_malformed(payload):
    assert _parse_quote(json.dumps(payload).encode()) is None


def test_parse_quote_drops_garbage_bytes():
    assert _parse_quote(b"not json") is None


# ─────────────────────────────────────────────────────────────────────
# fit_and_publish — monkeypatch the PyMC fit away so we don't pay the
# sampling cost. We just want to verify the plumbing.
# ─────────────────────────────────────────────────────────────────────


class _FakeMsg:
    def __init__(self, data: bytes) -> None:
        self.data = data


class _FakeSub:
    """Fake NATS subscription that yields canned messages then stops."""

    def __init__(self, msgs: list[bytes]) -> None:
        self._msgs = msgs

    @property
    def messages(self):
        return self._iter_msgs()

    async def _iter_msgs(self):
        for raw in self._msgs:
            yield _FakeMsg(raw)

    async def unsubscribe(self) -> None:
        pass


class _FakeNC:
    def __init__(self, msgs: list[bytes] | None = None) -> None:
        self.published: list[tuple[str, bytes]] = []
        self._msgs = msgs or []
        self.subscribed_subject: str | None = None

    async def publish(self, subject: str, payload: bytes) -> None:
        self.published.append((subject, payload))

    async def subscribe(self, subject: str):
        self.subscribed_subject = subject
        return _FakeSub(self._msgs)


class _FakeJS:
    """Fake JetStream context that returns a canned subscription."""

    def __init__(self, msgs: list[bytes] | None = None, *, fail: bool = False) -> None:
        self._msgs = msgs or []
        self._fail = fail
        self.subscribed_subject: str | None = None

    async def subscribe(self, subject: str, **kwargs):
        if self._fail:
            raise RuntimeError("JetStream unavailable")
        self.subscribed_subject = subject
        return _FakeSub(self._msgs)


@pytest.mark.asyncio
async def test_fit_and_publish_skips_insufficient_samples():
    nc = _FakeNC()
    buf = QuoteBuffer(window_s=60.0)
    now = time.time()
    # Only 5 samples — under the default 20 threshold.
    for i in range(5):
        buf.append(now - (5 - i), 100.0 + i * 0.01)

    cfg = PosteriorConfig(min_samples=20)
    result = await fit_and_publish(nc, "SPY", buf, cfg)
    assert result is None
    assert nc.published == []


@pytest.mark.asyncio
async def test_fit_and_publish_happy_path(monkeypatch):
    nc = _FakeNC()
    buf = QuoteBuffer(window_s=60.0)
    now = time.time()
    for i in range(30):
        buf.append(now - (30 - i) * 0.5, 100.0 + i * 0.001)

    # Stub the PyMC fit out.
    fake_sigmas = np.full(50, 0.01)
    monkeypatch.setattr(
        "analytics.publisher.fit_sigma_posterior",
        lambda *a, **k: fake_sigmas,
    )

    cfg = PosteriorConfig(min_samples=20, n_grid=50)
    result = await fit_and_publish(nc, "SPY", buf, cfg)
    assert result is not None
    assert result.symbol == "SPY"
    assert result.n_samples == 30
    assert result.sigma_mean == pytest.approx(0.01)
    assert len(nc.published) == 1

    subject, payload = nc.published[0]
    assert subject == "godxfeed.analytics.posterior.SPY"
    msg = json.loads(payload)
    assert msg["type"] == "posterior"
    assert msg["symbol"] == "SPY"
    assert len(msg["xs"]) == len(msg["ys"]) == 50
    # ∫ y dx ≈ 1 — trapz-normalized inside predictive_density.
    xs = np.asarray(msg["xs"])
    ys = np.asarray(msg["ys"])
    assert float(np.trapezoid(ys, xs)) == pytest.approx(1.0, rel=1e-2)


@pytest.mark.asyncio
async def test_fit_and_publish_survives_fit_error(monkeypatch):
    nc = _FakeNC()
    buf = QuoteBuffer(window_s=60.0)
    now = time.time()
    for i in range(30):
        buf.append(now - (30 - i) * 0.5, 100.0 + i * 0.001)

    def boom(*a, **k):
        raise RuntimeError("NUTS exploded")

    monkeypatch.setattr("analytics.publisher.fit_sigma_posterior", boom)

    cfg = PosteriorConfig(min_samples=20)
    result = await fit_and_publish(nc, "SPY", buf, cfg)
    assert result is None
    assert nc.published == []


# ─────────────────────────────────────────────────────────────────────
# ingest_quotes — JetStream vs plain NATS paths
# ─────────────────────────────────────────────────────────────────────


def _quote_bytes(sym: str, bid: float, ask: float) -> bytes:
    return json.dumps(
        {"eventType": "Quote", "eventSymbol": sym, "bidPrice": bid, "askPrice": ask}
    ).encode()


@pytest.mark.asyncio
async def test_ingest_quotes_jetstream_path():
    """When a JetStream context is provided, ingestion uses it."""
    msgs = [_quote_bytes("SPY", 100.0, 100.2)]
    nc = _FakeNC()
    js = _FakeJS(msgs)
    buffers: dict[str, QuoteBuffer] = {}
    stop = asyncio.Event()
    await ingest_quotes(nc, buffers, 60.0, stop, js=js)
    assert js.subscribed_subject == "godxfeed.quote.>"
    assert nc.subscribed_subject is None  # did NOT fall back to plain NATS
    assert "SPY" in buffers
    assert len(buffers["SPY"]) == 1


@pytest.mark.asyncio
async def test_ingest_quotes_fallback_on_js_failure():
    """If JetStream subscribe fails, falls back to plain NATS."""
    msgs = [_quote_bytes("AAPL", 150.0, 150.2)]
    nc = _FakeNC(msgs)
    js = _FakeJS(fail=True)
    buffers: dict[str, QuoteBuffer] = {}
    stop = asyncio.Event()
    await ingest_quotes(nc, buffers, 60.0, stop, js=js)
    assert nc.subscribed_subject == "godxfeed.quote.>"
    assert "AAPL" in buffers
    assert len(buffers["AAPL"]) == 1


@pytest.mark.asyncio
async def test_ingest_quotes_plain_nats_when_no_js():
    """Without a JetStream context, ingestion uses plain NATS."""
    msgs = [_quote_bytes("QQQ", 300.0, 300.4)]
    nc = _FakeNC(msgs)
    buffers: dict[str, QuoteBuffer] = {}
    stop = asyncio.Event()
    await ingest_quotes(nc, buffers, 60.0, stop, js=None)
    assert nc.subscribed_subject == "godxfeed.quote.>"
    assert "QQQ" in buffers
    assert len(buffers["QQQ"]) == 1

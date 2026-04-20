"""Tests for analytics.buffer.QuoteBuffer."""

from __future__ import annotations

from analytics.buffer import QuoteBuffer


def test_append_retains_within_window():
    buf = QuoteBuffer(window_s=10.0)
    buf.append(100.0, 1.0)
    buf.append(105.0, 2.0)
    assert len(buf) == 2


def test_append_evicts_old_samples_on_advance():
    buf = QuoteBuffer(window_s=10.0)
    buf.append(0.0, 1.0)
    buf.append(5.0, 2.0)
    # Advance — first sample is now 11s old, outside the 10s window.
    buf.append(11.0, 3.0)
    assert [m for _, m in buf.ticks] == [2.0, 3.0]


def test_window_returns_horizon_slice():
    buf = QuoteBuffer(window_s=60.0)
    for t, m in zip(range(0, 60, 5), range(12), strict=True):
        buf.append(float(t), float(m))
    # Last 20s at t=55: t>=35. That's t=35,40,45,50,55 → 5 samples.
    out = buf.window(now=55.0, horizon_s=20.0)
    assert [t for t, _ in out] == [35.0, 40.0, 45.0, 50.0, 55.0]


def test_window_prunes_on_access():
    buf = QuoteBuffer(window_s=10.0)
    buf.append(0.0, 1.0)
    buf.append(5.0, 2.0)
    # Ask for a window where "now" is 100s later; the buffer should
    # drop both samples as part of the prune, not just filter them out.
    out = buf.window(now=100.0, horizon_s=5.0)
    assert out == []
    assert len(buf) == 0

"""Per-symbol rolling windows of (timestamp, mid-price) samples.

The buffer is append-on-tick, prune-on-access. Intentionally simple: one
``deque`` per symbol, bounded by a wall-clock window. No background
pruner task — the fit path calls ``window()`` which prunes anything
older than the configured horizon before returning a snapshot.
"""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
from typing import Deque


@dataclass
class QuoteBuffer:
    """Wall-clock rolling window of (ts_epoch_s, mid_price) tuples.

    ``window_s`` caps how far back we keep samples; anything older is
    dropped on append or on window queries. The class is intentionally
    single-threaded — callers run it inside a single asyncio event loop.
    """

    window_s: float
    ticks: Deque[tuple[float, float]] = field(default_factory=deque)

    def append(self, ts: float, mid: float) -> None:
        self.ticks.append((ts, mid))
        self._prune(ts)

    def _prune(self, now: float) -> None:
        cutoff = now - self.window_s
        while self.ticks and self.ticks[0][0] < cutoff:
            self.ticks.popleft()

    def window(self, now: float, horizon_s: float) -> list[tuple[float, float]]:
        """Return samples within the last ``horizon_s`` of ``now``.

        ``horizon_s`` must be <= the buffer's ``window_s`` to be
        meaningful; larger values are silently clipped by the buffer's
        own retention.
        """
        self._prune(now)
        cutoff = now - horizon_s
        return [(t, m) for t, m in self.ticks if t >= cutoff]

    def __len__(self) -> int:
        return len(self.ticks)

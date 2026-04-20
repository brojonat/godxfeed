"""Runtime config from env vars."""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    nats_url: str
    nats_user: str | None
    nats_password: str | None
    symbols: tuple[str, ...]
    interval_s: float
    host: str
    port: int

    @classmethod
    def from_env(cls, env: dict[str, str] | None = None) -> "Config":
        e = env if env is not None else os.environ
        symbols_csv = e.get("ANALYTICS_SYMBOLS", "SPY,AAPL")
        symbols = tuple(s.strip() for s in symbols_csv.split(",") if s.strip())
        return cls(
            nats_url=e.get("NATS_URL", "nats://localhost:4222"),
            nats_user=e.get("NATS_GODXFEED_USER") or None,
            nats_password=e.get("NATS_GODXFEED_PASSWORD") or None,
            symbols=symbols,
            interval_s=float(e.get("ANALYTICS_INTERVAL_S", "2.0")),
            host=e.get("ANALYTICS_HOST", "0.0.0.0"),
            port=int(e.get("ANALYTICS_PORT", "8090")),
        )

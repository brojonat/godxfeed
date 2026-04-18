"""Generate synthetic quote time series via PyMC prior-predictive sampling.

The generative model is intentionally small and legible so downstream
inference (e.g. a Bayesian sigma sidecar) can share the same spec:

    log_return_t ~ Normal(mu, sigma)
    mid_t        = mid_0 * exp(cumsum(log_return_t))
    spread_t     = spread_pct * mid_t
    bid_t        = mid_t - spread_t / 2
    ask_t        = mid_t + spread_t / 2

Per-symbol trajectories are independent. True (mu, sigma) are written to
a companion `params` table so validators can read the ground truth
alongside the synthetic feed.
"""

from __future__ import annotations

import argparse
import os
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone

import duckdb
import numpy as np
import pymc as pm


@dataclass(frozen=True)
class SymbolParams:
    symbol: str
    mid_start: float
    mu: float
    sigma: float
    spread_pct: float


def sample_prior(params: SymbolParams, n_ticks: int, seed: int) -> np.ndarray:
    """Return an array of mid prices drawn from the prior predictive."""
    with pm.Model():
        pm.Normal("r", mu=params.mu, sigma=params.sigma, shape=n_ticks)
        idata = pm.sample_prior_predictive(draws=1, random_seed=seed)
    log_returns = np.asarray(idata.prior["r"].values).reshape(-1)
    if log_returns.size != n_ticks:
        raise RuntimeError(
            f"prior sample shape mismatch: got {log_returns.size}, want {n_ticks}"
        )
    return params.mid_start * np.exp(np.cumsum(log_returns))


def build_rows(
    params_by_symbol: list[SymbolParams],
    n_ticks: int,
    tick_ms: int,
    start_ns: int,
    seed: int,
) -> list[tuple]:
    rows: list[tuple] = []
    for i, p in enumerate(params_by_symbol):
        mid = sample_prior(p, n_ticks, seed + i)
        spread = p.spread_pct * mid
        for k in range(n_ticks):
            ts_ns = start_ns + k * tick_ms * 1_000_000
            m = float(mid[k])
            s = float(spread[k])
            rows.append(
                (
                    ts_ns,
                    p.symbol,
                    "Quote",
                    m - s / 2.0,
                    m + s / 2.0,
                    100.0,
                    100.0,
                )
            )
    return rows


def write_duckdb(
    path: str,
    rows: list[tuple],
    params_by_symbol: list[SymbolParams],
) -> None:
    # Overwrite — synthetic data is cheap to regenerate; preserving old
    # batches would mix runs with different true-params.
    if os.path.exists(path):
        os.remove(path)
    con = duckdb.connect(path)
    try:
        con.execute(
            """
            CREATE TABLE quotes (
                ts_ns      BIGINT,
                symbol     VARCHAR,
                event_type VARCHAR,
                bid_price  DOUBLE,
                ask_price  DOUBLE,
                bid_size   DOUBLE,
                ask_size   DOUBLE
            )
            """
        )
        con.executemany(
            "INSERT INTO quotes VALUES (?, ?, ?, ?, ?, ?, ?)", rows
        )
        con.execute(
            """
            CREATE TABLE params (
                symbol      VARCHAR,
                mid_start   DOUBLE,
                mu          DOUBLE,
                sigma       DOUBLE,
                spread_pct  DOUBLE
            )
            """
        )
        con.executemany(
            "INSERT INTO params VALUES (?, ?, ?, ?, ?)",
            [
                (p.symbol, p.mid_start, p.mu, p.sigma, p.spread_pct)
                for p in params_by_symbol
            ],
        )
        con.execute("CREATE INDEX idx_quotes_ts ON quotes(ts_ns)")
        con.execute("CREATE INDEX idx_quotes_sym ON quotes(symbol)")
    finally:
        con.close()


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument(
        "--symbols",
        nargs="+",
        default=["SPY", "AAPL"],
        help="Symbols to generate (default: SPY AAPL).",
    )
    ap.add_argument("--n-ticks", type=int, default=3600, help="Ticks per symbol.")
    ap.add_argument("--tick-ms", type=int, default=1000, help="Milliseconds between ticks.")
    ap.add_argument(
        "--start",
        default=None,
        help="ISO-8601 start timestamp (default: now). Playback resumes from this anchor.",
    )
    ap.add_argument("--mid-start", type=float, default=100.0)
    ap.add_argument("--mu", type=float, default=0.0, help="Per-tick log-return drift.")
    ap.add_argument("--sigma", type=float, default=0.001, help="Per-tick log-return volatility.")
    ap.add_argument("--spread-pct", type=float, default=0.001, help="Bid/ask spread as fraction of mid.")
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--out", default="quotes.duckdb", help="Output DuckDB path.")
    return ap.parse_args(argv)


def main() -> int:
    args = parse_args()

    if args.start is None:
        start_ns = time.time_ns()
    else:
        dt = datetime.fromisoformat(args.start)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        start_ns = int(dt.timestamp() * 1e9)

    params_by_symbol = [
        SymbolParams(
            symbol=s,
            mid_start=args.mid_start,
            mu=args.mu,
            sigma=args.sigma,
            spread_pct=args.spread_pct,
        )
        for s in args.symbols
    ]

    print(
        f"[synth.generate] {len(params_by_symbol)} symbols × {args.n_ticks} ticks "
        f"@ {args.tick_ms}ms (μ={args.mu}, σ={args.sigma})",
        file=sys.stderr,
    )
    rows = build_rows(params_by_symbol, args.n_ticks, args.tick_ms, start_ns, args.seed)
    write_duckdb(args.out, rows, params_by_symbol)

    print(
        f"[synth.generate] wrote {len(rows)} rows to {args.out}",
        file=sys.stderr,
    )
    print(args.out)  # stdout: path only (machine-readable)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

"""Bayesian inference + posterior-predictive density construction.

Model (GBM without drift — justified for short windows where μ is not
identifiable):

    log(p_t / p_{t-1}) ~ Normal(0, σ)
    σ ~ HalfNormal(sigma_prior)

Inference via PyMC / NUTS. We return posterior samples of σ and then
construct a posterior-predictive density over the *next* mid price:

    p(p_next | data) = E_{σ | data} [ LogNormal(p_next; log p₀, σ) ]

where p₀ is the latest observed mid. That places the predictive on
the same axis as the observed histogram, so the overlay reads visually
as "where the model thinks the next tick will land."
"""

from __future__ import annotations

import logging
import os

import numpy as np

log = logging.getLogger("analytics.posterior")

# Lazy import of PyMC — it's a heavy dependency and the module shouldn't
# pay the import cost at package-load time (e.g. during tests that don't
# exercise fitting).
_pm = None


def _get_pm():
    global _pm
    if _pm is None:
        import pymc as pm  # noqa: PLC0415 — lazy on purpose

        _pm = pm
    return _pm


def fit_sigma_posterior(
    log_returns: np.ndarray,
    *,
    sigma_prior: float = 0.01,
    draws: int = 200,
    tune: int = 200,
    chains: int = 2,
    random_seed: int | None = None,
) -> np.ndarray:
    """Fit σ via NUTS, return flat posterior samples (shape ``draws*chains``).

    ``sigma_prior`` is the scale of the ``HalfNormal`` prior on σ;
    ~0.01 is weakly informative for short-horizon log-returns of
    liquid equities.
    """
    if log_returns.ndim != 1:
        raise ValueError("log_returns must be 1-D")
    if len(log_returns) < 2:
        raise ValueError("need at least 2 log-returns to fit σ")

    pm = _get_pm()
    with pm.Model():
        sigma = pm.HalfNormal("sigma", sigma=sigma_prior)
        pm.Normal("r", mu=0.0, sigma=sigma, observed=log_returns)
        idata = pm.sample(
            draws=draws,
            tune=tune,
            chains=chains,
            cores=1,  # stay single-process; asyncio + fork is misery
            progressbar=False,
            compute_convergence_checks=False,
            random_seed=random_seed,
            return_inferencedata=True,
        )
    return idata.posterior["sigma"].to_numpy().ravel()


def predictive_density(
    latest_mid: float,
    sigma_samples: np.ndarray,
    *,
    n_grid: int = 100,
    span_sigmas: float = 4.0,
) -> tuple[np.ndarray, np.ndarray]:
    """Posterior-predictive density of the next mid price.

    ``latest_mid`` anchors the one-step-ahead predictive
    ``p_next ~ LogNormal(log latest_mid, σ)``. We evaluate the density on
    a grid spanning ``± span_sigmas * σ̄`` (in log space) around the
    anchor, then Monte-Carlo average the LogNormal kernel across the σ
    posterior and trapz-normalize to ∫ y dx ≈ 1.
    """
    if latest_mid <= 0:
        raise ValueError("latest_mid must be > 0")
    if sigma_samples.size == 0:
        raise ValueError("sigma_samples is empty")

    sigma_bar = float(np.mean(sigma_samples))
    # Guard the degenerate near-zero-σ case — NUTS occasionally returns
    # samples that hug 0, collapsing the grid. Floor at a tiny value so
    # the grid has nonzero width.
    span = max(span_sigmas * sigma_bar, 1e-6)
    lo = latest_mid * np.exp(-span)
    hi = latest_mid * np.exp(span)
    xs = np.linspace(lo, hi, n_grid)

    log_xs = np.log(xs)[:, None]               # (n_grid, 1)
    sig = sigma_samples[None, :]               # (1, S)
    z = (log_xs - np.log(latest_mid)) / sig
    # LogNormal pdf: 1 / (x · σ · √(2π)) · exp(-z²/2)
    kernels = np.exp(-0.5 * z * z) / (xs[:, None] * sig * np.sqrt(2 * np.pi))
    ys = kernels.mean(axis=1)

    mass = float(np.trapezoid(ys, xs))
    if mass > 0:
        ys = ys / mass
    return xs, ys


def mids_to_log_returns(
    mids: np.ndarray, *, max_abs_r: float | None = None
) -> np.ndarray:
    """Convenience: ``log(mids[1:] / mids[:-1])`` with positivity guard.

    If ``max_abs_r`` is given, returns with ``|r| > max_abs_r`` are
    dropped. This is a defensive filter for synthetic / replayed data
    where artificial discontinuities (e.g. the synth mock wrapping its
    replay buffer) produce single huge returns that otherwise drag σ
    posteriors upward for a full fit window.
    """
    mids = np.asarray(mids, dtype=float)
    if np.any(mids <= 0):
        raise ValueError("mids must be strictly positive")
    r = np.diff(np.log(mids))
    if max_abs_r is not None:
        r = r[np.abs(r) <= max_abs_r]
    return r


def env_int(name: str, default: int) -> int:
    try:
        return int(os.environ[name])
    except (KeyError, ValueError):
        return default


def env_float(name: str, default: float) -> float:
    try:
        return float(os.environ[name])
    except (KeyError, ValueError):
        return default

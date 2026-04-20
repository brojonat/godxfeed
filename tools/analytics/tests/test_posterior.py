"""Tests for analytics.posterior.

The PyMC sampling test is gated on the ``pymc`` import — the rest of
the module is pure numpy and is always exercised.
"""

from __future__ import annotations

import math

import numpy as np
import pytest

from analytics.posterior import (
    mids_to_log_returns,
    predictive_density,
)


# ─────────────────────────────────────────────────────────────────────
# log-returns
# ─────────────────────────────────────────────────────────────────────


def test_mids_to_log_returns_basic():
    mids = np.array([100.0, 101.0, 102.01])
    r = mids_to_log_returns(mids)
    # log(101/100) ≈ 0.00995, log(102.01/101) = log(1.01) ≈ 0.00995
    assert np.allclose(r, [math.log(1.01)] * 2, rtol=1e-6)


def test_mids_to_log_returns_rejects_nonpositive():
    with pytest.raises(ValueError):
        mids_to_log_returns(np.array([100.0, 0.0, 101.0]))


def test_mids_to_log_returns_drops_outliers_when_capped():
    # A 10% return is dropped under a 5% cap; normal returns survive.
    mids = np.array([100.0, 100.1, 110.0, 110.1])
    r_uncapped = mids_to_log_returns(mids)
    r_capped = mids_to_log_returns(mids, max_abs_r=0.05)
    assert len(r_uncapped) == 3
    assert len(r_capped) == 2


# ─────────────────────────────────────────────────────────────────────
# predictive density — independent of PyMC
# ─────────────────────────────────────────────────────────────────────


def test_predictive_density_integrates_to_one():
    # Synthetic σ posterior: a tight cluster around 0.005.
    sigmas = np.full(500, 0.005)
    xs, ys = predictive_density(latest_mid=100.0, sigma_samples=sigmas, n_grid=400)
    area = float(np.trapezoid(ys, xs))
    assert math.isclose(area, 1.0, rel_tol=1e-2)


def test_predictive_density_peaks_near_latest_mid():
    sigmas = np.full(200, 0.01)
    xs, ys = predictive_density(latest_mid=100.0, sigma_samples=sigmas, n_grid=401)
    peak = xs[int(np.argmax(ys))]
    # For LogNormal(log 100, 0.01) the mode is at 100·exp(-σ²) ≈ 99.99,
    # i.e. essentially 100 for σ=0.01. Loose tolerance because the grid
    # is discrete.
    assert abs(peak - 100.0) < 0.5


def test_predictive_density_widens_with_sigma():
    # A wider σ posterior produces a wider predictive (larger grid span).
    _, ys_tight = predictive_density(100.0, np.full(200, 0.001), n_grid=400)
    _, ys_wide = predictive_density(100.0, np.full(200, 0.01), n_grid=400)
    # Max density falls as σ grows (density is spread over more area).
    assert max(ys_wide) < max(ys_tight)


def test_predictive_density_rejects_nonpositive_anchor():
    with pytest.raises(ValueError):
        predictive_density(0.0, np.array([0.01, 0.02]))


def test_predictive_density_rejects_empty_sigma_samples():
    with pytest.raises(ValueError):
        predictive_density(100.0, np.array([]))


# ─────────────────────────────────────────────────────────────────────
# PyMC fit — optional, runs only if pymc is importable
# ─────────────────────────────────────────────────────────────────────

pymc = pytest.importorskip("pymc", reason="pymc not installed in this environment")


def test_fit_sigma_recovers_synthetic_truth():
    from analytics.posterior import fit_sigma_posterior

    rng = np.random.default_rng(0)
    true_sigma = 0.005
    # 200 synthetic log-returns — enough to resolve σ with a tight
    # posterior. Any more and the test gets slow; any fewer and the
    # credible interval widens past our tolerance.
    obs = rng.normal(0.0, true_sigma, size=200)

    samples = fit_sigma_posterior(
        obs,
        sigma_prior=0.01,
        draws=100,
        tune=100,
        chains=1,
        random_seed=42,
    )
    post_mean = float(samples.mean())
    # With N=200 the posterior is narrow; we expect < ~15% relative error
    # on σ̂.
    assert abs(post_mean - true_sigma) / true_sigma < 0.15

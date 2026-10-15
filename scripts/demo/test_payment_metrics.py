#!/usr/bin/env python3
"""Unit tests for payment_metrics_server allowlists (no Docker / no Prom required)."""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("payment_metrics_server", ROOT / "payment_metrics_server.py")
mod = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(mod)


def test_allowed_payment_queries() -> None:
    assert mod.allowed_query(
        'sum by (service_name) (rate(traces_span_metrics_calls_total{service_name="payment"}[2m]))'
    )
    assert mod.allowed_query(
        'sum by (service_name) (increase(traces_span_metrics_calls_total{service_name="payment",status_code="STATUS_CODE_ERROR"}[2m])) / clamp_min(sum by (service_name) (increase(traces_span_metrics_calls_total{service_name="payment"}[2m])), 1)'
    )
    assert not mod.allowed_query('up')
    assert not mod.allowed_query('sum(rate(traces_span_metrics_calls_total{service_name="checkout"}[2m]))')


def test_allowed_range_params() -> None:
    assert mod.allowed_range_params("1000", "1600", "15")
    assert mod.allowed_range_params("1000", "1600", "15s")
    assert not mod.allowed_range_params("1600", "1000", "15")  # inverted
    assert not mod.allowed_range_params("1000", str(1000 + 3600 * 7), "15")  # too long
    assert not mod.allowed_range_params("1000", "1600", "evil")


if __name__ == "__main__":
    test_allowed_payment_queries()
    test_allowed_range_params()
    print("PASS: payment_metrics_server allowlist tests")
    sys.exit(0)

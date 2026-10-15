#!/usr/bin/env python3
"""Tiny loopback payment metrics view for LIVE demo recording.

Serves scripts/demo/payment-metrics.html and proxies Prometheus instant + range
queries so the browser is same-origin (no CORS). Does not inject data or start Docker.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parent
HTML_PATH = ROOT / "payment-metrics.html"
ALLOWLIST_PREFIXES = ("sum by (service_name)", "histogram_quantile(0.99")
STEP_RE = re.compile(r"^\d+(\.\d+)?(s|m)?$")


def allowed_query(q: str) -> bool:
    q = (q or "").strip()
    if "payment" not in q:
        return False
    if "traces_span_metrics_" not in q:
        return False
    return any(q.startswith(p) for p in ALLOWLIST_PREFIXES) or (
        'traces_span_metrics_calls_total{service_name="payment"' in q
    )


def allowed_range_params(start: str, end: str, step: str) -> bool:
    try:
        s = float(start)
        e = float(end)
    except (TypeError, ValueError):
        return False
    if e < s:
        return False
    if e - s > 3600 * 6:  # cap lookback for the demo proxy
        return False
    if not STEP_RE.match((step or "").strip()):
        return False
    return True


class Handler(BaseHTTPRequestHandler):
    prom_base = "http://127.0.0.1:9090"

    def log_message(self, fmt: str, *args) -> None:  # quieter
        sys_stderr = __import__("sys").stderr
        sys_stderr.write("[payment-metrics] " + (fmt % args) + "\n")

    def do_GET(self) -> None:  # noqa: N802
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path in ("/", "/index.html"):
            body = HTML_PATH.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)
            return
        if parsed.path == "/api/v1/query":
            qs = urllib.parse.parse_qs(parsed.query)
            query = (qs.get("query") or [""])[0]
            if not allowed_query(query):
                self._json(403, {"status": "error", "error": "query not allowlisted for demo view"})
                return
            self._proxy("/api/v1/query", {"query": query})
            return
        if parsed.path == "/api/v1/query_range":
            qs = urllib.parse.parse_qs(parsed.query)
            query = (qs.get("query") or [""])[0]
            start = (qs.get("start") or [""])[0]
            end = (qs.get("end") or [""])[0]
            step = (qs.get("step") or [""])[0]
            if not allowed_query(query):
                self._json(403, {"status": "error", "error": "query not allowlisted for demo view"})
                return
            if not allowed_range_params(start, end, step):
                self._json(400, {"status": "error", "error": "invalid start/end/step"})
                return
            self._proxy("/api/v1/query_range", {"query": query, "start": start, "end": end, "step": step})
            return
        if parsed.path == "/healthz":
            self._json(200, {"ok": True, "prom": self.prom_base})
            return
        self.send_error(404)

    def _proxy(self, path: str, params: dict) -> None:
        url = self.prom_base.rstrip("/") + path + "?" + urllib.parse.urlencode(params)
        try:
            with urllib.request.urlopen(url, timeout=10) as resp:  # noqa: S310 — loopback Prom only
                data = resp.read()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        except urllib.error.URLError as exc:
            self._json(502, {"status": "error", "error": f"prometheus unreachable: {exc}"})

    def _json(self, code: int, obj: dict) -> None:
        data = json.dumps(obj).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen", default="127.0.0.1:3132")
    parser.add_argument("--prom", default=os.environ.get("PERCH_LIVE_PULSE_PROM_URL", "http://127.0.0.1:9090"))
    args = parser.parse_args()
    host, _, port_s = args.listen.partition(":")
    port = int(port_s or "3132")
    Handler.prom_base = args.prom
    if not HTML_PATH.is_file():
        raise SystemExit(f"missing {HTML_PATH}")
    httpd = ThreadingHTTPServer((host, port), Handler)
    print(f"[payment-metrics] listening on http://{host}:{port}/  prom={args.prom}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()

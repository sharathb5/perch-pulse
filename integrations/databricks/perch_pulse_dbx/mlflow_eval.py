"""MLflow evaluation logging from F2b/F2c reports / F3a eval_runs rows."""

from __future__ import annotations

import json
import os
import re
import tempfile
from contextlib import contextmanager
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterator, Protocol

SENSITIVE_KEY_RE = re.compile(
    r"(api[_-]?key|authorization|bearer|password|secret|token|credential)",
    re.IGNORECASE,
)

# Token-like substrings that must never appear in operator-facing errors.
SENSITIVE_VALUE_RE = re.compile(
    r"(?i)(sk-[a-z0-9]{8,}|bearer\s+[a-z0-9\-._~+/]+=*|dapi[a-f0-9]{20,})"
)

# Live MLflow HTTP/filesystem ops stay bounded — no automatic billable retries.
LIVE_MLFLOW_TIMEOUT_SEC = 30


class MLflowClient(Protocol):
    def log_run(self, payload: dict[str, Any]) -> str:
        ...


@dataclass
class FakeMLflowClient:
    runs: list[dict[str, Any]] = field(default_factory=list)

    def log_run(self, payload: dict[str, Any]) -> str:
        # Idempotent by eval_run_id + method tag when present.
        run_key = (
            str(payload.get("params", {}).get("eval_run_id", "")),
            str(payload.get("tags", {}).get("method", "")),
            str(payload.get("params", {}).get("dataset_id", "")),
        )
        for existing in self.runs:
            ek = (
                str(existing.get("params", {}).get("eval_run_id", "")),
                str(existing.get("tags", {}).get("method", "")),
                str(existing.get("params", {}).get("dataset_id", "")),
            )
            if ek == run_key and run_key != ("", "", ""):
                existing.clear()
                existing.update(payload)
                return str(existing.get("run_id", "run-updated"))
        run_id = f"fake-run-{len(self.runs) + 1}"
        stored = dict(payload)
        stored["run_id"] = run_id
        self.runs.append(stored)
        return run_id


@dataclass
class EvalLogResult:
    ok: bool
    run_id: str | None
    metrics: dict[str, float] = field(default_factory=dict)
    params: dict[str, str] = field(default_factory=dict)
    tags: dict[str, str] = field(default_factory=dict)
    problems: list[str] = field(default_factory=list)
    artifact: dict[str, Any] | None = None
    live_executed: bool = False
    error_category: str | None = None

    def to_dict(self) -> dict[str, Any]:
        return {
            "ok": self.ok,
            "run_id": self.run_id,
            "metrics": dict(self.metrics),
            "params": dict(self.params),
            "tags": dict(self.tags),
            "problems": list(self.problems),
            "artifact": self.artifact,
            "live_executed": self.live_executed,
            "error_category": self.error_category,
        }


def redact_mapping(data: dict[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for k, v in data.items():
        if SENSITIVE_KEY_RE.search(str(k)):
            continue
        if isinstance(v, dict):
            out[k] = redact_mapping(v)
        elif isinstance(v, list):
            out[k] = [redact_mapping(x) if isinstance(x, dict) else x for x in v[:50]]
        elif isinstance(v, str) and SENSITIVE_KEY_RE.search(v):
            out[k] = "[redacted]"
        else:
            out[k] = v
    return out


def sanitize_error_message(exc: BaseException | str) -> str:
    """Preserve error class/category text without leaking credential material."""
    text = str(exc)
    text = SENSITIVE_VALUE_RE.sub("[redacted]", text)
    # Scrub query-string style secrets and common env dumps.
    text = re.sub(
        r"(?i)(api[_-]?key|token|password|secret|authorization)\s*[:=]\s*\S+",
        r"\1=[redacted]",
        text,
    )
    if len(text) > 400:
        text = text[:400] + "…"
    return text


def classify_mlflow_error(exc: BaseException) -> str:
    """Map exceptions to stable categories without inventing success."""
    name = type(exc).__name__
    msg = str(exc).lower()
    if isinstance(exc, ImportError) or "not installed" in msg:
        return "mlflow_unavailable"
    if isinstance(exc, TimeoutError) or "timed out" in msg or "timeout" in msg:
        return "timeout"
    if any(s in msg for s in ("401", "403", "unauthorized", "forbidden", "permission", "auth")):
        return "auth_rejected"
    if any(s in msg for s in ("connection", "connect", "dns", "network", "refused", "unreachable")):
        return "connection_failed"
    if any(s in msg for s in ("invalid", "rejected", "bad request", "400", "422")):
        return "request_rejected"
    if name:
        return f"provider_error:{name}"
    return "provider_error"


def metrics_from_eval_report(report: dict[str, Any]) -> tuple[dict[str, float], list[str]]:
    """Extract metrics with explicit denominators. Never invent zeros for missing values."""
    problems: list[str] = []
    agg = report.get("aggregate")
    if not isinstance(agg, dict):
        return {}, ["aggregate missing — no metrics logged"]

    metrics: dict[str, float] = {}

    def take(name: str, key: str) -> None:
        if key not in agg:
            problems.append(f"metric {name} unavailable (missing {key})")
            return
        val = agg[key]
        if val is None:
            problems.append(f"metric {name} unavailable (null)")
            return
        try:
            metrics[name] = float(val)
        except (TypeError, ValueError):
            problems.append(f"metric {name} unavailable (non-numeric)")

    take("case_count", "case_count")
    take("deterministic_pass_count", "deterministic_pass_count")
    take("deterministic_denominator", "deterministic_denominator")
    take("llm_available_count", "llm_available_count")
    take("llm_available_denominator", "llm_available_denominator")
    take("llm_pass_count", "llm_pass_count")
    take("llm_pass_denominator", "llm_pass_denominator")
    take("synthetic_proposal_count", "synthetic_proposal_count")
    take("missing_llm_proposal_count", "missing_llm_proposal_count")
    take("false_causal_llm_total", "false_causal_llm_total")
    take("unresolved_citations_llm_total", "unresolved_citations_llm_total")
    # Optional token/cost — only when present (never fabricate).
    if "prompt_tokens_total" in agg and agg["prompt_tokens_total"] is not None:
        take("prompt_tokens_total", "prompt_tokens_total")
    else:
        problems.append("prompt_tokens_total unavailable")
    if "estimated_cost_usd_total" in agg and agg["estimated_cost_usd_total"] is not None:
        take("estimated_cost_usd_total", "estimated_cost_usd_total")
    else:
        problems.append("estimated_cost_usd_total unavailable")
    return metrics, problems


def build_eval_payload(report: dict[str, Any]) -> dict[str, Any]:
    metrics, metric_problems = metrics_from_eval_report(report)
    params = {
        "eval_run_id": str(report.get("eval_run_id") or report.get("generated_at") or ""),
        "dataset_id": str(report.get("dataset_id") or ""),
        "schema_version": str(report.get("schema_version") or ""),
        "mode": str(report.get("mode") or "offline"),
        "rubric_version": str(report.get("rubric_version") or ""),
    }
    tags = {
        "pulse_component": "investigation_eval",
        "method": "deterministic_vs_llm",
        "subjective_reviews": "present"
        if (report.get("human_review_summary") or {}).get("count", 0)
        else "not_evaluated",
    }
    # Compact redacted artifact — no full prompts / unrestricted payloads.
    artifact = redact_mapping(
        {
            "dataset_id": report.get("dataset_id"),
            "mode": report.get("mode"),
            "aggregate": report.get("aggregate"),
            "limitations": report.get("limitations"),
            "human_review_summary": report.get("human_review_summary"),
            "case_count": len(report.get("cases") or []),
            "evidence_fingerprints": [
                c.get("evidence_fingerprint")
                for c in (report.get("cases") or [])[:50]
                if isinstance(c, dict)
            ],
        }
    )
    return {
        "metrics": metrics,
        "params": params,
        "tags": tags,
        "artifact": artifact,
        "metric_problems": metric_problems,
    }


def log_eval_report(
    report: dict[str, Any] | str | Path,
    *,
    client: MLflowClient | None = None,
    experiment: str = "perch-pulse-investigation-eval",
    live: bool = False,
) -> EvalLogResult:
    if isinstance(report, (str, Path)):
        try:
            report = json.loads(Path(report).read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            return EvalLogResult(
                ok=False,
                run_id=None,
                problems=[f"report unreadable: {sanitize_error_message(exc)}"],
                error_category="report_unreadable",
                live_executed=False,
            )
    if not isinstance(report, dict):
        return EvalLogResult(
            ok=False,
            run_id=None,
            problems=["report must be an object"],
            error_category="invalid_report",
            live_executed=False,
        )

    payload = build_eval_payload(report)
    result = EvalLogResult(
        ok=False,
        run_id=None,
        metrics=payload["metrics"],
        params=payload["params"],
        tags=payload["tags"],
        artifact=payload["artifact"],
        problems=list(payload["metric_problems"]),
        live_executed=False,
    )

    if client is None:
        if live:
            try:
                client = LiveMLflowClient(experiment=experiment)
            except Exception as exc:  # noqa: BLE001 — structured failure, no retry
                cat = classify_mlflow_error(exc)
                result.error_category = cat
                result.problems.append(f"mlflow unavailable ({cat}): {sanitize_error_message(exc)}")
                return result
        else:
            client = FakeMLflowClient()

    try:
        run_id = client.log_run(
            {
                "experiment": experiment,
                "metrics": result.metrics,
                "params": result.params,
                "tags": result.tags,
                "artifact": result.artifact,
            }
        )
    except Exception as exc:  # noqa: BLE001 — never claim success on partial/failed log
        cat = classify_mlflow_error(exc)
        result.error_category = cat
        result.problems.append(f"mlflow log_run failed ({cat}): {sanitize_error_message(exc)}")
        result.ok = False
        result.run_id = None
        result.live_executed = False
        return result

    if not run_id:
        result.error_category = "empty_run_id"
        result.problems.append("mlflow log_run returned empty run_id")
        result.ok = False
        result.live_executed = False
        return result

    result.run_id = str(run_id)
    result.ok = True
    result.live_executed = live and not isinstance(client, FakeMLflowClient)
    return result


@contextmanager
def _bounded_mlflow_http(timeout_sec: int) -> Iterator[None]:
    """Apply MLFLOW_HTTP_REQUEST_TIMEOUT for the duration of live calls only."""
    key = "MLFLOW_HTTP_REQUEST_TIMEOUT"
    prev = os.environ.get(key)
    os.environ[key] = str(max(1, int(timeout_sec)))
    try:
        yield
    finally:
        if prev is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = prev


class LiveMLflowClient:
    """Thin live wrapper. Failures surface as exceptions for log_eval_report to classify.

    No automatic billable retries. Operators must re-invoke explicitly.
    HTTP is bounded via MLFLOW_HTTP_REQUEST_TIMEOUT for the call scope.
    """

    def __init__(self, *, experiment: str, timeout_sec: int = LIVE_MLFLOW_TIMEOUT_SEC) -> None:
        try:
            import mlflow  # type: ignore
        except ImportError as exc:
            raise ImportError("mlflow is not installed; pip install 'perch-pulse-dbx[mlflow]'") from exc
        self._mlflow = mlflow
        self._experiment = experiment
        self._timeout_sec = max(1, int(timeout_sec))
        with _bounded_mlflow_http(self._timeout_sec):
            mlflow.set_experiment(experiment)

    def log_run(self, payload: dict[str, Any]) -> str:
        mlflow = self._mlflow
        # Versioned run name from eval_run_id for operator visibility (not exactly-once).
        run_name = f"eval-{payload.get('params', {}).get('eval_run_id', 'unknown')}"[:200]
        with _bounded_mlflow_http(self._timeout_sec):
            with mlflow.start_run(run_name=run_name) as run:
                for k, v in (payload.get("params") or {}).items():
                    if v is not None and v != "":
                        mlflow.log_param(k, v)
                for k, v in (payload.get("metrics") or {}).items():
                    mlflow.log_metric(k, float(v))
                for k, v in (payload.get("tags") or {}).items():
                    mlflow.set_tag(k, v)
                artifact = payload.get("artifact") or {}
                # Artifact is already redacted in build_eval_payload.
                with tempfile.TemporaryDirectory() as td:
                    path = Path(td) / "eval_summary.json"
                    path.write_text(json.dumps(artifact, indent=2), encoding="utf-8")
                    mlflow.log_artifact(str(path), artifact_path="eval")
                return run.info.run_id

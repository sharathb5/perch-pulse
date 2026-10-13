"""MLflow evaluation logging from F2b/F2c reports / F3a eval_runs rows."""

from __future__ import annotations

import json
import re
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Protocol

SENSITIVE_KEY_RE = re.compile(
    r"(api[_-]?key|authorization|bearer|password|secret|token|credential)",
    re.IGNORECASE,
)


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
        report = json.loads(Path(report).read_text(encoding="utf-8"))
    if not isinstance(report, dict):
        return EvalLogResult(ok=False, run_id=None, problems=["report must be an object"])

    payload = build_eval_payload(report)
    result = EvalLogResult(
        ok=False,
        run_id=None,
        metrics=payload["metrics"],
        params=payload["params"],
        tags=payload["tags"],
        artifact=payload["artifact"],
        problems=list(payload["metric_problems"]),
    )

    if client is None:
        if live:
            try:
                client = LiveMLflowClient(experiment=experiment)
            except Exception as exc:  # noqa: BLE001
                result.problems.append(f"mlflow unavailable: {exc}")
                return result
        else:
            client = FakeMLflowClient()

    run_id = client.log_run(
        {
            "experiment": experiment,
            "metrics": result.metrics,
            "params": result.params,
            "tags": result.tags,
            "artifact": result.artifact,
        }
    )
    result.run_id = run_id
    result.ok = True
    result.live_executed = live and not isinstance(client, FakeMLflowClient)
    return result


class LiveMLflowClient:
    def __init__(self, *, experiment: str) -> None:
        try:
            import mlflow  # type: ignore
        except ImportError as exc:
            raise ImportError("mlflow is not installed; pip install 'perch-pulse-dbx[mlflow]'") from exc
        self._mlflow = mlflow
        self._experiment = experiment
        mlflow.set_experiment(experiment)

    def log_run(self, payload: dict[str, Any]) -> str:
        mlflow = self._mlflow
        # Versioned run name from eval_run_id for operator visibility (not exactly-once).
        run_name = f"eval-{payload.get('params', {}).get('eval_run_id', 'unknown')}"[:200]
        with mlflow.start_run(run_name=run_name) as run:
            for k, v in (payload.get("params") or {}).items():
                if v is not None and v != "":
                    mlflow.log_param(k, v)
            for k, v in (payload.get("metrics") or {}).items():
                mlflow.log_metric(k, float(v))
            for k, v in (payload.get("tags") or {}).items():
                mlflow.set_tag(k, v)
            artifact = payload.get("artifact") or {}
            with tempfile.TemporaryDirectory() as td:
                path = Path(td) / "eval_summary.json"
                path.write_text(json.dumps(artifact, indent=2), encoding="utf-8")
                mlflow.log_artifact(str(path), artifact_path="eval")
            return run.info.run_id

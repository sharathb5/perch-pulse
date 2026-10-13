import json
from pathlib import Path

from perch_pulse_dbx.mlflow_eval import (
    FakeMLflowClient,
    classify_mlflow_error,
    log_eval_report,
    redact_mapping,
    sanitize_error_message,
)

REPO = Path(__file__).resolve().parents[3]
SAMPLE_REPORT = REPO / "examples" / "pulse-export" / "sample" / "eval-report.json"


def test_redaction_strips_secrets():
    raw = {"ok": True, "api_key": "sk-test", "nested": {"authorization": "Bearer x"}}
    out = redact_mapping(raw)
    assert "api_key" not in out
    assert "authorization" not in out["nested"]


def test_log_eval_report_fake_and_idempotent():
    client = FakeMLflowClient()
    report = json.loads(SAMPLE_REPORT.read_text(encoding="utf-8"))
    # Ensure denominators exist; sample report should.
    r1 = log_eval_report(report, client=client, live=False)
    assert r1.ok and r1.run_id
    assert "deterministic_denominator" in r1.metrics
    # Missing token metrics must be noted, not invented as zero.
    assert any("prompt_tokens_total unavailable" in p for p in r1.problems)
    assert "prompt_tokens_total" not in r1.metrics

    r2 = log_eval_report(report, client=client, live=False)
    assert r2.ok
    assert len(client.runs) == 1  # idempotent update by eval_run_id/dataset


def test_missing_aggregate_does_not_fabricate():
    client = FakeMLflowClient()
    res = log_eval_report({"schema_version": "x", "dataset_id": "d"}, client=client)
    assert res.ok  # logging still succeeds
    assert res.metrics == {}
    assert any("aggregate missing" in p for p in res.problems)


class _BoomClient:
    def __init__(self, exc: BaseException):
        self.exc = exc

    def log_run(self, payload):
        raise self.exc


def test_live_log_failure_is_structured_not_success():
    report = json.loads(SAMPLE_REPORT.read_text(encoding="utf-8"))
    res = log_eval_report(
        report,
        client=_BoomClient(TimeoutError("mlflow tracking timed out after 30s")),
        live=True,
    )
    assert not res.ok
    assert res.live_executed is False
    assert res.run_id is None
    assert res.error_category == "timeout"
    assert any("timed out" in p.lower() or "timeout" in p.lower() for p in res.problems)


def test_auth_failure_category_and_secret_redaction():
    report = {"aggregate": {"case_count": 1, "deterministic_pass_count": 1, "deterministic_denominator": 1}}
    secret = "sk-abcdefghijklmnopqrstuvwxyz012345"
    res = log_eval_report(
        report,
        client=_BoomClient(PermissionError(f"Unauthorized token={secret}")),
        live=True,
    )
    assert not res.ok
    assert res.live_executed is False
    assert res.error_category == "auth_rejected"
    blob = " ".join(res.problems)
    assert secret not in blob
    assert "[redacted]" in blob or "auth" in (res.error_category or "")


def test_sanitize_and_classify_helpers():
    assert "sk-" not in sanitize_error_message("Authorization: Bearer sk-abcdefghijklmnop")
    assert classify_mlflow_error(ConnectionError("connection refused")) == "connection_failed"
    assert classify_mlflow_error(ImportError("mlflow")) == "mlflow_unavailable"


def test_empty_run_id_not_success():
    class EmptyID:
        def log_run(self, payload):
            return ""

    res = log_eval_report({"aggregate": {}}, client=EmptyID(), live=False)
    assert not res.ok
    assert res.error_category == "empty_run_id"

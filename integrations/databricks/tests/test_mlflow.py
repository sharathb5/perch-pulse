import json
from pathlib import Path

from perch_pulse_dbx.mlflow_eval import FakeMLflowClient, log_eval_report, redact_mapping

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

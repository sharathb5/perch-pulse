import json
import re
from pathlib import Path

import pytest

from perch_pulse_dbx.load import FakeSQLExecutor, LoadOptions, load_export
from perch_pulse_dbx.merge_plan import plan_merges
from perch_pulse_dbx.validate import validate_export_dir

REPO = Path(__file__).resolve().parents[3]
SAMPLE = REPO / "examples" / "pulse-export" / "sample" / "export"


def test_validate_sample_export():
    res = validate_export_dir(SAMPLE)
    assert res.ok, [i.message for i in res.errors()]
    assert res.export_schema_version == "pulse.export.analytics.v1"
    assert res.counts["incidents"] == 1
    assert res.counts["eval_case_results"] == 15


def test_reject_unsupported_schema(tmp_path: Path):
    (tmp_path / "checkpoint.json").write_text(
        json.dumps(
            {
                "schema_version": "pulse.export.checkpoint.v1",
                "export_schema_version": "pulse.export.analytics.v999",
                "datasets": {},
            }
        ),
        encoding="utf-8",
    )
    (tmp_path / "export-manifest.json").write_text(
        json.dumps(
            {
                "schema_version": "pulse.export.manifest.v1",
                "export_schema_version": "pulse.export.analytics.v999",
                "datasets": [],
            }
        ),
        encoding="utf-8",
    )
    res = validate_export_dir(tmp_path)
    assert not res.ok
    assert any(i.code == "unsupported_export_schema" for i in res.errors())


def test_missing_checkpoint(tmp_path: Path):
    (tmp_path / "export-manifest.json").write_text("{}", encoding="utf-8")
    res = validate_export_dir(tmp_path)
    assert not res.ok
    assert any(i.code == "missing_checkpoint" for i in res.errors())


def test_malformed_jsonl(tmp_path: Path):
    (tmp_path / "checkpoint.json").write_text(
        json.dumps(
            {
                "schema_version": "pulse.export.checkpoint.v1",
                "export_schema_version": "pulse.export.analytics.v1",
                "datasets": {},
            }
        ),
        encoding="utf-8",
    )
    (tmp_path / "export-manifest.json").write_text(
        json.dumps(
            {
                "schema_version": "pulse.export.manifest.v1",
                "export_schema_version": "pulse.export.analytics.v1",
                "datasets": [],
            }
        ),
        encoding="utf-8",
    )
    (tmp_path / "incidents.jsonl").write_text("{not-json\n", encoding="utf-8")
    res = validate_export_dir(tmp_path)
    assert not res.ok
    assert any(i.code == "malformed_jsonl" for i in res.errors())


def test_dry_run_plan_and_idempotent_fake_load():
    validation = validate_export_dir(SAMPLE)
    plan = plan_merges(validation, catalog="perch_pulse", base_schema="analytics")
    assert plan.operations
    assert all(
        "THEN DELETE" not in op.sql_preview.upper() and "\nDELETE " not in op.sql_preview.upper()
        for op in plan.operations
    )
    domains = {op.domain for op in plan.operations}
    assert "synthetic" in domains or "observed" in domains or "evaluation" in domains

    fake = FakeSQLExecutor()
    first = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="dry_run",
            executor=fake,
            apply_fake_merges=True,
        )
    )
    assert first.ok
    counts1 = dict(first.row_counts_after)

    second = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="dry_run",
            executor=fake,
            apply_fake_merges=True,
            prior_fingerprints={
                ds: {r["dedupe_key"]: r["content_fingerprint"] for r in validation.datasets[ds]}
                for ds in validation.datasets
            },
        )
    )
    assert second.ok
    # Row counts unchanged on identical fingerprints.
    for table, n in counts1.items():
        assert len(fake.row_store.get(table, {})) == n


def test_changed_fingerprint_appends_history():
    fake = FakeSQLExecutor()
    validation = validate_export_dir(SAMPLE)
    load_export(
        LoadOptions(export_dir=SAMPLE, mode="dry_run", executor=fake, apply_fake_merges=True)
    )
    table = next(iter(fake.row_store))
    key = next(iter(fake.row_store[table]))
    old = fake.row_store[table][key]
    before = len(fake.row_store[table])
    new_row = dict(old)
    new_row["content_fingerprint"] = "ff" * 32
    inserted, skipped = fake.apply_merge_rows(table, [new_row])
    assert inserted == 1 and skipped == 0
    assert len(fake.row_store[table]) == before + 1


def test_merge_sql_is_append_only():
    validation = validate_export_dir(SAMPLE)
    plan = plan_merges(validation, catalog="perch_pulse", base_schema="analytics")
    for op in plan.operations:
        assert op.merge_keys == ["dedupe_key", "content_fingerprint"]
        assert "WHEN NOT MATCHED THEN INSERT" in op.sql_preview
        assert "THEN UPDATE" not in op.sql_preview.upper()


def test_live_requires_authorization():
    res = load_export(LoadOptions(export_dir=SAMPLE, mode="live", authorize_live=False))
    assert not res.ok
    assert not res.live_executed
    assert any("authorize" in p for p in res.problems)


def test_unsafe_catalog_rejected():
    res = load_export(LoadOptions(export_dir=SAMPLE, catalog="bad-name", mode="dry_run"))
    assert not res.ok


def test_staged_merge_and_idempotent_rerun():
    fake = FakeSQLExecutor()
    first = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="dry_run",
            executor=fake,
            execute_merge=True,
            max_batch=20,
        )
    )
    assert first.ok, first.problems
    assert first.rows_merged
    assert first.lineage and first.lineage.get("ok")
    assert first.row_counts_after
    counts1 = dict(first.row_counts_after)

    second = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="dry_run",
            executor=fake,
            execute_merge=True,
            max_batch=20,
        )
    )
    assert second.ok, second.problems
    assert second.row_counts_after == counts1
    # Staging land + MERGE statements were recorded.
    assert any("MERGE INTO" in s for s in fake.statements)
    assert any(s.startswith("-- insert_rows") for s in fake.statements)


def test_connection_only_does_not_claim_merge():
    fake = FakeSQLExecutor()
    res = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="live_connection",
            authorize_live=True,
            executor=fake,
            execute_merge=False,
        )
    )
    assert res.ok
    assert res.live_connection_ok
    assert not res.rows_merged
    assert any("MERGE not executed" in n for n in res.notes)


def test_live_execute_merge_with_fake():
    fake = FakeSQLExecutor()
    res = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="live",
            authorize_live=True,
            executor=fake,
            execute_merge=True,
            max_batch=50,
        )
    )
    assert res.ok, res.problems
    assert res.live_connection_ok
    assert res.rows_merged
    assert res.lineage and res.lineage.get("append_only") is True


def test_insert_batch_chunks_without_export_reject():
    """max_batch is insert chunk size; export may be larger when max_export_rows allows."""
    fake = FakeSQLExecutor()
    res = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="dry_run",
            executor=fake,
            execute_merge=True,
            max_batch=3,
            max_export_rows=500,
        )
    )
    assert res.ok, res.problems
    assert res.rows_merged
    insert_stmts = [s for s in fake.statements if s.startswith("-- insert_rows")]
    assert len(insert_stmts) >= 2  # eval_case_results (15) needs multiple chunks of 3


def test_staging_tables_use_unique_suffix():
    fake = FakeSQLExecutor()
    res = load_export(
        LoadOptions(
            export_dir=SAMPLE,
            mode="dry_run",
            executor=fake,
            execute_merge=True,
            max_batch=50,
        )
    )
    assert res.ok
    staging = {
        op["staging_table"]
        for op in (res.stage or {}).get("operations", [])
    }
    assert staging
    assert all(re.search(r"\.stg_.+_[a-f0-9]{8}$", t) for t in staging)

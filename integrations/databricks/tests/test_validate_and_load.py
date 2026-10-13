import json
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


def test_changed_fingerprint_updates():
    fake = FakeSQLExecutor()
    validation = validate_export_dir(SAMPLE)
    load_export(
        LoadOptions(export_dir=SAMPLE, mode="dry_run", executor=fake, apply_fake_merges=True)
    )
    # Mutate one fingerprint in memory store then reload same key with new fp via apply.
    table = next(iter(fake.row_store))
    key = next(iter(fake.row_store[table]))
    old = fake.row_store[table][key]
    new_row = dict(old)
    new_row["content_fingerprint"] = "ff" * 32
    inserted, updated = fake.apply_merge_rows(table, [new_row])
    assert inserted == 0 and updated == 1


def test_live_requires_authorization():
    res = load_export(LoadOptions(export_dir=SAMPLE, mode="live", authorize_live=False))
    assert not res.ok
    assert not res.live_executed
    assert any("authorize" in p for p in res.problems)


def test_unsafe_catalog_rejected():
    res = load_export(LoadOptions(export_dir=SAMPLE, catalog="bad-name", mode="dry_run"))
    assert not res.ok

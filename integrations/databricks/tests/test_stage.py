from perch_pulse_dbx.stage import (
    build_insert_sql,
    row_to_columns,
    sql_string_literal,
    verify_lineage_from_result,
    StageMergeResult,
    OpExecution,
)


def test_sql_string_literal_escapes_quotes():
    assert sql_string_literal("a'b\\c") == "'a''b\\\\c'"


def test_row_to_columns_puts_extras_in_payload():
    row = {
        "dedupe_key": "k1",
        "content_fingerprint": "fp1",
        "export_schema_version": "pulse.export.analytics.v1",
        "provenance_class": "synthetic_fixture",
        "exported_at": "2026-10-12T00:00:00Z",
        "observation_at": "2026-10-12T00:00:00Z",
        "incident_id": "inc|1",
        "summary_present": True,
    }
    cols = row_to_columns(row)
    assert cols["dedupe_key"] == "k1"
    assert "incident_id" not in cols or cols.get("incident_id") is None
    assert '"incident_id":"inc|1"' in cols["payload"]


def test_build_insert_sql_escapes_embedded_quotes():
    rows = [
        {
            "dedupe_key": "k'or'1",
            "content_fingerprint": "fp",
            "export_schema_version": "pulse.export.analytics.v1",
            "provenance_class": "synthetic_fixture",
            "exported_at": "t",
            "observation_at": None,
        }
    ]
    sql = build_insert_sql("perch_pulse.analytics_synthetic.stg_pulse_incidents", rows)
    assert "INSERT INTO perch_pulse.analytics_synthetic.stg_pulse_incidents" in sql
    assert "'k''or''1'" in sql


def test_verify_lineage_requires_merge():
    empty = StageMergeResult(ok=False, rows_merged=False)
    assert verify_lineage_from_result(empty)["ok"] is False
    done = StageMergeResult(
        ok=True,
        rows_merged=True,
        lineage={"tables": {"t": {"count_after": 1}}},
        operations=[
            OpExecution(
                dataset="incidents",
                domain="synthetic",
                target_table="t",
                staging_table="s",
                merge_executed=True,
                count_after=1,
            )
        ],
    )
    lin = verify_lineage_from_result(done)
    assert lin["ok"] is True
    assert lin["append_only"] is True

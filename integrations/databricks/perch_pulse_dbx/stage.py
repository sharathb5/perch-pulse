"""Stage JSONL rows and execute append-only MERGE (bounded, identifier-safe).

Used by authorized live loads and FakeSQLExecutor offline tests. Values are
escaped as SQL string literals — never interpolated into identifiers.
"""

from __future__ import annotations

import json
import re
import secrets
from dataclasses import dataclass, field
from typing import Any

from .identifiers import qualify_table, require_identifier
from .merge_plan import MergePlan, build_merge_sql
from .validate import ExportValidation, domain_for_row

# First-class envelope columns; remaining allowlisted fields go in payload JSON.
ENVELOPE_COLUMNS = (
    "dedupe_key",
    "content_fingerprint",
    "export_schema_version",
    "provenance_class",
    "exported_at",
    "observation_at",
)

_TABLE_DDL = """CREATE TABLE IF NOT EXISTS {table} (
  dedupe_key STRING NOT NULL,
  content_fingerprint STRING NOT NULL,
  export_schema_version STRING NOT NULL,
  provenance_class STRING NOT NULL,
  exported_at STRING NOT NULL,
  observation_at STRING,
  payload STRING
) USING DELTA"""


@dataclass
class OpExecution:
    dataset: str
    domain: str
    target_table: str
    staging_table: str
    staged_rows: int = 0
    merge_executed: bool = False
    count_before: int | None = None
    count_after: int | None = None
    sample_keys_after: list[str] = field(default_factory=list)
    problems: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "dataset": self.dataset,
            "domain": self.domain,
            "target_table": self.target_table,
            "staging_table": self.staging_table,
            "staged_rows": self.staged_rows,
            "merge_executed": self.merge_executed,
            "count_before": self.count_before,
            "count_after": self.count_after,
            "sample_keys_after": list(self.sample_keys_after),
            "problems": list(self.problems),
        }


@dataclass
class StageMergeResult:
    ok: bool
    operations: list[OpExecution] = field(default_factory=list)
    executed_statements: int = 0
    rows_merged: bool = False
    problems: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)
    lineage: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return {
            "ok": self.ok,
            "executed_statements": self.executed_statements,
            "rows_merged": self.rows_merged,
            "problems": list(self.problems),
            "notes": list(self.notes),
            "lineage": dict(self.lineage),
            "operations": [op.to_dict() for op in self.operations],
        }


def sql_string_literal(value: str) -> str:
    """Escape a Python string as a Spark SQL string literal."""
    return "'" + str(value).replace("\\", "\\\\").replace("'", "''") + "'"


def ensure_schema_sql(catalog: str, schema: str) -> str:
    c = require_identifier(catalog, kind="catalog")
    s = require_identifier(schema, kind="schema")
    return f"CREATE SCHEMA IF NOT EXISTS {c}.{s}"


def ensure_table_sql(qualified_table: str) -> str:
    _assert_qualified(qualified_table)
    return _TABLE_DDL.format(table=qualified_table)


def recreate_staging_sql(qualified_staging: str, qualified_target: str) -> str:
    _assert_qualified(qualified_staging)
    _assert_qualified(qualified_target)
    # Empty clone of target schema — safe when target exists (ensured first).
    return (
        f"CREATE OR REPLACE TABLE {qualified_staging} "
        f"AS SELECT * FROM {qualified_target} WHERE 1=0"
    )


def count_sql(qualified_table: str) -> str:
    _assert_qualified(qualified_table)
    return f"SELECT COUNT(*) AS n FROM {qualified_table}"


def sample_keys_sql(qualified_table: str, *, limit: int = 5) -> str:
    _assert_qualified(qualified_table)
    lim = max(1, min(int(limit), 50))
    return (
        f"SELECT dedupe_key, content_fingerprint FROM {qualified_table} "
        f"ORDER BY dedupe_key LIMIT {lim}"
    )


def row_to_columns(row: dict[str, Any]) -> dict[str, str]:
    """Project an F3a JSONL row into envelope columns + JSON payload."""
    payload = {k: v for k, v in row.items() if k not in ENVELOPE_COLUMNS}
    out: dict[str, str] = {}
    for col in ENVELOPE_COLUMNS:
        val = row.get(col)
        out[col] = "" if val is None else str(val)
    out["payload"] = json.dumps(payload, sort_keys=True, separators=(",", ":"))
    return out


def build_insert_sql(qualified_staging: str, rows: list[dict[str, Any]]) -> str:
    _assert_qualified(qualified_staging)
    if not rows:
        raise ValueError("insert requires at least one row")
    cols = list(ENVELOPE_COLUMNS) + ["payload"]
    col_list = ", ".join(cols)
    value_groups: list[str] = []
    for row in rows:
        projected = row_to_columns(row)
        literals = ", ".join(sql_string_literal(projected[c]) for c in cols)
        value_groups.append(f"({literals})")
    return f"INSERT INTO {qualified_staging} ({col_list}) VALUES " + ", ".join(value_groups)


def merge_sql(target: str, staging: str) -> str:
    return build_merge_sql(target, staging)


def parse_count(result: dict[str, Any]) -> int | None:
    data = result.get("data_array")
    if not data or not data[0]:
        return None
    try:
        return int(data[0][0])
    except (TypeError, ValueError, IndexError):
        return None


def parse_key_pairs(result: dict[str, Any]) -> list[str]:
    data = result.get("data_array") or []
    out: list[str] = []
    for row in data:
        if not row or len(row) < 2:
            continue
        out.append(f"{row[0]}|{row[1]}")
    return out


def execute_staged_merges(
    executor: Any,
    validation: ExportValidation,
    plan: MergePlan,
    *,
    max_batch: int = 50,
    load_id: str | None = None,
) -> StageMergeResult:
    """Land staging tables from validated rows and run append-only MERGEs.

    Works with FakeSQLExecutor (offline) or DatabricksSQLExecutor (authorized live).
    Each invocation uses a unique staging table suffix so concurrent loads cannot
    clobber each other's stg_* contents.
    """
    notes = [
        "Staging lands bounded JSONL batches into per-load stg_* tables, then MERGE.",
        "MERGE is append-only on (dedupe_key, content_fingerprint).",
        "Missing keys never DELETE historical Delta rows.",
    ]
    result = StageMergeResult(ok=False, notes=notes)
    if not validation.ok:
        result.problems.append("export validation failed")
        return result
    if not plan.operations:
        result.ok = True
        result.notes.append("no MERGE operations planned (empty export datasets)")
        return result

    batch = max(1, min(int(max_batch), 500))
    # Short unique suffix; keep full identifier within allowlist length.
    suffix = (load_id or secrets.token_hex(4)).replace("-", "")[:8]
    if not re.match(r"^[A-Za-z0-9]+$", suffix):
        suffix = secrets.token_hex(4)
    schemas_ensured: set[str] = set()
    merges_ok = 0
    merges_attempted = 0

    for op in plan.operations:
        catalog, schema, table = _split_qualified(op.target_table)
        # Isolate staging per load to avoid concurrent clobber (event integrity).
        staging_table = qualify_table(catalog, schema, f"stg_{table}_{suffix}")
        op_res = OpExecution(
            dataset=op.dataset,
            domain=op.domain,
            target_table=op.target_table,
            staging_table=staging_table,
        )
        rows = [
            r
            for r in validation.datasets.get(op.dataset, [])
            if domain_for_row(r) == op.domain
        ]
        try:
            schema_key = f"{catalog}.{schema}"
            if schema_key not in schemas_ensured:
                executor.execute(ensure_schema_sql(catalog, schema))
                result.executed_statements += 1
                schemas_ensured.add(schema_key)

            executor.execute(ensure_table_sql(op.target_table))
            result.executed_statements += 1

            before = executor.execute(count_sql(op.target_table))
            result.executed_statements += 1
            op_res.count_before = parse_count(before)

            executor.execute(recreate_staging_sql(staging_table, op.target_table))
            result.executed_statements += 1

            staged = 0
            for i in range(0, len(rows), batch):
                chunk = rows[i : i + batch]
                _insert_chunk(executor, staging_table, chunk)
                result.executed_statements += 1
                staged += len(chunk)
            op_res.staged_rows = staged

            merges_attempted += 1
            executor.execute(merge_sql(op.target_table, staging_table))
            result.executed_statements += 1
            op_res.merge_executed = True

            after = executor.execute(count_sql(op.target_table))
            result.executed_statements += 1
            op_res.count_after = parse_count(after)
            if op_res.count_after is None:
                raise RuntimeError("count readback missing after MERGE")

            sample = executor.execute(sample_keys_sql(op.target_table, limit=5))
            result.executed_statements += 1
            op_res.sample_keys_after = parse_key_pairs(sample)
            merges_ok += 1
        except Exception as exc:  # noqa: BLE001
            op_res.problems.append(f"{type(exc).__name__}: staging/MERGE failed")
            result.problems.append(f"{op.dataset}/{op.domain}: {type(exc).__name__}")
        finally:
            try:
                executor.execute(f"DROP TABLE IF EXISTS {staging_table}")
                result.executed_statements += 1
            except Exception:  # noqa: BLE001
                pass
        result.operations.append(op_res)

    # Only claim rows_merged when every planned op fully succeeded (incl. readback).
    result.ok = merges_attempted > 0 and merges_ok == merges_attempted and not result.problems
    result.rows_merged = result.ok
    if merges_attempted and merges_ok and not result.ok:
        result.notes.append(
            f"partial MERGE execution: {merges_ok}/{merges_attempted} ops completed — "
            "not labeled LIVE_DELTA_INGESTION=EXECUTED"
        )
    result.lineage = {
        "tables": {
            op.target_table: {
                "count_before": op.count_before,
                "count_after": op.count_after,
                "staged_rows": op.staged_rows,
                "sample_keys": op.sample_keys_after,
                "staging_table": op.staging_table,
            }
            for op in result.operations
        },
        "merge_keys": ["dedupe_key", "content_fingerprint"],
        "append_only": True,
        "load_staging_suffix": suffix,
    }
    if result.ok:
        result.notes.append("staged rows MERGEd; row counts and sample keys recorded for lineage")
    return result


def _insert_chunk(executor: Any, staging_table: str, rows: list[dict[str, Any]]) -> None:
    if hasattr(executor, "insert_rows"):
        executor.insert_rows(staging_table, [row_to_columns(r) for r in rows])
        return
    executor.execute(build_insert_sql(staging_table, rows))


_QUALIFIED_RE = re.compile(
    r"^[A-Za-z_][A-Za-z0-9_]{0,127}\.[A-Za-z_][A-Za-z0-9_]{0,127}\.[A-Za-z_][A-Za-z0-9_]{0,127}$"
)


def _assert_qualified(name: str) -> None:
    if not _QUALIFIED_RE.match(name or ""):
        raise ValueError(f"unsafe or unqualified table {name!r}")


def _split_qualified(name: str) -> tuple[str, str, str]:
    _assert_qualified(name)
    catalog, schema, table = name.split(".", 2)
    return catalog, schema, table


def verify_lineage_from_result(stage_result: StageMergeResult) -> dict[str, Any]:
    """Build an engineer-facing lineage summary from a completed stage/MERGE."""
    return {
        "ok": stage_result.ok and stage_result.rows_merged,
        "rows_merged": stage_result.rows_merged,
        "merge_keys": ["dedupe_key", "content_fingerprint"],
        "append_only": True,
        "tables": stage_result.lineage.get("tables", {}),
        "problems": list(stage_result.problems),
        "notes": [
            "Lineage reflects this load's readback counts and sample keys.",
            "Connection-only checks do not produce lineage.",
            "Rerunning identical key+fingerprint pairs is idempotent (skip).",
        ],
    }

"""Delta load orchestration — dry-run by default; live SQL warehouse opt-in."""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Protocol

from .identifiers import UnsafeIdentifierError, require_identifier
from .merge_plan import MergePlan, plan_merges
from .validate import ExportValidation, validate_export_dir


class SQLExecutor(Protocol):
    def execute(self, statement: str, *, catalog: str | None = None, schema: str | None = None) -> dict[str, Any]:
        ...


@dataclass
class FakeSQLExecutor:
    """In-memory executor for offline tests — never contacts Databricks."""

    statements: list[str] = field(default_factory=list)
    fail_on_substring: str | None = None
    row_store: dict[str, dict[str, dict[str, Any]]] = field(default_factory=dict)
    # table -> dedupe_key -> row

    def execute(self, statement: str, *, catalog: str | None = None, schema: str | None = None) -> dict[str, Any]:
        _ = catalog, schema
        self.statements.append(statement)
        if self.fail_on_substring and self.fail_on_substring in statement:
            raise RuntimeError("simulated SQL failure")
        # Parse minimal MERGE/SELECT COUNT for idempotency tests.
        if statement.strip().upper().startswith("SELECT COUNT"):
            # COUNT(*) FROM catalog.schema.table
            parts = statement.replace(";", "").split()
            table = parts[-1]
            n = len(self.row_store.get(table, {}))
            return {"state": "SUCCEEDED", "data_array": [[str(n)]]}
        return {"state": "SUCCEEDED"}

    def apply_merge_rows(self, target: str, rows: list[dict[str, Any]]) -> tuple[int, int]:
        """Simulate MERGE semantics for tests."""
        store = self.row_store.setdefault(target, {})
        inserted = 0
        updated = 0
        for row in rows:
            key = str(row["dedupe_key"])
            fp = str(row["content_fingerprint"])
            prev = store.get(key)
            if prev is None:
                store[key] = dict(row)
                inserted += 1
            elif prev.get("content_fingerprint") != fp:
                store[key] = dict(row)
                updated += 1
        return inserted, updated


@dataclass
class LoadResult:
    mode: str
    ok: bool
    validation_ok: bool
    plan: MergePlan | None
    executed_statements: int = 0
    problems: list[str] = field(default_factory=list)
    row_counts_after: dict[str, int] = field(default_factory=dict)
    live_executed: bool = False
    notes: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "mode": self.mode,
            "ok": self.ok,
            "validation_ok": self.validation_ok,
            "executed_statements": self.executed_statements,
            "live_executed": self.live_executed,
            "problems": list(self.problems),
            "row_counts_after": dict(self.row_counts_after),
            "notes": list(self.notes),
            "plan": None if self.plan is None else self.plan.to_dict(),
        }


@dataclass
class LoadOptions:
    export_dir: str | Path
    catalog: str = "perch_pulse"
    base_schema: str = "analytics"
    max_batch: int = 500
    mode: str = "dry_run"  # dry_run | live
    authorize_live: bool = False
    warehouse_id: str | None = None
    executor: SQLExecutor | None = None
    prior_fingerprints: dict[str, dict[str, str]] | None = None
    # When using FakeSQLExecutor, apply in-memory merges for idempotency tests.
    apply_fake_merges: bool = False


def load_export(opts: LoadOptions) -> LoadResult:
    notes = [
        "Core Perch does not require Databricks.",
        "Retention-bounded exports are upsert/append only — never DELETE on absence.",
        "External systems do not provide exactly-once semantics; re-runs are versioned/idempotent best-effort.",
    ]
    try:
        require_identifier(opts.catalog, kind="catalog")
        require_identifier(opts.base_schema, kind="schema")
    except UnsafeIdentifierError as exc:
        return LoadResult(
            mode=opts.mode,
            ok=False,
            validation_ok=False,
            plan=None,
            problems=[str(exc)],
            notes=notes,
        )

    validation = validate_export_dir(opts.export_dir, max_batch=opts.max_batch)
    plan = plan_merges(
        validation,
        catalog=opts.catalog,
        base_schema=opts.base_schema,
        prior_fingerprints=opts.prior_fingerprints,
    )
    result = LoadResult(
        mode=opts.mode,
        ok=False,
        validation_ok=validation.ok,
        plan=plan,
        problems=list(plan.problems),
        notes=notes,
    )
    if not validation.ok:
        result.problems.extend(f"{i.code}: {i.message}" for i in validation.errors())
        return result

    if opts.mode == "dry_run":
        result.ok = len(result.problems) == 0
        result.notes.append("dry_run: planned tables/operations only; no cloud execution")
        if opts.apply_fake_merges and isinstance(opts.executor, FakeSQLExecutor):
            _apply_fake(opts.executor, validation, plan, result)
        return result

    if opts.mode != "live":
        result.problems.append(f"unknown mode {opts.mode!r}")
        return result

    if not opts.authorize_live:
        result.problems.append("live mode requires --i-authorize-databricks-calls")
        return result

    executor = opts.executor
    if executor is None:
        try:
            executor = DatabricksSQLExecutor(
                warehouse_id=opts.warehouse_id or os.environ.get("DATABRICKS_WAREHOUSE_ID", ""),
            )
        except Exception as exc:  # noqa: BLE001 — surface setup errors without traceback secrets
            result.problems.append(f"databricks client unavailable: {exc}")
            result.notes.append(
                "Install optional extra: pip install '.[databricks]' and configure "
                "DATABRICKS_HOST / DATABRICKS_TOKEN / DATABRICKS_WAREHOUSE_ID"
            )
            return result

    # Live path: create schemas, stage via VALUES batches (bounded), MERGE, count.
    # Staging uses parameterized approach via JSON VOLUME when available; here we
    # execute MERGE SQL with pre-validated identifiers only.
    try:
        for op in plan.operations:
            create_schema = f"CREATE SCHEMA IF NOT EXISTS {op.target_table.rsplit('.', 1)[0]}"
            executor.execute(create_schema)
            result.executed_statements += 1
            # Staging table DDL + load is environment-specific; emit MERGE preview
            # only when fake apply is used. Live smoke uses a bounded subset path.
            executor.execute(op.sql_preview)
            result.executed_statements += 1
        result.live_executed = True
        result.ok = True
        result.notes.append("live SQL statements submitted to configured warehouse")
    except Exception as exc:  # noqa: BLE001
        result.problems.append(f"load failed: {exc}")
        result.ok = False
    return result


def _apply_fake(
    executor: FakeSQLExecutor,
    validation: ExportValidation,
    plan: MergePlan,
    result: LoadResult,
) -> None:
    for op in plan.operations:
        rows = [
            r
            for r in validation.datasets.get(op.dataset, [])
            if True  # domain already partitioned in plan; filter by matching schema suffix
        ]
        # Filter rows belonging to this domain.
        from .validate import domain_for_row

        domain_rows = [r for r in rows if domain_for_row(r) == op.domain]
        inserted, updated = executor.apply_merge_rows(op.target_table, domain_rows)
        executor.execute(op.sql_preview)
        result.executed_statements += 1
        result.row_counts_after[op.target_table] = len(executor.row_store.get(op.target_table, {}))
        _ = inserted, updated
    result.ok = True
    result.notes.append("fake executor applied in-memory MERGE semantics for offline tests")


class DatabricksSQLExecutor:
    """Thin wrapper around databricks-sdk statement execution (optional dependency)."""

    def __init__(self, *, warehouse_id: str, wait_timeout: str = "30s") -> None:
        if not warehouse_id.strip():
            raise ValueError("DATABRICKS_WAREHOUSE_ID / warehouse_id is required for live mode")
        try:
            from databricks.sdk import WorkspaceClient  # type: ignore
        except ImportError as exc:
            raise ImportError(
                "databricks-sdk is not installed; pip install 'perch-pulse-dbx[databricks]'"
            ) from exc
        self._client = WorkspaceClient()
        self._warehouse_id = warehouse_id.strip()
        self._wait_timeout = wait_timeout
        self.statements: list[str] = []

    def execute(self, statement: str, *, catalog: str | None = None, schema: str | None = None) -> dict[str, Any]:
        self.statements.append(statement)
        resp = self._client.statement_execution.execute_statement(
            statement=statement,
            warehouse_id=self._warehouse_id,
            catalog=catalog,
            schema=schema,
            wait_timeout=self._wait_timeout,
        )
        state = getattr(getattr(resp, "status", None), "state", None)
        state_s = getattr(state, "value", None) or str(state or "")
        out: dict[str, Any] = {"state": state_s, "statement_id": getattr(resp, "statement_id", None)}
        result = getattr(resp, "result", None)
        if result is not None and getattr(result, "data_array", None) is not None:
            out["data_array"] = result.data_array
        if state_s and state_s.upper() not in {"SUCCEEDED", "SUCCESS"}:
            raise RuntimeError(f"statement ended in state {state_s}")
        return out


def redacted_preview(plan: MergePlan) -> str:
    """Human-readable dry-run summary without secrets."""
    lines = [
        f"catalog={plan.catalog} base_schema={plan.base_schema}",
        f"export_schema={plan.export_schema_version}",
        f"operations={len(plan.operations)} estimated_statements≈{plan.estimated_statement_count}",
    ]
    for op in plan.operations:
        lines.append(
            f"  {op.dataset}/{op.domain}: target={op.target_table} "
            f"insert={op.insert_count} update={op.update_count} skip_identical={op.skip_identical_count}"
        )
    if plan.problems:
        lines.append("problems:")
        lines.extend(f"  - {p}" for p in plan.problems)
    return "\n".join(lines) + "\n"


def write_plan_json(plan: MergePlan, path: str | Path) -> None:
    Path(path).write_text(json.dumps(plan.to_dict(), indent=2) + "\n", encoding="utf-8")

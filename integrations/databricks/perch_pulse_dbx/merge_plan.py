"""Deterministic MERGE plan generation (no cloud calls)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from . import SUPPORTED_DATASETS
from .identifiers import qualify_table, schema_for_domain, table_for_dataset
from .validate import ExportValidation, domain_for_row


@dataclass
class MergeOperation:
    dataset: str
    domain: str
    target_table: str
    staging_table: str
    merge_keys: list[str]
    insert_count: int
    update_count: int  # always 0 under append-only fingerprint history
    skip_identical_count: int
    sample_dedupe_keys: list[str] = field(default_factory=list)
    sql_preview: str = ""


@dataclass
class MergePlan:
    catalog: str
    base_schema: str
    export_schema_version: str
    operations: list[MergeOperation] = field(default_factory=list)
    problems: list[str] = field(default_factory=list)
    estimated_statement_count: int = 0
    notes: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "catalog": self.catalog,
            "base_schema": self.base_schema,
            "export_schema_version": self.export_schema_version,
            "estimated_statement_count": self.estimated_statement_count,
            "problems": list(self.problems),
            "notes": list(self.notes),
            "operations": [
                {
                    "dataset": op.dataset,
                    "domain": op.domain,
                    "target_table": op.target_table,
                    "staging_table": op.staging_table,
                    "merge_keys": op.merge_keys,
                    "insert_count": op.insert_count,
                    "update_count": op.update_count,
                    "skip_identical_count": op.skip_identical_count,
                    "sample_dedupe_keys": op.sample_dedupe_keys,
                    "sql_preview": op.sql_preview,
                }
                for op in self.operations
            ],
        }


def merge_keys_for(_dataset: str) -> list[str]:
    # F3a emits a new JSONL line when content_fingerprint changes for the same
    # dedupe_key. Warehouse ingest is append-only history: match both columns.
    # Never UPDATE-collapse fingerprint history; never DELETE on absence.
    return ["dedupe_key", "content_fingerprint"]


def build_merge_sql(target: str, staging: str) -> str:
    # Identifiers already validated/qualified. Values are not interpolated.
    return (
        f"MERGE INTO {target} AS t\n"
        f"USING {staging} AS s\n"
        f"ON t.dedupe_key = s.dedupe_key "
        f"AND t.content_fingerprint = s.content_fingerprint\n"
        f"WHEN NOT MATCHED THEN INSERT *\n"
        f"-- Append-only: fingerprint history is retained; absence never DELETEs"
    )


def plan_merges(
    validation: ExportValidation,
    *,
    catalog: str,
    base_schema: str,
    prior_fingerprints: dict[str, dict[str, str]] | None = None,
) -> MergePlan:
    """Build a dry-run MERGE plan from a validated export.

    prior_fingerprints: optional map dataset -> {dedupe_key: content_fingerprint}
    representing one known fingerprint per key (preview only).
    """
    plan = MergePlan(
        catalog=catalog,
        base_schema=base_schema,
        export_schema_version=validation.export_schema_version or "",
        notes=[
            "Dry-run plan only — no Databricks API calls are made by plan_merges.",
            "MERGE is append-only on (dedupe_key, content_fingerprint).",
            "Missing keys never become DELETE; fingerprint changes insert a new row.",
            "Synthetic and observed rows land in separate schema suffixes.",
            "Idempotency is best-effort for external systems; identical key+fingerprint skips.",
            "Estimated statement counts exclude unlanded staging file ingestion.",
        ],
    )
    if not validation.ok:
        plan.problems.extend(f"{i.code}: {i.message}" for i in validation.errors())
        return plan

    prior = prior_fingerprints or {}
    for dataset in SUPPORTED_DATASETS:
        rows = validation.datasets.get(dataset, [])
        if not rows:
            continue
        by_domain: dict[str, list[dict[str, Any]]] = {}
        for row in rows:
            by_domain.setdefault(domain_for_row(row), []).append(row)

        for domain, domain_rows in sorted(by_domain.items()):
            try:
                schema = schema_for_domain(base_schema, domain)
                table = table_for_dataset(dataset)
                target = qualify_table(catalog, schema, table)
                staging = qualify_table(catalog, schema, f"stg_{table}")
            except ValueError as exc:
                plan.problems.append(str(exc))
                continue

            known = prior.get(dataset, {})
            insert_n = 0
            skip_n = 0
            samples: list[str] = []
            for row in domain_rows:
                key = str(row["dedupe_key"])
                fp = str(row["content_fingerprint"])
                if len(samples) < 5:
                    samples.append(key)
                prev = known.get(key)
                if prev == fp:
                    skip_n += 1
                else:
                    # New key, or new fingerprint for existing key → insert history row.
                    insert_n += 1

            op = MergeOperation(
                dataset=dataset,
                domain=domain,
                target_table=target,
                staging_table=staging,
                merge_keys=merge_keys_for(dataset),
                insert_count=insert_n,
                update_count=0,
                skip_identical_count=skip_n,
                sample_dedupe_keys=samples,
                sql_preview=build_merge_sql(target, staging),
            )
            plan.operations.append(op)

    # Schema ensure + MERGE per op (staging land is a separate prerequisite).
    plan.estimated_statement_count = len(plan.operations) * 2
    return plan

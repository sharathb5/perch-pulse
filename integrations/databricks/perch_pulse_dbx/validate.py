"""Validate F3a export directories before any Databricks load."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from . import (
    CHECKPOINT_SCHEMA_VERSION,
    EXPORT_SCHEMA_VERSION,
    MANIFEST_SCHEMA_VERSION,
    PROVENANCE_DOMAINS,
    QUALITY_REPORT_SCHEMA_VERSION,
    SUPPORTED_DATASETS,
)

ENVELOPE_REQUIRED = (
    "export_schema_version",
    "dataset",
    "dedupe_key",
    "content_fingerprint",
    "source_schema_version",
    "provenance_class",
    "exported_at",
)

MAX_BATCH_DEFAULT = 500


@dataclass
class ValidationIssue:
    severity: str  # error | warn
    code: str
    message: str
    dataset: str | None = None


@dataclass
class ExportValidation:
    export_dir: Path
    ok: bool
    export_schema_version: str | None = None
    datasets: dict[str, list[dict[str, Any]]] = field(default_factory=dict)
    counts: dict[str, int] = field(default_factory=dict)
    issues: list[ValidationIssue] = field(default_factory=list)
    manifest: dict[str, Any] | None = None
    checkpoint: dict[str, Any] | None = None
    quality: dict[str, Any] | None = None

    def errors(self) -> list[ValidationIssue]:
        return [i for i in self.issues if i.severity == "error"]


def _read_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    if not isinstance(data, dict):
        raise ValueError(f"{path.name}: expected JSON object")
    return data


def _read_jsonl(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8") as fh:
        for lineno, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ValueError(f"{path.name}:{lineno}: malformed JSONL: {exc}") from exc
            if not isinstance(obj, dict):
                raise ValueError(f"{path.name}:{lineno}: expected object")
            rows.append(obj)
    return rows


def validate_export_dir(export_dir: str | Path, *, max_batch: int = MAX_BATCH_DEFAULT) -> ExportValidation:
    root = Path(export_dir).resolve()
    result = ExportValidation(export_dir=root, ok=False)
    if not root.is_dir():
        result.issues.append(ValidationIssue("error", "missing_export_dir", f"not a directory: {root}"))
        return result

    manifest_path = root / "export-manifest.json"
    checkpoint_path = root / "checkpoint.json"
    quality_path = root / "quality-report.json"

    if not checkpoint_path.is_file():
        result.issues.append(ValidationIssue("error", "missing_checkpoint", "checkpoint.json is required"))
        return result
    if not manifest_path.is_file():
        result.issues.append(ValidationIssue("error", "missing_manifest", "export-manifest.json is required"))
        return result

    try:
        result.checkpoint = _read_json(checkpoint_path)
        result.manifest = _read_json(manifest_path)
        if quality_path.is_file():
            result.quality = _read_json(quality_path)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        result.issues.append(ValidationIssue("error", "sidecar_unreadable", str(exc)))
        return result

    if result.checkpoint.get("schema_version") != CHECKPOINT_SCHEMA_VERSION:
        result.issues.append(
            ValidationIssue(
                "error",
                "unsupported_checkpoint_schema",
                f"got {result.checkpoint.get('schema_version')!r}, want {CHECKPOINT_SCHEMA_VERSION!r}",
            )
        )
    if result.manifest.get("schema_version") != MANIFEST_SCHEMA_VERSION:
        result.issues.append(
            ValidationIssue(
                "error",
                "unsupported_manifest_schema",
                f"got {result.manifest.get('schema_version')!r}, want {MANIFEST_SCHEMA_VERSION!r}",
            )
        )

    export_schema = result.manifest.get("export_schema_version") or result.checkpoint.get(
        "export_schema_version"
    )
    result.export_schema_version = export_schema
    if export_schema != EXPORT_SCHEMA_VERSION:
        result.issues.append(
            ValidationIssue(
                "error",
                "unsupported_export_schema",
                f"got {export_schema!r}, want {EXPORT_SCHEMA_VERSION!r} — refusing to guess mappings",
            )
        )
        return result

    if result.quality is not None:
        if result.quality.get("schema_version") not in (None, QUALITY_REPORT_SCHEMA_VERSION):
            result.issues.append(
                ValidationIssue(
                    "warn",
                    "quality_schema_mismatch",
                    f"quality-report schema {result.quality.get('schema_version')!r}",
                )
            )
        if result.quality.get("ok") is False:
            result.issues.append(
                ValidationIssue("error", "quality_not_ok", "quality-report.json reports ok=false")
            )

    seen_keys: dict[str, set[str]] = {ds: set() for ds in SUPPORTED_DATASETS}
    for dataset in SUPPORTED_DATASETS:
        path = root / f"{dataset}.jsonl"
        if not path.is_file():
            result.datasets[dataset] = []
            result.counts[dataset] = 0
            continue
        try:
            rows = _read_jsonl(path)
        except ValueError as exc:
            result.issues.append(ValidationIssue("error", "malformed_jsonl", str(exc), dataset=dataset))
            continue
        if len(rows) > max_batch:
            result.issues.append(
                ValidationIssue(
                    "error",
                    "batch_too_large",
                    f"{dataset} has {len(rows)} rows; max_batch={max_batch}",
                    dataset=dataset,
                )
            )
        valid_rows: list[dict[str, Any]] = []
        for idx, row in enumerate(rows):
            for key in ENVELOPE_REQUIRED:
                if key not in row:
                    result.issues.append(
                        ValidationIssue(
                            "error",
                            "missing_envelope_field",
                            f"{dataset} row {idx}: missing {key}",
                            dataset=dataset,
                        )
                    )
            if row.get("export_schema_version") != EXPORT_SCHEMA_VERSION:
                result.issues.append(
                    ValidationIssue(
                        "error",
                        "row_schema_mismatch",
                        f"{dataset} row {idx}: export_schema_version {row.get('export_schema_version')!r}",
                        dataset=dataset,
                    )
                )
                continue
            if row.get("dataset") != dataset:
                result.issues.append(
                    ValidationIssue(
                        "error",
                        "dataset_field_mismatch",
                        f"{dataset} row {idx}: dataset field {row.get('dataset')!r}",
                        dataset=dataset,
                    )
                )
                continue
            prov = row.get("provenance_class")
            if prov not in PROVENANCE_DOMAINS:
                result.issues.append(
                    ValidationIssue(
                        "error",
                        "unknown_provenance",
                        f"{dataset} row {idx}: provenance_class {prov!r}",
                        dataset=dataset,
                    )
                )
                continue
            dk = str(row.get("dedupe_key", ""))
            if dk in seen_keys[dataset]:
                # Same key may appear with different fingerprints (append history).
                # Duplicate key+fingerprint is an error.
                prior = [
                    r
                    for r in valid_rows
                    if r.get("dedupe_key") == dk
                    and r.get("content_fingerprint") == row.get("content_fingerprint")
                ]
                if prior:
                    result.issues.append(
                        ValidationIssue(
                            "error",
                            "duplicate_key_fingerprint",
                            f"{dataset}: duplicate dedupe_key+fingerprint {dk}",
                            dataset=dataset,
                        )
                    )
                    continue
            seen_keys[dataset].add(dk)
            valid_rows.append(row)
        result.datasets[dataset] = valid_rows
        result.counts[dataset] = len(valid_rows)

    # Soft join checks: proposal → investigation; feedback → target.
    inv_ids = {r.get("investigation_id") for r in result.datasets.get("investigations", [])}
    for row in result.datasets.get("proposals", []):
        src = row.get("source_investigation_id")
        if src and inv_ids and src not in inv_ids:
            result.issues.append(
                ValidationIssue(
                    "warn",
                    "broken_join_proposal",
                    f"proposal {row.get('proposal_id')} references missing investigation {src}",
                    dataset="proposals",
                )
            )

    result.ok = len(result.errors()) == 0
    return result


def domain_for_row(row: dict[str, Any]) -> str:
    prov = row.get("provenance_class", "unknown")
    return PROVENANCE_DOMAINS.get(str(prov), "unknown")

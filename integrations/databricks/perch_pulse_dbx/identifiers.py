"""Safe SQL identifier validation — never interpolate untrusted names."""

from __future__ import annotations

import re

_IDENT_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{0,127}$")

# Allowed physical table names mapped from F3a datasets.
TABLE_FOR_DATASET = {
    "incidents": "pulse_incidents",
    "incident_signals": "pulse_incident_signals",
    "investigations": "pulse_investigations",
    "hypotheses": "pulse_hypotheses",
    "citations": "pulse_citations",
    "investigation_feedback": "pulse_investigation_feedback",
    "proposals": "pulse_proposals",
    "eval_runs": "pulse_eval_runs",
    "eval_case_results": "pulse_eval_case_results",
}

# Schema namespaces for provenance domain isolation.
DOMAIN_SCHEMA_SUFFIX = {
    "observed": "observed",
    "synthetic": "synthetic",
    "model": "model",
    "human": "human",
    "evaluation": "evaluation",
    "unknown": "unknown",
}


class UnsafeIdentifierError(ValueError):
    """Raised when a catalog/schema/table identifier fails allowlist checks."""


def require_identifier(name: str, *, kind: str = "identifier") -> str:
    value = (name or "").strip()
    if not _IDENT_RE.match(value):
        raise UnsafeIdentifierError(
            f"unsafe {kind} {name!r}: must match {_IDENT_RE.pattern}"
        )
    return value


def qualify_table(catalog: str, schema: str, table: str) -> str:
    c = require_identifier(catalog, kind="catalog")
    s = require_identifier(schema, kind="schema")
    t = require_identifier(table, kind="table")
    return f"{c}.{s}.{t}"


def table_for_dataset(dataset: str) -> str:
    if dataset not in TABLE_FOR_DATASET:
        raise UnsafeIdentifierError(f"unsupported dataset {dataset!r}")
    return TABLE_FOR_DATASET[dataset]


def schema_for_domain(base_schema: str, domain: str) -> str:
    base = require_identifier(base_schema, kind="schema")
    if domain not in DOMAIN_SCHEMA_SUFFIX:
        raise UnsafeIdentifierError(f"unsupported provenance domain {domain!r}")
    suffix = DOMAIN_SCHEMA_SUFFIX[domain]
    return require_identifier(f"{base}_{suffix}", kind="schema")

"""Optional Databricks Delta + MLflow integration for Pulse F3a exports.

Core Perch CLI/UI do not import this package. Cloud SDKs are optional extras.
"""

__version__ = "0.1.0"

EXPORT_SCHEMA_VERSION = "pulse.export.analytics.v1"
CHECKPOINT_SCHEMA_VERSION = "pulse.export.checkpoint.v1"
MANIFEST_SCHEMA_VERSION = "pulse.export.manifest.v1"
QUALITY_REPORT_SCHEMA_VERSION = "pulse.export.quality_report.v1"

SUPPORTED_DATASETS = (
    "incidents",
    "incident_signals",
    "investigations",
    "hypotheses",
    "citations",
    "investigation_feedback",
    "proposals",
    "eval_runs",
    "eval_case_results",
)

# Domain isolation via catalog schema suffixes / provenance partitions.
PROVENANCE_DOMAINS = {
    "observed_local": "observed",
    "historical_demo": "observed",
    "synthetic_fixture": "synthetic",
    "model_generated": "model",
    "human_assessment": "human",
    "evaluation_annotation": "evaluation",
    "unknown": "unknown",
}

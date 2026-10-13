"""CLI for offline dry-run / explicit live Databricks + MLflow workflows."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .load import FakeSQLExecutor, LoadOptions, load_export, redacted_preview, write_plan_json
from .mlflow_eval import FakeMLflowClient, log_eval_report
from .validate import validate_export_dir


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="perch-pulse-dbx",
        description="Optional Databricks Delta + MLflow integration for Pulse F3a exports",
    )
    sub = p.add_subparsers(dest="cmd", required=True)

    v = sub.add_parser("validate", help="Validate an F3a export directory")
    v.add_argument("--export-dir", required=True)
    v.add_argument("--max-batch", type=int, default=500)
    v.add_argument("--json", action="store_true")

    d = sub.add_parser("dry-run", help="Plan Delta MERGE operations (default; no cloud calls)")
    d.add_argument("--export-dir", required=True)
    d.add_argument("--catalog", default="perch_pulse")
    d.add_argument("--schema", default="analytics", dest="base_schema")
    d.add_argument("--max-batch", type=int, default=500)
    d.add_argument("--out", default="", help="Write plan JSON to this path")
    d.add_argument(
        "--simulate-merge",
        action="store_true",
        help="Offline: land staging + MERGE via FakeSQLExecutor (no cloud)",
    )
    d.add_argument("--json", action="store_true")

    live = sub.add_parser(
        "live-load",
        help="Authorized live warehouse path (connection check; optional staging+MERGE)",
    )
    live.add_argument("--export-dir", required=True)
    live.add_argument("--catalog", default="perch_pulse")
    live.add_argument("--schema", default="analytics", dest="base_schema")
    live.add_argument("--warehouse-id", default="")
    live.add_argument("--i-authorize-databricks-calls", action="store_true")
    live.add_argument("--max-batch", type=int, default=50)
    live.add_argument(
        "--connection-only",
        action="store_true",
        help="Only verify warehouse connectivity (SELECT 1); do not MERGE",
    )
    live.add_argument(
        "--execute-merge",
        action="store_true",
        help="Stage JSONL into stg_* tables and run append-only MERGE (requires authorize)",
    )
    live.add_argument("--json", action="store_true")

    m = sub.add_parser("mlflow-log", help="Log an investigation-eval report to MLflow (fake by default)")
    m.add_argument("--report", required=True, help="Path to investigation-eval report.json")
    m.add_argument("--experiment", default="perch-pulse-investigation-eval")
    m.add_argument("--live", action="store_true", help="Use real mlflow client")
    m.add_argument("--i-authorize-mlflow-calls", action="store_true")
    m.add_argument("--json", action="store_true")

    lin = sub.add_parser(
        "verify-lineage",
        help="Offline: simulate stage+MERGE and print lineage readback (no cloud)",
    )
    lin.add_argument("--export-dir", required=True)
    lin.add_argument("--catalog", default="perch_pulse")
    lin.add_argument("--schema", default="analytics", dest="base_schema")
    lin.add_argument("--max-batch", type=int, default=50)
    lin.add_argument("--json", action="store_true")

    s = sub.add_parser("live-smoke", help="Bounded live smoke (auth + dry validation + optional load/mlflow)")
    s.add_argument("--export-dir", required=True)
    s.add_argument("--report", default="", help="Optional eval report.json for MLflow")
    s.add_argument("--catalog", default="perch_pulse")
    s.add_argument("--schema", default="analytics", dest="base_schema")
    s.add_argument("--warehouse-id", default="")
    s.add_argument("--i-authorize-databricks-calls", action="store_true")
    s.add_argument("--i-authorize-mlflow-calls", action="store_true")
    s.add_argument("--execute-merge", action="store_true")
    s.add_argument("--max-batch", type=int, default=50)
    s.add_argument("--json", action="store_true")
    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.cmd == "validate":
        res = validate_export_dir(args.export_dir, max_batch=args.max_batch)
        payload = {
            "ok": res.ok,
            "export_dir": str(res.export_dir),
            "export_schema_version": res.export_schema_version,
            "counts": res.counts,
            "issues": [
                {"severity": i.severity, "code": i.code, "message": i.message, "dataset": i.dataset}
                for i in res.issues
            ],
        }
        if args.json:
            print(json.dumps(payload, indent=2))
        else:
            print(f"ok={res.ok} schema={res.export_schema_version}")
            for ds, n in res.counts.items():
                if n:
                    print(f"  {ds}: {n}")
            for i in res.issues:
                print(f"  [{i.severity}] {i.code}: {i.message}")
        return 0 if res.ok else 2

    if args.cmd == "dry-run":
        fake = FakeSQLExecutor() if args.simulate_merge else None
        result = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode="dry_run",
                executor=fake,
                execute_merge=bool(args.simulate_merge),
            )
        )
        if args.out and result.plan is not None:
            write_plan_json(result.plan, args.out)
        if args.json:
            print(json.dumps(result.to_dict(), indent=2))
        else:
            if result.plan is not None:
                print(redacted_preview(result.plan), end="")
            if result.rows_merged:
                print(f"simulate_merge rows_merged=true counts={result.row_counts_after}")
            for p in result.problems:
                print(f"problem: {p}", file=sys.stderr)
        return 0 if result.ok else 2

    if args.cmd == "live-load":
        if args.execute_merge and args.connection_only:
            print("choose either --connection-only or --execute-merge, not both", file=sys.stderr)
            return 2
        mode = "live" if args.execute_merge else "live_connection"
        result = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode=mode,
                authorize_live=bool(args.i_authorize_databricks_calls),
                warehouse_id=args.warehouse_id or None,
                execute_merge=bool(args.execute_merge),
            )
        )
        if args.json:
            print(json.dumps(result.to_dict(), indent=2))
        else:
            print(
                f"ok={result.ok} live_connection_ok={result.live_connection_ok} "
                f"rows_merged={result.rows_merged} statements={result.executed_statements}"
            )
            for p in result.problems:
                print(f"problem: {p}", file=sys.stderr)
        return 0 if result.ok else 2

    if args.cmd == "verify-lineage":
        fake = FakeSQLExecutor()
        result = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode="dry_run",
                executor=fake,
                execute_merge=True,
            )
        )
        # Second pass proves idempotent skip on identical fingerprints.
        result2 = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode="dry_run",
                executor=fake,
                execute_merge=True,
            )
        )
        payload = {
            "ok": result.ok and result2.ok,
            "rows_merged": result.rows_merged,
            "lineage": result.lineage,
            "row_counts_after_first": result.row_counts_after,
            "row_counts_after_rerun": result2.row_counts_after,
            "idempotent_rerun": result.row_counts_after == result2.row_counts_after,
            "notes": [
                "OFFLINE lineage simulation via FakeSQLExecutor — not a live warehouse readback.",
                "Authorized live lineage requires live-load --execute-merge.",
            ],
        }
        if args.json:
            print(json.dumps(payload, indent=2))
        else:
            print(
                f"ok={payload['ok']} rows_merged={payload['rows_merged']} "
                f"idempotent_rerun={payload['idempotent_rerun']}"
            )
            if result.lineage:
                for table, meta in (result.lineage.get("tables") or {}).items():
                    print(f"  {table}: {meta}")
        return 0 if payload["ok"] else 2

    if args.cmd == "mlflow-log":
        live = bool(args.live)
        if live and not args.i_authorize_mlflow_calls:
            print("live MLflow requires --i-authorize-mlflow-calls", file=sys.stderr)
            return 2
        client = None if live else FakeMLflowClient()
        res = log_eval_report(
            args.report,
            client=client,
            experiment=args.experiment,
            live=live,
        )
        if args.json:
            print(json.dumps(res.to_dict(), indent=2))
        else:
            print(f"ok={res.ok} run_id={res.run_id} live={res.live_executed}")
            for p in res.problems:
                print(f"note: {p}")
        return 0 if res.ok else 2

    if args.cmd == "live-smoke":
        # Always validate + dry-run first. Live steps only with explicit auth.
        summary: dict = {"steps": []}
        ok = True
        val = validate_export_dir(args.export_dir, max_batch=args.max_batch)
        summary["steps"].append({"step": "validate", "ok": val.ok, "counts": val.counts})
        ok = ok and val.ok
        dry = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode="dry_run",
            )
        )
        summary["steps"].append({"step": "dry_run", "ok": dry.ok, "plan": dry.plan.to_dict() if dry.plan else None})
        ok = ok and dry.ok
        if not args.i_authorize_databricks_calls:
            summary["live_databricks"] = "NOT RUN — missing --i-authorize-databricks-calls"
            summary["LIVE_DELTA_INGESTION"] = "NOT RUN"
        else:
            mode = "live" if args.execute_merge else "live_connection"
            live = load_export(
                LoadOptions(
                    export_dir=args.export_dir,
                    catalog=args.catalog,
                    base_schema=args.base_schema,
                    max_batch=args.max_batch,
                    mode=mode,
                    authorize_live=True,
                    warehouse_id=args.warehouse_id or None,
                    execute_merge=bool(args.execute_merge),
                )
            )
            summary["steps"].append(
                {
                    "step": "live_databricks",
                    "ok": live.ok,
                    "live_connection_ok": live.live_connection_ok,
                    "rows_merged": live.rows_merged,
                    "lineage": live.lineage,
                }
            )
            if live.ok and live.rows_merged:
                summary["live_databricks"] = "MERGE_OK"
                summary["LIVE_DELTA_INGESTION"] = "EXECUTED"
            elif live.rows_merged and not live.ok:
                summary["live_databricks"] = "PARTIAL_FAILURE"
                summary["LIVE_DELTA_INGESTION"] = "PARTIAL_FAILURE"
            elif live.live_connection_ok:
                summary["live_databricks"] = "CONNECTION_OK"
                summary["LIVE_DELTA_INGESTION"] = "NOT RUN — connection only (pass --execute-merge)"
            else:
                summary["live_databricks"] = "FAILED_OR_SKIPPED"
                summary["LIVE_DELTA_INGESTION"] = "FAILED_OR_SKIPPED"
            ok = ok and live.ok
        if args.report:
            if not args.i_authorize_mlflow_calls:
                summary["mlflow"] = "NOT RUN — missing --i-authorize-mlflow-calls"
                summary["LIVE_MLFLOW_TRACKING"] = "NOT RUN"
            else:
                mres = log_eval_report(args.report, live=True, experiment="perch-pulse-investigation-eval")
                summary["steps"].append({"step": "mlflow", "ok": mres.ok, "run_id": mres.run_id})
                summary["mlflow"] = "EXECUTED" if mres.live_executed else "FAILED_OR_SKIPPED"
                summary["LIVE_MLFLOW_TRACKING"] = summary["mlflow"]
                ok = ok and mres.ok
        else:
            summary["LIVE_MLFLOW_TRACKING"] = "NOT RUN — no --report"
        print(json.dumps(summary, indent=2))
        return 0 if ok else 2

    return 2


if __name__ == "__main__":
    raise SystemExit(main())

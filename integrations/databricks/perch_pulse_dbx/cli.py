"""CLI for offline dry-run / explicit live Databricks + MLflow workflows."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .load import LoadOptions, load_export, redacted_preview, write_plan_json
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
    d.add_argument("--json", action="store_true")

    live = sub.add_parser(
        "live-load",
        help="Live warehouse connection check + plan (requires authorization; does not invent staged MERGEs)",
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
        default=True,
        help="Only verify warehouse connectivity (default; does not MERGE unlanded staging)",
    )
    live.add_argument("--json", action="store_true")

    m = sub.add_parser("mlflow-log", help="Log an investigation-eval report to MLflow (fake by default)")
    m.add_argument("--report", required=True, help="Path to investigation-eval report.json")
    m.add_argument("--experiment", default="perch-pulse-investigation-eval")
    m.add_argument("--live", action="store_true", help="Use real mlflow client")
    m.add_argument("--i-authorize-mlflow-calls", action="store_true")
    m.add_argument("--json", action="store_true")

    s = sub.add_parser("live-smoke", help="Bounded live smoke (auth + dry validation + optional load/mlflow)")
    s.add_argument("--export-dir", required=True)
    s.add_argument("--report", default="", help="Optional eval report.json for MLflow")
    s.add_argument("--catalog", default="perch_pulse")
    s.add_argument("--schema", default="analytics", dest="base_schema")
    s.add_argument("--warehouse-id", default="")
    s.add_argument("--i-authorize-databricks-calls", action="store_true")
    s.add_argument("--i-authorize-mlflow-calls", action="store_true")
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
        result = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode="dry_run",
            )
        )
        if args.out and result.plan is not None:
            write_plan_json(result.plan, args.out)
        if args.json:
            print(json.dumps(result.to_dict(), indent=2))
        else:
            if result.plan is not None:
                print(redacted_preview(result.plan), end="")
            for p in result.problems:
                print(f"problem: {p}", file=sys.stderr)
        return 0 if result.ok else 2

    if args.cmd == "live-load":
        result = load_export(
            LoadOptions(
                export_dir=args.export_dir,
                catalog=args.catalog,
                base_schema=args.base_schema,
                max_batch=args.max_batch,
                mode="live_connection",
                authorize_live=bool(args.i_authorize_databricks_calls),
                warehouse_id=args.warehouse_id or None,
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
        else:
            live = load_export(
                LoadOptions(
                    export_dir=args.export_dir,
                    catalog=args.catalog,
                    base_schema=args.base_schema,
                    max_batch=args.max_batch,
                    mode="live_connection",
                    authorize_live=True,
                    warehouse_id=args.warehouse_id or None,
                )
            )
            summary["steps"].append(
                {
                    "step": "live_connection",
                    "ok": live.ok,
                    "live_connection_ok": live.live_connection_ok,
                    "rows_merged": live.rows_merged,
                }
            )
            summary["live_databricks"] = (
                "CONNECTION_OK" if live.live_connection_ok else "FAILED_OR_SKIPPED"
            )
            ok = ok and live.ok
        if args.report:
            if not args.i_authorize_mlflow_calls:
                summary["mlflow"] = "NOT RUN — missing --i-authorize-mlflow-calls"
            else:
                mres = log_eval_report(args.report, live=True, experiment="perch-pulse-investigation-eval")
                summary["steps"].append({"step": "mlflow", "ok": mres.ok, "run_id": mres.run_id})
                summary["mlflow"] = "EXECUTED" if mres.live_executed else "FAILED_OR_SKIPPED"
                ok = ok and mres.ok
        print(json.dumps(summary, indent=2))
        return 0 if ok else 2

    return 2


if __name__ == "__main__":
    raise SystemExit(main())

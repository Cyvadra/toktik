#!/usr/bin/env python3
"""Run selected Toktik DSL strategies against a running api-server."""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_STRATEGIES = [
    "pkg/dsl/scripts/strategies/index-options.toktik",
    "pkg/dsl/scripts/strategies/strong-momentum.toktik",
    "pkg/dsl/scripts/strategies/value-allocation.toktik",
]

DEFAULT_PRELOAD_SYMBOLS = {
    "index-options.toktik": ["SPY", "QQQ"],
}

DEFAULT_REPORTS_DIR = Path("tmp/dsl-backtest-reports")

TERMINAL_STATUSES = {"completed", "failed", "canceled"}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Submit DSL strategy backtests to a Toktik api-server sequentially."
    )
    parser.add_argument("--base-url", default="http://127.0.0.1:9010", help="api-server base URL")
    parser.add_argument("--api-key", default=os.getenv("TOKTIK_API_KEY", ""), help="API key, defaults to TOKTIK_API_KEY")
    parser.add_argument("--from", dest="from_date", default="2024-01-01", help="backtest start date")
    parser.add_argument("--to", dest="to_date", default="2024-12-31", help="backtest end date")
    parser.add_argument("--capital", type=float, default=100000.0, help="initial capital")
    parser.add_argument("--market", default="us", help="underlying market")
    parser.add_argument("--asset", default="SPY", help="primary asset used to load bars")
    parser.add_argument("--interval", default="1d", help="bar interval")
    parser.add_argument(
        "--symbols",
        nargs="+",
        help="option-chain symbols to preload for every submitted strategy",
    )
    parser.add_argument("--timeout", type=float, default=1800.0, help="max seconds to wait for each run")
    parser.add_argument("--poll-interval", type=float, default=5.0, help="seconds between status polls")
    parser.add_argument("--output", type=Path, help="optional JSON file for full run statuses")
    parser.add_argument(
        "--reports-dir",
        type=Path,
        default=DEFAULT_REPORTS_DIR,
        help=f"directory to download completed HTML reports, defaults to {DEFAULT_REPORTS_DIR}",
    )
    parser.add_argument(
        "--no-download-reports",
        action="store_true",
        help="skip downloading completed HTML reports",
    )
    parser.add_argument(
        "strategies",
        nargs="*",
        type=Path,
        help="DSL strategy files. Defaults to the three bundled options allocation strategies.",
    )
    return parser.parse_args()


def repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def request_json(base_url: str, path: str, api_key: str, method: str = "GET", payload: dict[str, Any] | None = None) -> dict[str, Any]:
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if api_key:
        headers["X-API-Key"] = api_key

    url = base_url.rstrip("/") + path
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {path} returned HTTP {exc.code}: {body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"cannot connect to {url}: {exc.reason}") from exc

    if not raw.strip():
        return {}
    return json.loads(raw)


def request_bytes(base_url: str, path: str, api_key: str) -> bytes:
    headers = {"Accept": "text/html"}
    if api_key:
        headers["X-API-Key"] = api_key

    url = base_url.rstrip("/") + path
    req = urllib.request.Request(url, headers=headers, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return resp.read()
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"GET {path} returned HTTP {exc.code}: {body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"cannot connect to {url}: {exc.reason}") from exc


def build_payload(args: argparse.Namespace, strategy_path: Path, dsl: str) -> dict[str, Any]:
    payload = {
        "market": args.market,
        "asset": args.asset,
        "interval": args.interval,
        "from": args.from_date,
        "to": args.to_date,
        "capital": args.capital,
        "dsl": dsl,
        "dsl_profile": {"uses_options": True, "regular_trade": "none"},
    }
    symbols = args.symbols or DEFAULT_PRELOAD_SYMBOLS.get(strategy_path.name)
    if symbols:
        payload["symbols"] = symbols
    return payload


def run_one(args: argparse.Namespace, strategy_path: Path) -> dict[str, Any]:
    dsl = strategy_path.read_text(encoding="utf-8")
    payload = build_payload(args, strategy_path, dsl)
    accepted = request_json(args.base_url, "/api/v1/backtests/runs", args.api_key, method="POST", payload=payload)
    run_id = accepted.get("run_id")
    if not run_id:
        raise RuntimeError(f"missing run_id in response: {accepted}")

    print(f"[{strategy_path.name}] submitted run_id={run_id}", flush=True)
    deadline = time.monotonic() + args.timeout
    last_message = ""
    while True:
        status = request_json(args.base_url, f"/api/v1/backtests/runs/{run_id}", args.api_key)
        state = status.get("status", "unknown")
        progress = status.get("progress") or {}
        message = progress.get("message") or progress.get("phase") or state
        percent = progress.get("percent")
        progress_line = f"[{strategy_path.name}] {state}"
        if isinstance(percent, (int, float)):
            progress_line += f" {percent:.1f}%"
        if message:
            progress_line += f" - {message}"
        if progress_line != last_message:
            print(progress_line, flush=True)
            last_message = progress_line

        if state in TERMINAL_STATUSES:
            return status
        if time.monotonic() >= deadline:
            raise TimeoutError(f"timed out waiting for {strategy_path.name} run_id={run_id}")
        time.sleep(args.poll_interval)


def print_summary(strategy_path: Path, status: dict[str, Any]) -> None:
    state = status.get("status", "unknown")
    if state != "completed":
        print(f"[{strategy_path.name}] {state}: {status.get('error', '')}", flush=True)
        return

    summaries = ((status.get("result") or {}).get("summaries") or [])
    if not summaries:
        print(f"[{strategy_path.name}] completed with no summaries", flush=True)
        return
    summary = summaries[0]
    print(
        "[{name}] completed: final_equity={equity:.2f} total_return={ret:.4f} "
        "max_drawdown={dd:.4f} trades={trades} report={report}".format(
            name=strategy_path.name,
            equity=float(summary.get("final_equity") or 0),
            ret=float(summary.get("total_return") or 0),
            dd=float(summary.get("max_drawdown") or 0),
            trades=summary.get("total_trades", 0),
            report=summary.get("report_url") or (status.get("result") or {}).get("report_url") or status.get("report_url") or "",
        ),
        flush=True,
    )


def report_url_for(status: dict[str, Any]) -> str:
    summaries = ((status.get("result") or {}).get("summaries") or [])
    if summaries:
        report_url = summaries[0].get("report_url")
        if report_url:
            return str(report_url)
    result_report_url = (status.get("result") or {}).get("report_url")
    if result_report_url:
        return str(result_report_url)
    return str(status.get("report_url") or "")


def download_report(args: argparse.Namespace, strategy_path: Path, status: dict[str, Any]) -> Path | None:
    if args.no_download_reports or not args.reports_dir or status.get("status") != "completed":
        return None

    report_url = report_url_for(status)
    if not report_url:
        return None

    args.reports_dir.mkdir(parents=True, exist_ok=True)
    run_id = status.get("run_id") or "unknown-run"
    output_path = args.reports_dir / f"{strategy_path.stem}_{run_id}.html"
    output_path.write_bytes(request_bytes(args.base_url, report_url, args.api_key))
    print(f"[{strategy_path.name}] downloaded report {output_path}", flush=True)
    return output_path


def main() -> int:
    args = parse_args()
    root = repo_root()
    strategies = args.strategies or [root / rel for rel in DEFAULT_STRATEGIES]
    strategies = [path if path.is_absolute() else root / path for path in strategies]

    missing = [str(path) for path in strategies if not path.exists()]
    if missing:
        print("missing strategy files: " + ", ".join(missing), file=sys.stderr)
        return 2

    results: list[dict[str, Any]] = []
    for strategy_path in strategies:
        try:
            status = run_one(args, strategy_path)
            print_summary(strategy_path, status)
            report_path = download_report(args, strategy_path, status)
            result: dict[str, Any] = {"strategy": str(strategy_path.relative_to(root)), "status": status}
            if report_path:
                result["downloaded_report"] = str(report_path)
            results.append(result)
        except Exception as exc:
            print(f"[{strategy_path.name}] error: {exc}", file=sys.stderr, flush=True)
            results.append({"strategy": str(strategy_path.relative_to(root)), "error": str(exc)})
            break

    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(results, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        print(f"wrote {args.output}", flush=True)

    all_completed = len(results) == len(strategies) and all(
        result.get("status", {}).get("status") == "completed" for result in results
    )
    return 0 if all_completed else 1


if __name__ == "__main__":
    raise SystemExit(main())
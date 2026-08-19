#!/usr/bin/env python3
"""Run the US option ridge DSL independently across a stock universe."""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import sys
import time
from pathlib import Path
from typing import Any

from run_dsl_backtests import request_bytes, request_json


DEFAULT_SYMBOLS = ["AAPL", "MSFT", "GOOG", "AMZN", "NVDA", "META", "TSLA"]
DEFAULT_STRATEGY = Path("pkg/dsl/scripts/strategies/us-option-min-iv-strike-percentiles.toktik")
TERMINAL_STATUSES = {"completed", "failed", "canceled"}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run independent ridge backtests with a maximum concurrency of three.")
    parser.add_argument("--base-url", default="http://127.0.0.1:9010")
    parser.add_argument("--api-key", default=os.getenv("TOKTIK_API_KEY", ""))
    parser.add_argument("--from", dest="from_date", default="2023-01-01")
    parser.add_argument("--to", dest="to_date", default="2025-12-31")
    parser.add_argument("--total-capital", type=float, default=100000)
    parser.add_argument("--workers", type=int, default=3)
    parser.add_argument("--symbols", nargs="+", default=DEFAULT_SYMBOLS)
    parser.add_argument("--strategy", type=Path, default=DEFAULT_STRATEGY)
    parser.add_argument("--chart-symbol", default="SPY")
    parser.add_argument("--params-json", default="{}", help="JSON object merged into each run's dsl_params")
    parser.add_argument("--reports-dir", type=Path, default=Path("reports/backtests/seven-sisters-ridge"))
    parser.add_argument("--timeout", type=float, default=1800)
    parser.add_argument("--poll-interval", type=float, default=2)
    return parser.parse_args()


def run_symbol(args: argparse.Namespace, dsl: str, common_params: dict[str, Any], symbol: str) -> dict[str, Any]:
    capital = args.total_capital / len(args.symbols)
    params = dict(common_params)
    params["Symbol"] = symbol
    payload = {
        "market": "us",
        "instrument": "mixed",
        "asset": symbol,
        "symbols": [symbol],
        "interval": "1d",
        "from": args.from_date,
        "to": args.to_date,
        "capital": capital,
        "dsl": dsl,
        "dsl_params": params,
        "dsl_profile": {"uses_options": True, "regular_trade": "material"},
        "report_chart_market": "us",
        "report_chart_symbol": args.chart_symbol,
        "report_chart_interval": "1d",
    }
    accepted = request_json(args.base_url, "/api/v1/backtests/runs", args.api_key, method="POST", payload=payload)
    run_id = accepted.get("run_id")
    if not run_id:
        raise RuntimeError(f"{symbol}: missing run_id: {accepted}")
    print(f"[{symbol}] submitted run_id={run_id}", flush=True)

    deadline = time.monotonic() + args.timeout
    while True:
        status = request_json(args.base_url, f"/api/v1/backtests/runs/{run_id}", args.api_key)
        state = status.get("status", "unknown")
        if state in TERMINAL_STATUSES:
            break
        if time.monotonic() >= deadline:
            raise TimeoutError(f"{symbol}: timed out waiting for run_id={run_id}")
        time.sleep(args.poll_interval)

    result: dict[str, Any] = {"symbol": symbol, "run_id": run_id, "status": status}
    if status.get("status") == "completed":
        args.reports_dir.mkdir(parents=True, exist_ok=True)
        report_path = args.reports_dir / f"{symbol.lower()}.html"
        report_path.write_bytes(request_bytes(args.base_url, f"/api/v1/backtests/runs/{run_id}/report", args.api_key))
        result["report"] = str(report_path)
    summary = ((status.get("result") or {}).get("summaries") or [{}])[0]
    print(
        f"[{symbol}] {status.get('status')} closed={summary.get('closed_trades', 0)} "
        f"return={float(summary.get('total_return') or 0):.4f} "
        f"sharpe={float(summary.get('sharpe_ratio') or 0):.3f} "
        f"drawdown={float(summary.get('max_drawdown') or 0):.4f}",
        flush=True,
    )
    return result


def main() -> int:
    args = parse_args()
    if args.workers < 1 or args.workers > 3:
        print("--workers must be between 1 and 3", file=sys.stderr)
        return 2
    args.symbols = [symbol.strip().upper() for symbol in args.symbols if symbol.strip()]
    if not args.symbols:
        print("at least one symbol is required", file=sys.stderr)
        return 2
    try:
        common_params = json.loads(args.params_json)
    except json.JSONDecodeError as exc:
        print(f"invalid --params-json: {exc}", file=sys.stderr)
        return 2
    if not isinstance(common_params, dict):
        print("--params-json must decode to an object", file=sys.stderr)
        return 2
    dsl = args.strategy.read_text(encoding="utf-8")

    results: list[dict[str, Any]] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
        futures = {
            executor.submit(run_symbol, args, dsl, common_params, symbol): symbol
            for symbol in args.symbols
        }
        for future in concurrent.futures.as_completed(futures):
            symbol = futures[future]
            try:
                results.append(future.result())
            except Exception as exc:
                print(f"[{symbol}] error: {exc}", file=sys.stderr, flush=True)
                results.append({"symbol": symbol, "error": str(exc)})

    results.sort(key=lambda item: args.symbols.index(item["symbol"]))
    args.reports_dir.mkdir(parents=True, exist_ok=True)
    output_path = args.reports_dir / "results.json"
    output_path.write_text(json.dumps(results, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {output_path}", flush=True)
    return 1 if any(item.get("error") or item.get("status", {}).get("status") != "completed" for item in results) else 0


if __name__ == "__main__":
    raise SystemExit(main())
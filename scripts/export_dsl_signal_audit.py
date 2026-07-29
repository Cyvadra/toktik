#!/usr/bin/env python3
"""Export trace.emit signal events from a DSL backtest status JSON to CSV."""

from __future__ import annotations

import argparse
import csv
import json
from collections import defaultdict
from pathlib import Path
from typing import Any


FIELD_NAMES = [
    "bar_index",
    "symbol",
    "turnover_score",
    "valuation_percentile",
    "iv_percentile",
    "hv_percentile",
    "rsi",
    "cci",
    "matched_strategies",
    "final_action",
    "rejection_reason",
]

TRACE_PREFIX = "dsl.trace.signal_"
TRACE_TRUNCATED_CODE = "dsl.trace.truncated"


def parse_reason(reason: str) -> dict[str, str]:
    fields: dict[str, str] = {}
    for part in reason.split(";"):
        key, separator, value = part.partition("=")
        if not separator or not key or not value:
            raise ValueError(f"invalid signal trace reason: {reason!r}")
        if key in fields:
            raise ValueError(f"duplicate signal trace field {key!r}: {reason!r}")
        fields[key] = value
    return fields


def parse_message(message: str) -> tuple[str, str]:
    stage_and_symbol, separator, reason = message.partition(": ")
    if not separator:
        raise ValueError(f"invalid signal trace message: {message!r}")
    _, separator, symbol = stage_and_symbol.partition(" ")
    if not separator or not symbol:
        raise ValueError(f"invalid signal trace message: {message!r}")
    return symbol, reason


def warnings_from_statuses(payload: list[dict[str, Any]]) -> list[dict[str, Any]]:
    warnings: list[dict[str, Any]] = []
    for item in payload:
        status = item.get("status") or {}
        summaries = ((status.get("result") or {}).get("summaries") or [])
        for summary in summaries:
            warnings.extend(summary.get("warnings") or [])
    return warnings


def build_rows(payload: list[dict[str, Any]]) -> list[dict[str, str | int]]:
    rows: dict[tuple[int, str], dict[str, str | int]] = {}
    opens_by_bar: dict[int, int] = defaultdict(int)

    for warning in warnings_from_statuses(payload):
        code = str(warning.get("code") or "")
        if code == TRACE_TRUNCATED_CODE:
            raise ValueError("signal trace is incomplete because trace diagnostics were truncated")
        if not code.startswith(TRACE_PREFIX):
            continue
        bar_index = warning.get("bar_index")
        if not isinstance(bar_index, int):
            raise ValueError(f"signal trace has no integer bar_index: {warning!r}")
        stage = code.removeprefix("dsl.trace.")
        symbol, reason = parse_message(str(warning.get("message") or ""))
        fields = parse_reason(reason)
        key = (bar_index, symbol)
        row = rows.setdefault(key, {"bar_index": bar_index, "symbol": symbol})

        if stage == "signal_input":
            row.update(
                {
                    "turnover_score": fields.get("turnover", ""),
                    "valuation_percentile": fields.get("valuation", ""),
                    "iv_percentile": fields.get("ivp", ""),
                    "hv_percentile": fields.get("hvp", ""),
                    "rsi": fields.get("rsi", ""),
                    "cci": fields.get("cci", ""),
                }
            )
        elif stage == "signal_match":
            row["matched_strategies"] = fields.get("strategies", "")
        elif stage == "signal_open":
            opens_by_bar[bar_index] += 1
            row["final_action"] = fields.get("strategy", "")
            row["rejection_reason"] = ""
        elif stage == "signal_reject":
            if not row.get("final_action"):
                row["rejection_reason"] = fields.get("reason", "")
        else:
            raise ValueError(f"unsupported signal trace stage: {stage!r}")

    errors: list[str] = []
    for bar_index, count in opens_by_bar.items():
        if count > 1:
            errors.append(f"bar {bar_index} has {count} signal_open events")
    for row in rows.values():
        action = str(row.get("final_action") or "")
        if not action:
            continue
        missing = [field for field in ("turnover_score", "valuation_percentile", "iv_percentile", "hv_percentile", "matched_strategies") if not row.get(field)]
        if missing:
            errors.append(f"bar {row['bar_index']} {row['symbol']} open lacks {', '.join(missing)}")
        strategies = str(row.get("matched_strategies") or "").split("|")
        if action not in strategies:
            errors.append(f"bar {row['bar_index']} {row['symbol']} opened {action!r} outside matched strategies")
    if errors:
        raise ValueError("; ".join(errors))

    return sorted(rows.values(), key=lambda row: (int(row["bar_index"]), str(row["symbol"])))


def export_audit(input_path: Path, output_path: Path) -> int:
    payload = json.loads(input_path.read_text(encoding="utf-8"))
    if not isinstance(payload, list):
        raise ValueError("backtest status JSON must be an array")
    rows = build_rows(payload)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", newline="", encoding="utf-8") as handle:
        writer = csv.DictWriter(handle, fieldnames=FIELD_NAMES, extrasaction="ignore")
        writer.writeheader()
        for row in rows:
            writer.writerow(row)
    return len(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Export DSL signal trace events to CSV.")
    parser.add_argument("input", type=Path, help="JSON written by run_dsl_backtests.py --output")
    parser.add_argument("output", type=Path, help="CSV audit output path")
    args = parser.parse_args()
    try:
        count = export_audit(args.input, args.output)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        parser.error(str(exc))
    print(f"wrote {args.output} ({count} signal rows)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
#!/usr/bin/env python3
import argparse
import json
from collections import defaultdict
from pathlib import Path


def load_group_data(path: Path) -> dict[int, dict]:
    with path.open() as fh:
        data = json.load(fh)

    group_data = defaultdict(lambda: {"pnl": 0.0, "reasons": set(), "spreads": 0})
    for spread in data.get("spread_positions", []):
        group_id = spread.get("group_id", 0)
        group_data[group_id]["pnl"] += spread.get("realized_pnl", 0.0) or 0.0
        group_data[group_id]["spreads"] += 1
        for leg in spread.get("legs", []):
            reason = leg.get("close_reason", "")
            if reason:
                group_data[group_id]["reasons"].add(reason)
    return group_data


def main() -> None:
    parser = argparse.ArgumentParser(description="Compare per-group realized PnL between two backtest JSON reports.")
    parser.add_argument("baseline", help="Baseline backtest JSON report")
    parser.add_argument("candidate", help="Candidate backtest JSON report")
    args = parser.parse_args()

    baseline_path = Path(args.baseline)
    candidate_path = Path(args.candidate)
    baseline = load_group_data(baseline_path)
    candidate = load_group_data(candidate_path)

    label_left = baseline_path.stem[:18]
    label_right = candidate_path.stem[:18]
    print(
        f"{'group':>5} | {label_left:>18} | {label_right:>18} | {'delta':>10} | candidate_close_reasons"
    )
    print("-" * 110)

    group_ids = sorted(set(baseline) | set(candidate))
    total_left = 0.0
    total_right = 0.0
    for group_id in group_ids:
        left_pnl = baseline[group_id]["pnl"]
        right_pnl = candidate[group_id]["pnl"]
        delta = right_pnl - left_pnl
        total_left += left_pnl
        total_right += right_pnl
        reasons = ", ".join(sorted(candidate[group_id]["reasons"]))
        print(
            f"{group_id:>5} | {left_pnl:>+18.4f} | {right_pnl:>+18.4f} | {delta:>+10.4f} | {reasons}"
        )

    print("-" * 110)
    print(
        f"{'TOTAL':>5} | {total_left:>+18.4f} | {total_right:>+18.4f} | {total_right - total_left:>+10.4f}"
    )


if __name__ == "__main__":
    main()

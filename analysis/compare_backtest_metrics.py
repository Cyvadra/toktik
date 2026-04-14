#!/usr/bin/env python3
import argparse
import json
from pathlib import Path


def load_metrics(path: Path) -> dict:
    with path.open() as fh:
        data = json.load(fh)
    return {
        "name": path.stem,
        "final_equity": data.get("final_equity", 0.0),
        "total_return": data.get("total_return", 0.0),
        "sharpe_ratio": data.get("sharpe_ratio", 0.0),
        "calmar_ratio": data.get("calmar_ratio", 0.0),
        "max_drawdown": data.get("max_drawdown", 0.0),
        "spread_pnl": data.get("spread_summary", {}).get("total_pnl", 0.0),
        "winning_spreads": data.get("spread_summary", {}).get("winning_spreads", 0),
        "losing_spreads": data.get("spread_summary", {}).get("losing_spreads", 0),
        "win_rate": data.get("spread_summary", {}).get("win_rate", 0.0),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Compare top-level metrics across backtest JSON reports.")
    parser.add_argument("reports", nargs="+", help="One or more backtest JSON report paths")
    args = parser.parse_args()

    metrics = [load_metrics(Path(report)) for report in args.reports]

    header = (
        f"{'report':<36} {'spread_pnl':>12} {'return%':>10} {'max_dd%':>10} "
        f"{'sharpe':>8} {'calmar':>8} {'wins':>6} {'losses':>7} {'win_rate':>10}"
    )
    print(header)
    print("-" * len(header))
    for item in metrics:
        print(
            f"{item['name']:<36} "
            f"{item['spread_pnl']:>12.4f} "
            f"{item['total_return'] * 100:>9.2f}% "
            f"{item['max_drawdown'] * 100:>9.2f}% "
            f"{item['sharpe_ratio']:>8.2f} "
            f"{item['calmar_ratio']:>8.2f} "
            f"{item['winning_spreads']:>6d} "
            f"{item['losing_spreads']:>7d} "
            f"{item['win_rate'] * 100:>9.2f}%"
        )


if __name__ == "__main__":
    main()

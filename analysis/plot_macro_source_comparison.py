#!/usr/bin/env python3
import argparse
import csv
import subprocess
import sys
from dataclasses import dataclass
from datetime import date, datetime, timezone
from pathlib import Path

import matplotlib.pyplot as plt


FACTORS = ("pe10", "sp500", "earnings")


@dataclass
class SeriesRow:
    month: str
    values: dict[str, float]


def first_day_of_month(value: date) -> date:
    return value.replace(day=1)


def default_window(months: int) -> tuple[date, date]:
    today = datetime.now(timezone.utc).date()
    end = first_day_of_month(today)
    start_month = end.month - months
    start_year = end.year
    while start_month <= 0:
        start_month += 12
        start_year -= 1
    start = date(start_year, start_month, 1)
    return start, end


def run_command(command: list[str], cwd: Path) -> str:
    result = subprocess.run(command, cwd=cwd, check=True, capture_output=True, text=True)
    return result.stdout


def fetch_guru_series(repo_root: Path, start: date, end: date) -> list[SeriesRow]:
    sql = f"""
SELECT
    formatDateTime(toStartOfMonth(event_ts), '%Y-%m-01') AS month,
    factor_code,
    argMax(value, known_at) AS value
FROM macro_observation
WHERE dataset = 'gurufocus-shiller'
  AND source = 'gurufocus'
    AND reference_symbol = 'SPY'
  AND factor_code IN ('pe10', 'sp500', 'earnings')
  AND event_ts >= toDateTime('{start.isoformat()} 00:00:00', 'UTC')
  AND event_ts < toDateTime('{end.isoformat()} 00:00:00', 'UTC')
GROUP BY month, factor_code
ORDER BY month, factor_code
""".strip()
    output = run_command(["clickhouse-client", "--format", "CSVWithNames", "-q", sql], repo_root)
    reader = csv.DictReader(output.splitlines())
    by_month: dict[str, dict[str, float]] = {}
    for row in reader:
        month = row["month"]
        factor = row["factor_code"]
        by_month.setdefault(month, {})[factor] = float(row["value"])
    return [SeriesRow(month=month, values=values) for month, values in sorted(by_month.items())]


def generate_fmp_csv(repo_root: Path, start: date, end: date, csv_path: Path, live_csv_path: Path) -> str:
    csv_path.parent.mkdir(parents=True, exist_ok=True)
    command = [
        "go",
        "run",
        "./cmd/us-market-macro-fmp-sync",
        "--from",
        start.isoformat(),
        "--to",
        end.isoformat(),
        "--dry-run",
        "--debug-csv",
        str(csv_path),
        "--debug-live-csv",
        str(live_csv_path),
    ]
    result = subprocess.run(command, cwd=repo_root, check=True, capture_output=True, text=True)
    return (result.stdout + "\n" + result.stderr).strip()


def load_fmp_series(csv_path: Path) -> list[SeriesRow]:
    with csv_path.open(newline="") as handle:
        reader = csv.DictReader(handle)
        rows = []
        for row in reader:
            rows.append(
                SeriesRow(
                    month=row["month"],
                    values={factor: float(row[factor]) for factor in FACTORS if row.get(factor)},
                )
            )
    return rows


def write_merged_csv(output_path: Path, guru_rows: list[SeriesRow], fmp_rows: list[SeriesRow]) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    guru_map = {row.month: row.values for row in guru_rows}
    fmp_map = {row.month: row.values for row in fmp_rows}
    months = sorted(set(guru_map) | set(fmp_map))
    with output_path.open("w", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow([
            "month",
            "guru_pe10",
            "fmp_pe10",
            "guru_sp500",
            "fmp_sp500",
            "guru_earnings",
            "fmp_earnings",
        ])
        for month in months:
            writer.writerow([
                month,
                guru_map.get(month, {}).get("pe10", ""),
                fmp_map.get(month, {}).get("pe10", ""),
                guru_map.get(month, {}).get("sp500", ""),
                fmp_map.get(month, {}).get("sp500", ""),
                guru_map.get(month, {}).get("earnings", ""),
                fmp_map.get(month, {}).get("earnings", ""),
            ])


def plot_series(output_path: Path, guru_rows: list[SeriesRow], fmp_rows: list[SeriesRow], start: date, end: date) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    guru_map = {row.month: row.values for row in guru_rows}
    fmp_map = {row.month: row.values for row in fmp_rows}
    months = sorted(set(guru_map) | set(fmp_map))

    figure, axes = plt.subplots(3, 1, figsize=(12, 10), sharex=True)
    for axis, factor in zip(axes, FACTORS):
        guru_values = [guru_map.get(month, {}).get(factor) for month in months]
        fmp_values = [fmp_map.get(month, {}).get(factor) for month in months]
        axis.plot(months, guru_values, marker="o", linewidth=2, label="Guru")
        axis.plot(months, fmp_values, marker="s", linewidth=2, label="FMP")
        axis.set_ylabel(factor)
        axis.grid(True, alpha=0.3)
        axis.legend(loc="best")

    axes[-1].set_xlabel("month")
    figure.suptitle(f"Guru vs FMP macro series ({start.isoformat()} to {end.isoformat()})")
    figure.autofmt_xdate(rotation=30)
    figure.tight_layout()
    figure.savefig(output_path, dpi=180, bbox_inches="tight")
    plt.close(figure)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Plot Guru and FMP macro series for the last N months.")
    parser.add_argument("--months", type=int, default=12, help="Number of trailing calendar months to compare")
    parser.add_argument("--repo-root", default=".", help="Repository root path")
    parser.add_argument("--fmp-csv", default="tmp/fmp_shiller_last_12m.csv", help="Path for generated FMP monthly CSV")
    parser.add_argument("--fmp-live-csv", default="tmp/fmp_shiller_live_last_12m.csv", help="Path for generated FMP live CSV")
    parser.add_argument("--merged-csv", default="tmp/guru_vs_fmp_last_12m.csv", help="Path for merged comparison CSV")
    parser.add_argument("--plot", default="tmp/guru_vs_fmp_last_12m.png", help="Path for output plot PNG")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repo_root = Path(args.repo_root).resolve()
    start, end = default_window(args.months)

    guru_rows = fetch_guru_series(repo_root, start, end)
    if not guru_rows:
        raise SystemExit("No Guru rows returned for the selected range")

    fmp_log = generate_fmp_csv(repo_root, start, end, Path(args.fmp_csv), Path(args.fmp_live_csv))
    fmp_rows = load_fmp_series(Path(args.fmp_csv))
    if not fmp_rows:
        raise SystemExit("No FMP rows returned for the selected range")

    write_merged_csv(Path(args.merged_csv), guru_rows, fmp_rows)
    plot_series(Path(args.plot), guru_rows, fmp_rows, start, end)

    print(f"window: {start.isoformat()} -> {end.isoformat()}")
    print(f"guru months: {len(guru_rows)}")
    print(f"fmp months: {len(fmp_rows)}")
    print(f"merged csv: {Path(args.merged_csv)}")
    print(f"plot: {Path(args.plot)}")
    if fmp_log:
        print("fmp log:")
        print(fmp_log.splitlines()[-1])
    return 0


if __name__ == "__main__":
    sys.exit(main())
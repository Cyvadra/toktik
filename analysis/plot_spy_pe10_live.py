#!/usr/bin/env python3
import argparse
import csv
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path

import matplotlib.pyplot as plt


@dataclass
class PriceRow:
	timestamp: datetime
	close: float


@dataclass
class MacroObservation:
	event_ts: datetime
	known_at: datetime
	value: float
	anchor_value: float


def run_clickhouse(repo_root: Path, sql: str) -> str:
	result = subprocess.run(
		["clickhouse-client", "--format", "CSVWithNames", "-q", sql],
		cwd=repo_root,
		check=True,
		capture_output=True,
		text=True,
	)
	return result.stdout


def parse_timestamp(value: str) -> datetime:
	return datetime.fromisoformat(value.replace(" ", "T")).replace(tzinfo=timezone.utc)


def fetch_prices(repo_root: Path, symbol: str, trading_days: int) -> list[PriceRow]:
	lookback_days = max(500, trading_days * 2)
	sql = f"""
SELECT
	toString(timestamp) AS ts,
    toFloat64(close) AS close
FROM us_stocks_bar_1d
WHERE symbol = '{symbol}'
  AND timestamp >= today() - {lookback_days}
ORDER BY timestamp ASC
""".strip()
	output = run_clickhouse(repo_root, sql)
	reader = csv.DictReader(output.splitlines())
	rows = [PriceRow(timestamp=parse_timestamp(row["ts"]), close=float(row["close"])) for row in reader]
	if len(rows) < trading_days:
		raise SystemExit(f"not enough {symbol} price rows returned: {len(rows)}")
	return rows[-trading_days:]


def fetch_macro_observations(repo_root: Path, trading_days: int) -> list[MacroObservation]:
	lookback_days = max(700, trading_days * 3)
	sql = f"""
SELECT
	toString(event_ts) AS event_ts_text,
	toString(known_at) AS known_at_text,
    toFloat64(value) AS value,
    toFloat64(anchor_value) AS anchor_value
FROM macro_observation
WHERE dataset = 'gurufocus-shiller'
  AND factor_code = 'pe10'
  AND known_at <= now()
  AND event_ts >= today() - {lookback_days}
ORDER BY event_ts ASC, known_at ASC, revision ASC
""".strip()
	output = run_clickhouse(repo_root, sql)
	reader = csv.DictReader(output.splitlines())
	rows = [
		MacroObservation(
			event_ts=parse_timestamp(row["event_ts_text"]),
			known_at=parse_timestamp(row["known_at_text"]),
			value=float(row["value"]),
			anchor_value=float(row["anchor_value"]),
		)
		for row in reader
	]
	if not rows:
		raise SystemExit("no gurufocus pe10 observations returned")
	return rows


def collapse_observations(rows: list[MacroObservation]) -> list[MacroObservation]:
	collapsed: list[MacroObservation] = []
	for row in rows:
		if collapsed and collapsed[-1].event_ts == row.event_ts:
			collapsed[-1] = row
			continue
		collapsed.append(row)
	return collapsed


def build_daily_series(
	spy_rows: list[PriceRow],
	observations: list[MacroObservation],
) -> list[dict[str, object]]:
	spy_anchor_by_date = {row.timestamp.date(): row.close for row in spy_rows}
	series_index = 0
	out: list[dict[str, object]] = []
	for spy_row in spy_rows:
		day_end = spy_row.timestamp + timedelta(days=1)
		for next_index in range(series_index + 1, len(observations)):
			if observations[next_index].known_at >= day_end:
				break
			series_index = next_index
		current = observations[series_index]
		if current.known_at >= day_end:
			continue
		spy_anchor = spy_anchor_by_date.get(current.event_ts.date())
		if spy_anchor is None or spy_anchor == 0:
			continue
		broken_value = current.value * (spy_row.close / current.anchor_value)
		corrected_value = current.value * (spy_row.close / spy_anchor)
		out.append(
			{
				"date": spy_row.timestamp.date().isoformat(),
				"event_ts": current.event_ts.isoformat(),
				"known_at": current.known_at.isoformat(),
				"spy_close": spy_row.close,
				"spy_anchor_close": spy_anchor,
				"broken_spy_reference": broken_value,
				"corrected_spy_anchor": corrected_value,
			}
		)
	return out


def write_csv(path: Path, rows: list[dict[str, object]]) -> None:
	path.parent.mkdir(parents=True, exist_ok=True)
	with path.open("w", newline="") as handle:
		writer = csv.DictWriter(
			handle,
			fieldnames=[
				"date",
				"event_ts",
				"known_at",
				"spy_close",
				"spy_anchor_close",
				"broken_spy_reference",
				"corrected_spy_anchor",
			],
		)
		writer.writeheader()
		writer.writerows(rows)


def plot_rows(path: Path, rows: list[dict[str, object]]) -> None:
	path.parent.mkdir(parents=True, exist_ok=True)
	dates = [row["date"] for row in rows]
	broken = [float(row["broken_spy_reference"]) for row in rows]
	corrected = [float(row["corrected_spy_anchor"]) for row in rows]

	figure, axis = plt.subplots(figsize=(13, 6))
	axis.plot(dates, corrected, linewidth=2.2, color="#0b6e4f", label="Corrected (SPY self-anchor)")
	axis.plot(dates, broken, linewidth=1.8, color="#c1121f", linestyle="--", label="Broken (raw SPY reference)")
	axis.set_title("SPY page pe10_live over the last 300 trading days")
	axis.set_ylabel("pe10_live")
	axis.set_xlabel("date")
	axis.grid(True, alpha=0.25)
	axis.legend(loc="best")
	for tick in axis.get_xticklabels():
		tick.set_rotation(30)
	figure.tight_layout()
	figure.savefig(path, dpi=180, bbox_inches="tight")
	plt.close(figure)


def parse_args() -> argparse.Namespace:
	parser = argparse.ArgumentParser(description="Plot SPY pe10_live for the last N trading days.")
	parser.add_argument("--repo-root", default=".", help="Repository root")
	parser.add_argument("--trading-days", type=int, default=300, help="Number of trailing trading days")
	parser.add_argument("--csv", default="tmp/spy_pe10_live_300d.csv", help="Output CSV path")
	parser.add_argument("--plot", default="tmp/spy_pe10_live_300d.png", help="Output PNG path")
	return parser.parse_args()


def main() -> int:
	args = parse_args()
	repo_root = Path(args.repo_root).resolve()
	spy_rows = fetch_prices(repo_root, "SPY", args.trading_days)
	observations = collapse_observations(fetch_macro_observations(repo_root, args.trading_days))
	rows = build_daily_series(spy_rows, observations)
	if not rows:
		raise SystemExit("no daily pe10_live rows were produced")

	write_csv(Path(args.csv), rows)
	plot_rows(Path(args.plot), rows)

	last = rows[-1]
	print(f"rows: {len(rows)}")
	print(f"csv: {Path(args.csv)}")
	print(f"plot: {Path(args.plot)}")
	print(
		"latest: "
		f"date={last['date']} "
		f"broken={float(last['broken_spy_reference']):.4f} "
		f"corrected={float(last['corrected_spy_anchor']):.4f}"
	)
	return 0


if __name__ == "__main__":
	sys.exit(main())
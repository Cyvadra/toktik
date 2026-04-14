#!/usr/bin/env python3
import argparse
import csv
from datetime import datetime
from pathlib import Path


def parse_ts(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def main() -> None:
    parser = argparse.ArgumentParser(description="Inspect when conditionFallExtra fired in a trade CSV report.")
    parser.add_argument("trade_csv", help="Path to trade CSV output")
    parser.add_argument("--note", default="conditionFallExtra", help="Close note to inspect")
    args = parser.parse_args()

    path = Path(args.trade_csv)
    with path.open() as fh:
        rows = list(csv.DictReader(fh))

    group_ids = sorted({int(row["grp"]) for row in rows if row.get("grp")})
    for group_id in group_ids:
        trades = [row for row in rows if int(row["grp"]) == group_id]
        opens = [row for row in trades if row["kind"] == "option_open"]
        note_closes = [row for row in trades if row.get("note", "") == args.note]
        if not opens:
            continue

        open_ts = parse_ts(opens[0]["ts"])
        if note_closes:
            first_close_ts = parse_ts(note_closes[0]["ts"])
            delay_hours = (first_close_ts - open_ts).total_seconds() / 3600.0
            note_pnl = sum(float(row["pnl"] or 0.0) for row in note_closes)
            when = note_closes[0]["ts"][:16]
            delay = f"{delay_hours:.1f}h"
        else:
            when = "never"
            delay = "N/A"
            note_pnl = 0.0

        print(
            f"group={group_id:>2} open={opens[0]['ts'][:16]} note={when:>16} delay={delay:>6} pnl={note_pnl:+.4f}"
        )


if __name__ == "__main__":
    main()

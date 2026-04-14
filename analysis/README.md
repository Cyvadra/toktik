# Analysis Scripts

This directory contains reusable analysis helpers for backtest result comparison.

## Scripts

### `compare_backtest_metrics.py`

Compare top-level metrics across one or more backtest JSON reports.

Usage:

```bash
python3 analysis/compare_backtest_metrics.py \
  tmp/backtest-version-compare/results/retracement_long_v3_pnl_gated.json \
  tmp/backtest-version-compare/results/retracement_long_v9i.json
```

Outputs:

- `spread_pnl`
- `return%`
- `max_dd%`
- `sharpe`
- `calmar`
- spread win/loss counts
- spread win rate

### `compare_group_pnl.py`

Compare realized PnL by `group_id` between two backtest JSON reports.

Usage:

```bash
python3 analysis/compare_group_pnl.py \
  tmp/backtest-version-compare/results/retracement_long_v3_pnl_gated.json \
  tmp/backtest-version-compare/results/retracement_long_v9i.json
```

Outputs:

- per-group realized PnL delta
- candidate close reasons
- total realized PnL delta

Useful for finding which trade groups improved or degraded after a strategy change.

### `check_extra_reduction_timing.py`

Inspect when a specific close note first fired in a trade CSV, relative to group open time.

Usage:

```bash
python3 analysis/check_extra_reduction_timing.py \
  tmp/backtest-version-compare/results/retracement_long_v9_trailing_lock_trades.csv
```

Custom note example:

```bash
python3 analysis/check_extra_reduction_timing.py \
  tmp/backtest-version-compare/results/retracement_long_v9_trailing_lock_trades.csv \
  --note trailing_profit_lock
```

Outputs:

- group open time
- first note trigger time
- delay in hours
- aggregated PnL for rows with that note

## Typical Workflow

1. Run a new backtest and save the JSON and trade CSV.
2. Use `compare_backtest_metrics.py` for headline performance comparison.
3. Use `compare_group_pnl.py` to locate which groups drove the change.
4. Use `check_extra_reduction_timing.py` to verify event timing assumptions.

## Notes

- These scripts assume the current toktik backtest JSON structure, especially `spread_positions`, `spread_summary`, and trade CSV columns such as `grp`, `kind`, `note`, and `pnl`.
- They are intended for fast iterative strategy tuning, not for general-purpose reporting.

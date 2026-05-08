# Forex Market FMP Sync

This command mirrors the existing US-market FMP kline sync flow, but targets a
dedicated forex market domain.

## Storage

- Schema file: `schema/clickhouse/forex_market.sql`
- Base table: `forex_bar_1m`
- Structure: aligned with `us_stocks_bar_1m`

## Command

```bash
go run ./cmd/forex-market-fmp-klines-sync \
  --start-date 2026-05-01 \
  --end-date 2026-05-06 \
  --dry-run
```

## Symbol Resolution

Priority order:

1. `--symbols EURUSD,USDJPY,XAUUSD`
2. `--symbols-file path/to/file.txt`
3. default watchlist at `signal-list/forex-fmp-watchlist.txt`

## Notes

- FMP forex intraday timestamps are treated as UTC.
- Session metadata uses a simple 24x5 model with UTC calendar days.
- Saturday bars, if ever returned by a provider, are marked as `closed`.
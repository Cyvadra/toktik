# US Market Symbol Gap Notes

Current findings from the local ClickHouse `1d` views:

- `us_options_bar_1d` distinct underlyings: `7742`
- `us_stocks_bar_1d` distinct symbols: `18718`
- Raw missing underlyings present in options but absent in stocks: `158`
- Dotted-share fallback mapped in stocks already: `10`
- Still missing after fallback normalization: `148`

Observed gap patterns:

- Some OPRA underlyings are share-class aliases that need dot normalization before comparing to stock symbols, for example `BRKB -> BRK.B` and `CWENA -> CWEN.A`.
- Some raw-missing symbols are likely index-style or volatility-style underlyings, such as `SPX`, `XSP`, `VIX`, `RUT`, `NDX`.
- Some are delisted, OTC, warrant, or test-like symbols, such as `BBBYQ`, `SAVEQ`, `CBTXW`, `ZTEST`.

Current full FMP coverage results for the `148` still-missing symbols:

- Any FMP coverage: `89`
- Resolved via direct stock-like symbol: `52`
- Resolved via FMP search suggestions: `22`
- Resolved via index candidates: `15`
- No FMP coverage after direct/search/index attempts: `59`

No-coverage breakdown:

- `20` delisted-or-bankruptcy OTC symbols
- `5` warrant-or-SPAC-derivative symbols
- `5` foreign ADR / OTC-style symbols
- `15` custom-or-unsupported index symbols
- `11` test-or-placeholder symbols
- `3` other unresolved symbols

Current FMP stock sync behavior:

- Existing sync command: `cmd/us-market-fmp-klines-sync/main.go`
- If `--symbols` is empty, it now resolves logical sync targets from stored stock symbols plus deterministic option-underlying gap mappings.
- Gap mappings are limited to code-reviewed direct and index alias cases.
- Dotted share-class fallback mappings remain excluded from the FMP intraday sync path because runtime validation hit FMP `402 Premium Query Parameter` responses on symbols such as `BRK.B`.
- Resolver implementation: `internal/usmarket/fmp_sync_targets.go`, `ResolveUSStockSyncTargets(...)`
- Fetches can use an alias such as `^GSPC`, but inserted rows still keep the options `underlying` as the stored stock symbol.

New audit command:

```bash
go run ./cmd/us-market-symbol-gap-report \
  --clickhouse-dsn "clickhouse://default:@localhost:9000/default" \
  --output-dir docs \
  --limit 25
```

What it writes:

- `docs/us-stocks-symbols-1d.txt`
- `docs/us-options-underlyings-1d.txt`
- `docs/us-options-missing-stock-underlyings-1d.txt`
- `docs/us-options-missing-stock-underlyings-fallback-mapped-1d.txt`
- `docs/us-options-still-missing-stock-underlyings-1d.txt`
- `docs/us-options-missing-stock-underlyings-fmp-coverage.csv`
- `docs/us-options-no-fmp-coverage-categories.md`
- `docs/us-market-symbol-gap-report.md`
- `docs/us-market-fmp-sync-gap-resolution.md`
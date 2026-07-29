# Daily Picks Core DSL Strategy

`pkg/dsl/scripts/strategies/daily-picks-core.toktik` applies the deterministic
`daily-picks-dsl` decision path only to `SPY`, `QQQ`, and `NVDA`.

## Behavior

The strategy preserves the daily-picks candidate ranking, low-IV overrides,
value strategy matrix, option eligibility checks, ordered leg-building
attempts, spread-reference lifecycle, and one-successful-open-per-bar limit.
It does not use the index fallback rules from `index-options-dsl`.

The fixed symbols are ranked each bar by daily `close * volume`. A symbol is
eligible only when its price, volume, valuation percentile, volatility
percentiles, RSI(14), and CCI(20) are available. Missing data causes that
symbol to be skipped; the strategy does not substitute constants or another
symbol's valuation.

## Valuation sources

| Symbol | DSL factor | Source |
| --- | --- | --- |
| `SPY` | `pe10_live` | `fmp-sp500-shiller`, a live daily series based on monthly S&P 500 Shiller-style anchors |
| `QQQ` | `pe` | `fmp-nasdaq100-shiller`, the aggregated trailing PE of Nasdaq-100 constituents |
| `NVDA` | `pe` and `pb` | Point-in-time US stock fundamentals; available PE/PB percentiles are averaged |

All valuation series use `ta.percentrank_valid(..., 630, 200)`. QQQ deliberately
uses the existing Nasdaq-100 aggregate trailing PE mapping, not QQQ fund-level
financial statements and not a `pe10_live` alias.

## Data prerequisites

The relevant data-sync jobs are:

- `fmp_sp500_macro` for SPY valuation;
- `fmp_nasdaq100_macro` for QQQ valuation;
- `fmp_us_fundamentals` for NVDA PE/PB;
- `polygon_us_flatfiles`, `polygon_us_greeks`, and the feature-store backfill
  for daily prices, option chains, Greeks, HV30, and IV percentile.

Before selecting a backtest range, check the common coverage of the three
valuation series, daily OHLCV, volatility features, and US option chains. The
backtest should start no earlier than their latest common first date.

## Validation and backtest

Validate catalog parsing and static dependencies:

```bash
go test ./pkg/dsl/catalog
go test ./pkg/dsl/analysis ./pkg/dsl/bridge ./internal/service
```

Run the same backtest and signal-audit workflow used by daily picks:

```bash
python3 scripts/run_dsl_backtests.py \
  --from YYYY-MM-DD --to YYYY-MM-DD \
  --output tmp/daily-picks-core-backtest.json \
  --signal-audit tmp/daily-picks-core-signal-audit.csv \
  pkg/dsl/scripts/strategies/daily-picks-core.toktik
```

The signal audit should contain no more than one `signal_open` per bar and
should show the selected symbol's turnover, valuation percentile, IV
percentile, HV percentile, RSI, CCI, ordered strategy matches, and rejection
reason where applicable.
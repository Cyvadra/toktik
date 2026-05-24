# US Stock Split Adjustments

US stock prices returned by database-backed services are front-adjusted for stock splits by default.

## Data Source

Split events are fetched from FMP `GET /stable/splits?symbol={STOCK_SYMBOL}` and stored in ClickHouse table `us_stock_splits`.

The split sync is registered as `fmp_us_stock_splits` in `data-sync-pipeline` and is configured with a 3-day overlap. Each sync refreshes split events per symbol and writes rows idempotently using `(symbol, split_date)` replacement semantics.

## Price Basis

Raw OHLC bars remain unchanged in `us_stocks_bar_1m` and the precomputed `us_stocks_bar_*` aggregate tables. Query services apply the split factor at read time:

```text
price_factor = product(denominator / numerator for split_date > bar_date)
adjusted_price = raw_price * price_factor
```

For example, a 2-for-1 split has `numerator=2` and `denominator=1`, so prices before the split are multiplied by `0.5`.

## Affected Consumers

The following US stock price consumers use front-adjusted OHLC/close prices:

- US stock bars API and technical indicators built on it.
- PE/PB price-derived calculations in US stock fundamental enrichment.
- Macro reference price expansion.
- Feature-store underlying price history for US options.
- US turnover screener stock turnover calculations.
- US underlying datafeed used by backtests.

Volume and transactions remain raw for now. Option contract prices are not adjusted; only US stock underlying price reads use split adjustment.

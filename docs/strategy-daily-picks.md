# Daily Picks DSL Strategy

`pkg/dsl/scripts/strategies/daily-picks.toktik` is a deterministic migration of the `options-today/backend` top-pick recommendation path. It intentionally excludes all LLM calls, AI scores, recommendation scores, and narrative generation.

## Source behavior

The source pipeline builds documented-policy recommendations before the `top-pick-llm` enrichment step:

1. The recommendation engine calls `resolveDocumentedStrategyCandidates` for eligible underlyings.
2. The critic removes blocked recommendations.
3. The top-pick branch takes non-AI, non-index recommendations from the remaining pool.
4. `top-pick-llm` keeps the upstream strategy type, selects option legs, validates topology, and applies AI scoring.

The DSL migrates the deterministic strategy matching and option eligibility behavior. It does not migrate LLM leg selection, recommendation scoring, critic numeric scoring, narratives, or homepage ranking.

## Candidate universe and ordering

The strategy reads the `daily_picks` point-in-time universe. This universe is built from the top 20 non-ETF US underlyings by combined stock and option turnover over a 20-day lookback.

Within each bar, qualified candidates are ordered by the executable stock-turnover proxy `close * volume` in descending order. At most 20 candidates are retained.

Dedicated index proxies `SPY`, `QQQ`, and `IBIT` are excluded.

## Required inputs

Each candidate requires:

- positive daily close and volume;
- a valid PE or PB percentile;
- a valid combined valuation percentile;
- valid IV and HV percentiles;
- valid RSI(14) and CCI(20);
- a non-empty extended option chain;
- at least one contract passing the configured DTE, absolute delta, and minimum-premium filters.

Default option filters are 10-90 DTE, absolute delta 0.05-0.60, and minimum bid premium 1.

## Strategy matching

Extreme low-IV overrides are evaluated first:

| Condition | Strategy |
| --- | --- |
| IV percentile <= 30 and valuation percentile >= 80 | `BUY_PUT` |
| IV percentile <= 30 and valuation percentile < 80 | `BUY_STRADDLE` |

The documented value matrix is then appended without duplicates:

| Valuation | IV | HV | Strategies |
| --- | --- | --- | --- |
| Undervalued | High | Any | `SELL_PUT`, `COVERED_CALL` |
| Undervalued | Low | High | `BUY_SKEWED_STRADDLE` |
| Undervalued | Low | Not high | `BUY_CALL` |
| Overvalued | Low | Any | `BUY_PUT` |
| Overvalued | High | Any | `BEAR_CALL_SPREAD` |
| Fair | High | Any | `SHORT_STRANGLE`, `IRON_CONDOR` |
| Fair | Low | Any | `CALENDAR_SPREAD` |

For ordinary stocks, the source top-pick validator disallows naked `SELL_CALL`. The DSL therefore replaces it with `BEAR_CALL_SPREAD`.

## Execution constraints

- One open spread per underlying at a time.
- One successful opening per bar across the entire strategy.
- Strategies are attempted in deterministic rule order.
- If a strategy cannot build valid legs or opening is rejected, the next strategy is attempted.
- A closed or invalid spread reference is cleared so the underlying can re-enter later.

## Three-year daily backtest

Run ID: `ba88ad4e05a666bae9dea3012457dcfb`

Period: 2023-07-27 through 2026-07-27, with 751 replay bars and USD 100,000 initial capital.

| Metric | Result |
| --- | ---: |
| Final equity | USD 72,863.00 |
| Total return | -27.137% |
| Annualized return | -10.033% |
| Maximum drawdown | 31.892% |
| Sharpe ratio | -1.135 |
| Total spreads | 15 |
| Closed / open spreads | 11 / 4 |
| Spread PnL | USD -28,239.00 |
| Winning / losing closed spreads | 2 / 9 |
| Closed-spread win rate | 18.18% |

The report is stored at `tmp/dsl-backtest-reports/daily-picks_ba88ad4e05a666bae9dea3012457dcfb.html`, and the complete status response is stored at `tmp/daily-picks-backtest-3y.json`.

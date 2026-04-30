# Financial Modeling Prep (FMP) API — Reference & Implementation Checklist

> **Base URL:** `https://financialmodelingprep.com/stable`  
> **Auth:** `?apikey=<YOUR_KEY>` appended to every request  
> **Docs:** <https://site.financialmodelingprep.com/developer/docs>  
> **Package:** `pkg/fmp` — Go client, API key `Dts37dS6VtSN4CYvux6R1pVlyzqEdqqB`, rate limit 300 QPM

Legend: ✅ implemented · ⚠️ accessible (endpoint verified) but not yet wrapped · 🔒 premium tier required · 📌 notes follow

---

## 1. Quotes & Real-Time Prices

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Single quote (stock / crypto / forex) | `GET /quote` | ✅ | `Quote(ctx, symbol)` |
| Minimal quote snapshot | `GET /quote-short` | ✅ | `QuoteShort(ctx, symbol)` |
| Crypto quote (convenience) | wraps `/quote` | ✅ | `CryptoQuote(ctx, symbol)` |
| Forex quote (convenience) | wraps `/quote` | ✅ | `ForexQuote(ctx, symbol)` |
| Batch quotes by exchange | `GET /quotes/{exchange}` | 🔒 | — |
| Batch crypto quotes | `GET /quotes/crypto` | 🔒 | — |
| Batch forex quotes | `GET /quotes/forex` | 🔒 | — |

---

## 2. Historical Prices

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Daily EOD OHLCV (stock/crypto/forex) | `GET /historical-price-eod/full` | ✅ | `HistoricalPrices(ctx, symbol, from, to)` |
| Dividend-adjusted daily prices | `GET /historical-price-eod/dividend-adjusted` | ✅ | `AdjustedPrices(ctx, symbol, from, to)` |
| Intraday bars (1min/5min/15min/30min/1h/4h) | `GET /historical-chart/{interval}` | ✅ | `IntradayPrices(ctx, symbol, interval, from, to)` |
| Crypto intraday (convenience) | wraps above | ✅ | `CryptoIntradayPrices(ctx, symbol, interval, from, to)` |
| Forex intraday (convenience) | wraps above | ✅ | `ForexIntradayPrices(ctx, symbol, interval, from, to)` |
| EOD + fundamentals stitched | internal helper | ✅ | `DailyWithFundamentals(ctx, symbol, from, to)` |
| Crypto historical (convenience) | wraps `HistoricalPrices` | ✅ | `CryptoHistoricalPrices(ctx, symbol, from, to)` |
| Forex historical (convenience) | wraps `HistoricalPrices` | ✅ | `ForexHistoricalPrices(ctx, symbol, from, to)` |

---

## 3. Financial Statements (US Stocks)

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Income Statement | `GET /income-statement` | ✅ | `IncomeStatement(ctx, symbol, period, limit)` |
| Balance Sheet | `GET /balance-sheet-statement` | ✅ | `BalanceSheet(ctx, symbol, period, limit)` |
| Cash Flow Statement | `GET /cash-flow-statement` | ✅ | `CashFlowStatement(ctx, symbol, period, limit)` |
| Income Statement (TTM) | `GET /income-statement-ttm` | ✅ | `IncomeStatementTTM(ctx, symbol)` |
| Balance Sheet (TTM) | `GET /balance-sheet-statement-ttm` | ✅ | `BalanceSheetTTM(ctx, symbol)` |
| Cash Flow (TTM) | `GET /cash-flow-statement-ttm` | ✅ | `CashFlowStatementTTM(ctx, symbol)` |

> **P/E and P/B data availability:** ✅ confirmed  
> - `Ratios()` returns `priceToEarningsRatio` and `priceToBookRatio` per reporting period.  
> - `RatiosTTM()` returns the trailing-twelve-month equivalents.  
> - Raw inputs are also available: EPS from `IncomeStatement.EPS`, book value per share from `Ratios.BookValuePerShare` or `BalanceSheet.TotalStockholdersEquity / sharesOutstanding`.

---

## 4. Valuation Ratios & Key Metrics

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Full ratio set (annual/quarterly) | `GET /ratios` | ✅ | `Ratios(ctx, symbol, period, limit)` |
| Ratios TTM | `GET /ratios-ttm` | ✅ | `RatiosTTM(ctx, symbol)` |
| Key metrics (EV, ROE, FCF yield…) | `GET /key-metrics` | ✅ | `KeyMetrics(ctx, symbol, period, limit)` |
| Key metrics TTM | `GET /key-metrics-ttm` | ✅ | `KeyMetricsTTM(ctx, symbol)` |
| DCF intrinsic value | `GET /advanced-discounted-cash-flow` | ⚠️ | — |
| Discounted cash flow simple | `GET /discounted-cash-flow` | ⚠️ | — |
| Levered/unlevered DCF | `GET /levered-discounted-cash-flow` | ⚠️ | — |

---

## 5. Growth Metrics

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Financial growth (10-year per share) | `GET /financial-growth` | ✅ | `FinancialGrowth(ctx, symbol, period, limit)` |
| Income statement growth | `GET /income-statement-growth` | ✅ | `IncomeStatementGrowth(ctx, symbol, period, limit)` |
| Balance sheet growth | `GET /balance-sheet-statement-growth` | ✅ | `BalanceSheetGrowth(ctx, symbol, period, limit)` |
| Cash flow growth | `GET /cash-flow-statement-growth` | ✅ | `CashFlowGrowth(ctx, symbol, period, limit)` |

---

## 6. Company Information

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Company profile | `GET /profile` | ✅ | `Profile(ctx, symbol)` |
| Market cap history | `GET /historical-market-capitalization` | ✅ | `MarketCapHistory(ctx, symbol, from, to, limit)` |
| Employee count history | `GET /historical-employee-count` | ✅ | `EmployeeCount(ctx, symbol)` |
| Company notes | `GET /company-notes` | ✅ | `CompanyNotes(ctx, symbol)` |
| Executive compensation | `GET /governance/executive-compensation` | ⚠️ | — |
| Insider transactions | `GET /insider-trading` | ⚠️ | — |
| Institutional holdings | `GET /institutional-holder` | ⚠️ | — |

---

## 7. Events — Earnings & Dividends

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Earnings calendar / history | `GET /earnings` | ✅ | `Earnings(ctx, symbol, limit)` |
| Earnings calendar (date range) | `GET /earnings-calendar` | ⚠️ | — |
| Earnings surprises | `GET /earnings-surprises` | ⚠️ | — |
| Dividend history | `GET /dividends` | ✅ | `Dividends(ctx, symbol, limit)` |
| Dividend calendar | `GET /dividends-calendar` | ⚠️ | — |
| Stock splits | `GET /splits` | ⚠️ | — |

---

## 8. Analyst Data

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Price target summary | `GET /price-target-summary` | ✅ | `PriceTargetSummary(ctx, symbol)` |
| Price target consensus | `GET /price-target-consensus` | ✅ | `PriceTargetConsensus(ctx, symbol)` |
| Individual price targets | `GET /price-target` | ⚠️ | — |
| Analyst stock grades | `GET /grades` | ⚠️ | — |
| Analyst grade consensus | `GET /grades-consensus` | ⚠️ | — |
| Analyst estimates | `GET /analyst-estimates` | ⚠️ | — |

---

## 9. Market Movers & Market State

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Biggest gainers | `GET /biggest-gainers` | ✅ | `BiggestGainers(ctx)` |
| Biggest losers | `GET /biggest-losers` | ✅ | `BiggestLosers(ctx)` |
| Most active | `GET /most-actives` | ✅ | `MostActive(ctx)` |
| Sector P/E (historical) | `GET /historical-sector-pe` | ✅ | `HistoricalSectorPE(ctx, sector, exchange, from, to, limit)` |
| Sector P/E snapshot (today) | `GET /sector-pe-snapshot` | 🔒 | — |
| Market risk premium by country | `GET /market-risk-premium` | ✅ | `MarketRiskPremium(ctx)` |

---

## 10. Index & Constituents

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| S&P 500 current constituents | `GET /sp500-constituent` | ✅ | `SP500Constituents(ctx)` |
| Nasdaq-100 current constituents | `GET /nasdaq-constituent` | ✅ | `Nasdaq100Constituents(ctx)` |
| Dow Jones current constituents | `GET /dowjones-constituent` | ✅ | `DowJonesConstituents(ctx)` |
| S&P 500 change history | `GET /historical-sp500-constituent` | ✅ | `HistoricalSP500(ctx, limit)` |
| All tradable indexes list | `GET /indexes` | ✅ | `IndexList(ctx)` |

---

## 11. Search

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Search by symbol | `GET /search-symbol` | ✅ | `SearchSymbol(ctx, query)` |
| Search by company name | `GET /search-name` | ✅ | `SearchName(ctx, query)` |
| Full-text search | `GET /search` | ⚠️ | — |
| Exchange symbol list | `GET /symbol/{exchange}` | ⚠️ | — |
| Stock list (all) | `GET /stock-list` | ⚠️ | — |
| Company screener | `GET /company-screener` | ✅ | `Screener(ctx, …)` |

---

## 12. Forex

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Forex pair list (1 500+ pairs) | `GET /forex-list` | ✅ | `ForexList(ctx)` |
| Real-time forex quote | wraps `/quote` | ✅ | `ForexQuote(ctx, symbol)` |
| Daily OHLCV history | wraps `HistoricalPrices` | ✅ | `ForexHistoricalPrices(ctx, symbol, from, to)` |
| Intraday bars | wraps `IntradayPrices` | ✅ | `ForexIntradayPrices(ctx, symbol, interval, from, to)` |

---

## 13. Cryptocurrency

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Real-time crypto quote | wraps `/quote` | ✅ | `CryptoQuote(ctx, symbol)` |
| Daily OHLCV history | wraps `HistoricalPrices` | ✅ | `CryptoHistoricalPrices(ctx, symbol, from, to)` |
| Intraday bars | wraps `IntradayPrices` | ✅ | `CryptoIntradayPrices(ctx, symbol, interval, from, to)` |
| Crypto list | `GET /digital-asset-list` | 🔒 | — |
| Crypto market cap history | `GET /historical-market-capitalization` | ✅ | `MarketCapHistory(ctx, symbol, …)` |

> Note: `/digital-asset-list` and `/crypto-list` return empty arrays on the Starter plan. Use symbol strings directly (e.g. `"BTCUSD"`, `"ETHUSD"`).

---

## 14. Economics & Macro

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| US Treasury yield curve | `GET /treasury-rates` | ✅ | `TreasuryRates(ctx, from, to)` |
| Economic indicators (GDP, CPI, etc.) | `GET /economic-indicators` | ✅ | `EconomicIndicators(ctx, name, limit)` |
| Economic calendar | `GET /economic-calendar` | ⚠️ | — |

---

## 15. ESG

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| ESG disclosures (SEC-sourced) | `GET /esg-disclosures` | ✅ | `ESGDisclosures(ctx, symbol, limit)` |
| ESG risk rating | `GET /esg-ratings` | ✅ | `ESGRating(ctx, symbol)` |

---

## 16. News

| Feature | Endpoint | Status | Go method |
|---|---|---|---|
| Stock/general news | `GET /news` (or `/news/stock-latest`) | ✅ | `News(ctx, symbol, limit)` |
| Forex news | `GET /news/forex-latest` | ⚠️ | — |
| Crypto news | `GET /news/crypto-latest` | ⚠️ | — |
| Press releases | `GET /press-releases` | ⚠️ | — |

---

## P/E and P/B Ratio Data — Confirmation

FMP provides **all inputs** needed to compute or directly retrieve P/E and P/B ratios for US stocks:

| Data point | Source endpoint | Go method | Field |
|---|---|---|---|
| Price-to-Earnings ratio (pre-computed) | `/ratios` | `Ratios()` | `PriceToEarningsRatio` |
| P/E TTM (pre-computed) | `/ratios-ttm` | `RatiosTTM()` | `PriceToEarningsRatioTtm` |
| Price-to-Book ratio (pre-computed) | `/ratios` | `Ratios()` | `PriceToBookRatio` |
| P/B TTM (pre-computed) | `/ratios-ttm` | `RatiosTTM()` | `PriceToBookRatioTtm` |
| EPS (basic & diluted) | `/income-statement` | `IncomeStatement()` | `EPS`, `EPSDiluted` |
| Net income | `/income-statement` | `IncomeStatement()` | `NetIncome` |
| Shares outstanding | `/income-statement` | `IncomeStatement()` | `WeightedAverageShsOut` |
| Total stockholders' equity | `/balance-sheet-statement` | `BalanceSheet()` | `TotalStockholdersEquity` |
| Book value per share | `/ratios` | `Ratios()` | `BookValuePerShare` |
| Current price | `/quote` | `Quote()` | `Price` |
| Market cap | `/profile` | `Profile()` | `MarketCap` |

All of the above are available on the **Starter** tier (confirmed with live probing on 2026-04-30).

---

## Plan Tier Notes

| Feature group | Tier required |
|---|---|
| Real-time quotes, EOD prices, intraday, statements | Starter |
| Ratios, key metrics, growth, screener | Starter |
| Market movers (gainers/losers/actives) | Starter |
| Index constituents, news, ESG | Starter |
| Batch quotes by exchange (`/quotes/{exchange}`) | Standard+ |
| Sector P/E snapshot (`/sector-pe-snapshot?date=`) | Standard+ |
| Crypto list (`/digital-asset-list`) | Standard+ |

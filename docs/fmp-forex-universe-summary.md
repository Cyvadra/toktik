# FMP Forex Universe Snapshot

Snapshot date: 2026-05-07
Source: FMP `GET /stable/forex-list`

## What FMP Returns

FMP does not return a built-in category tree for forex. The API returns a flat
list of pairs, each with:

- `symbol`
- `fromCurrency`
- `toCurrency`
- `fromName`
- `toName`

That means the useful grouping has to be done on our side.

## Live Snapshot Summary

- Total pairs: `1545`
- Fiat / FX-index style pairs: `1475`
- Metal-linked pairs: `70`
- Crypto-linked pairs observed in this snapshot: `0`

## Most Common Quote Currencies

These counts answer: how many pairs use a currency on the right side.

| Quote | Pair count |
|---|---:|
| USD | 137 |
| EUR | 129 |
| GBP | 125 |
| ZAR | 53 |
| JPY | 43 |
| CAD | 42 |
| AUD | 40 |
| CHF | 38 |
| DKK | 31 |
| NZD | 28 |
| HKD | 27 |
| SGD | 27 |
| INR | 25 |
| CNY | 24 |
| SEK | 24 |

## Most Common Base Currencies

These counts answer: how many pairs use a currency on the left side.

| Base | Pair count |
|---|---:|
| USD | 137 |
| EUR | 129 |
| GBP | 125 |
| ZAR | 53 |
| JPY | 43 |
| CAD | 43 |
| AUD | 40 |
| CHF | 38 |
| DKK | 31 |
| NZD | 28 |
| SGD | 27 |
| HKD | 27 |
| INR | 25 |
| SEK | 24 |
| CNY | 24 |

## Practical Trading Buckets

If the goal is actual trading or research coverage, the full 1545-pair universe
is usually unnecessary. A smaller watchlist is more realistic.

### 1. G10 Core Crosses

FMP contains all ordered combinations among the usual 8 liquid currencies:
`USD`, `EUR`, `GBP`, `JPY`, `CHF`, `CAD`, `AUD`, `NZD`.

Observed count: `56`

Representative subset:

- `EURUSD`
- `GBPUSD`
- `USDJPY`
- `USDCHF`
- `AUDUSD`
- `USDCAD`
- `NZDUSD`
- `EURJPY`
- `GBPJPY`
- `EURGBP`
- `AUDJPY`
- `CADJPY`
- `CHFJPY`

### 2. Asia FX Basket

Observed examples: `13`

- `USDJPY`
- `USDCNH`
- `USDCNY`
- `USDHKD`
- `USDSGD`
- `USDKRW`
- `USDTWD`
- `USDINR`
- `USDTHB`
- `USDIDR`
- `USDPHP`
- `USDVND`
- `USDMYR`

### 3. EMEA FX Basket

Observed examples: `14`

- `USDTRY`
- `USDZAR`
- `USDAED`
- `USDSAR`
- `USDILS`
- `USDEGP`
- `USDQAR`
- `USDKWD`
- `USDNOK`
- `USDSEK`
- `USDDKK`
- `USDPLN`
- `USDCZK`
- `USDHUF`

### 4. LatAm FX Basket

Observed examples: `6`

- `USDMXN`
- `USDBRL`
- `USDCLP`
- `USDCOP`
- `USDPEN`
- `USDARS`

### 5. Precious Metals Crosses

Observed examples: `6`

- `XAUUSD`
- `XAGUSD`
- `XAUEUR`
- `XAUJPY`
- `XAUGBP`
- `XAGJPY`

## Recommended Narrow Universe

If we only want a practical first batch instead of the full forex universe,
this smaller list is enough for most macro or systematic work:

### Core FX

- `EURUSD`
- `GBPUSD`
- `USDJPY`
- `USDCHF`
- `AUDUSD`
- `USDCAD`
- `NZDUSD`
- `EURJPY`
- `GBPJPY`
- `EURGBP`

### Asia / China-sensitive

- `USDCNH`
- `USDHKD`
- `USDSGD`
- `USDKRW`
- `USDINR`

### EM / Carry / Commodity FX

- `USDMXN`
- `USDBRL`
- `USDZAR`
- `USDTRY`
- `USDPLN`

### Metal Proxies

- `XAUUSD`
- `XAGUSD`

## Notes

- This is a live snapshot summary, not a permanent contract. FMP may add or
  remove pairs over time.
- FMP's list is flat; if we need a stable internal taxonomy, we should define
  our own grouping layer rather than relying on the provider.
- If needed, we can also export the full 1545-pair list into `docs/` as CSV.
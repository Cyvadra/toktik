# FMP Forex Trading Watchlist

Snapshot date: 2026-05-07
Source universe: FMP `GET /stable/forex-list`

## Goal

This file narrows the full FMP forex universe down to a practical trading and
research watchlist.

Design principles:

- Keep the universe small enough to operate manually.
- Prioritize liquidity, macro sensitivity, and cleaner execution.
- Separate always-on core pairs from optional EM and metal proxies.

## Recommended Structure

Use a 3-layer setup instead of loading all 1545 pairs.

### Tier 1: Core FX

These are the first pairs to include. They cover the main developed-market FX
flows and most common macro expressions.

| Symbol | Why keep it |
|---|---|
| EURUSD | Deepest global FX liquidity, clean macro benchmark |
| GBPUSD | USD + UK rates and risk sentiment exposure |
| USDJPY | Global rates and risk-on/risk-off anchor |
| USDCHF | Defensive USD/CHF flow, useful stress indicator |
| AUDUSD | China and commodity-sensitive G10 proxy |
| USDCAD | Oil and North America macro linkage |
| NZDUSD | Higher-beta commodity / rates-sensitive G10 pair |
| EURJPY | Europe risk plus Japan funding dynamic |
| GBPJPY | High-beta carry and risk proxy |
| EURGBP | Clean Europe-vs-UK relative macro trade |

Tier 1 count: `10`

### Tier 2: Asia And China-Sensitive FX

These pairs expand the macro surface without making the universe too large.

| Symbol | Why keep it |
|---|---|
| USDCNH | Best offshore China macro proxy in the set |
| USDHKD | HK peg and regional funding monitor |
| USDSGD | Regional Asia growth / policy proxy |
| USDKRW | Korea export and global manufacturing sensitivity |
| USDINR | Large EM Asia carry and policy market |

Tier 2 count: `5`

### Tier 3: EM And Commodity / Carry FX

These are useful if we want a bit more return dispersion or macro breadth.

| Symbol | Why keep it |
|---|---|
| USDMXN | One of the more liquid EM carry pairs |
| USDBRL | LatAm risk and commodity sensitivity |
| USDZAR | Classic high-vol EM risk sentiment pair |
| USDTRY | High-vol policy stress instrument |
| USDPLN | Europe EM and regional rates proxy |

Tier 3 count: `5`

### Tier 4: Metal Proxies

These are not plain fiat FX, but they are useful when the strategy wants a
macro hedge or inflation / safe-haven sleeve.

| Symbol | Why keep it |
|---|---|
| XAUUSD | Gold as macro stress / inflation / USD hedge proxy |
| XAGUSD | Silver with more growth and volatility beta |

Tier 4 count: `2`

## Default Practical Universe

If we want one default production watchlist, use these `22` symbols:

### Core 10

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

### Asia 5

- `USDCNH`
- `USDHKD`
- `USDSGD`
- `USDKRW`
- `USDINR`

### EM 5

- `USDMXN`
- `USDBRL`
- `USDZAR`
- `USDTRY`
- `USDPLN`

### Metals 2

- `XAUUSD`
- `XAGUSD`

## Smaller Execution Set

If we want something even tighter for first deployment, start with these `12`:

- `EURUSD`
- `GBPUSD`
- `USDJPY`
- `USDCHF`
- `AUDUSD`
- `USDCAD`
- `NZDUSD`
- `EURJPY`
- `EURGBP`
- `USDCNH`
- `XAUUSD`
- `USDMXN`

This 12-symbol set is a good compromise between liquidity, macro breadth, and
operational simplicity.

## Suggested Use

- Intraday or short swing: focus on Tier 1 plus `USDCNH` and `XAUUSD`.
- Medium-term macro rotation: use the default 22-symbol universe.
- If execution quality matters more than breadth, avoid starting with too many
  EM pairs.

## Notes

- This watchlist is a trading taxonomy defined on our side, not an FMP-native
  category.
- If needed, the next step can be a machine-readable config file such as YAML or
  CSV for ingestion by sync jobs or signal pipelines.
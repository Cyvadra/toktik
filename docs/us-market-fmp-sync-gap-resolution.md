# US Market FMP Sync Gap Resolution

This document describes how the FMP stock sync now handles option underlyings
that were missing from `us_stocks_bar_1m`.

## Goal

Keep `us_stocks_bar_1m.symbol` aligned with the options `underlying`, while
still allowing FMP fetches to use an alternate symbol when needed.

## Sync Flow

1. Start from the existing stored stock universe in `us_stocks_bar_1m`.
2. List distinct option underlyings from `us_options_bar_1m`.
3. Compute underlyings that are present in options but missing from stocks.
4. Append only deterministic, code-reviewed sync targets:
   - direct FMP-supported gap symbols such as `ABBNY`, `FRCB`, `YELLQ`
   - index aliases such as `SPX -> ^GSPC`, `VIX -> ^VIX`, `DJX -> ^DJI`
5. Fetch from FMP using the mapped fetch symbol.
6. Insert rows into `us_stocks_bar_1m` using the option underlying as the
   stored symbol.

## Important Constraint

The sync does not auto-apply search-derived mappings during ingestion.

It also does not auto-apply dotted share-class fallback aliases in the FMP
intraday sync path.

During runtime validation, symbols such as `BRK.B` returned FMP `402 Premium
Query Parameter` errors on the intraday endpoint under the current
subscription, so dotted-share fallback remains analysis-only for now.

Those search results were useful for exploratory coverage analysis, but several
hits were heuristic or semantically ambiguous. For example, some search results
matched ETFs, leveraged products, foreign listings, or unrelated instruments.

For production sync, only deterministic mappings are used.

## Current Deterministic Sources

- `stored-stock`: symbol already exists in `us_stocks_bar_1m`
- `option-gap-direct`: FMP supports the missing underlying directly
- `option-gap-index-alias`: missing underlying is an index-style symbol with a
  code-reviewed FMP index alias

## CLI Behavior

`cmd/us-market-fmp-klines-sync` now resolves logical sync targets instead of a
flat list of fetch symbols.

When `--symbols` is empty:

- it syncs all stored stock symbols
- it also appends supported option-underlying gap mappings by default

When `--symbols` is provided:

- the provided values are treated as logical stored symbols
- deterministic index aliases such as `SPX -> ^GSPC` are applied automatically
  for fetching

Optional flag:

- `--include-option-gap-mappings=false` disables the automatic gap extension in
  the default bulk-sync mode

## One-Off Missing-Symbol Backfill

There is currently no dedicated `--only-missing-symbols` flag.

To backfill only the missing option-underlying symbols instead of running the
default bulk stock sync, pass `--symbols` explicitly with the logical
underlying list you want to backfill.

For `us_stocks_bar_1m` history backfill, prefer the deterministic subset that
was verified with `intraday_covered=true` in
`docs/us-options-missing-stock-underlyings-fmp-coverage.csv`.

Example:

```bash
go run ./cmd/us-market-fmp-klines-sync \
  --symbols "ABBNY,ADAPY,AFIIQ,BIGGQ,DIDIY,DJX,ELMSQ,FRCB,IBOT,JTKWY,KLDO,LNWO,MRUT,NDX,NDXP,NQX,ORANY,RUT,RUTW,SDCCQ,SICP,SONDQ,SPIKE,SPX,SPXW,TPICQ,VIX,VIXW,VOLQ,VQSSF,XND,XSP,YELLQ" \
  --start-date 2024-01-01 \
  --end-date 2024-12-31
```

If you want to generate that list from the coverage CSV at runtime:

```bash
SYMS="$(python - <<'PY'
import csv
from pathlib import Path
rows = csv.DictReader(Path('docs/us-options-missing-stock-underlyings-fmp-coverage.csv').open())
syms = sorted(
    row['underlying']
    for row in rows
    if row['resolved_via'] in ('direct', 'index')
    and row['intraday_covered'].lower() == 'true'
)
print(','.join(syms))
PY
)"

go run ./cmd/us-market-fmp-klines-sync \
  --symbols "$SYMS" \
  --start-date 2024-01-01 \
  --end-date 2024-12-31
```
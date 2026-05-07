# US Market Symbol Gap Report

Generated at: 2026-05-07T07:46:00Z UTC

## ClickHouse Universe Diff

- Options source table: `us_options_bar_1d`
- Stocks source table: `us_stocks_bar_1d`
- Distinct option underlyings: 7742
- Distinct stock symbols: 18718
- Raw missing underlyings: 158
- Raw-missing symbols that map to dotted stock symbols already in stocks table: 10
- Still missing after dotted fallback normalization: 148

Artifacts:
- `docs/us-stocks-symbols-1d.txt`
- `docs/us-options-underlyings-1d.txt`
- `docs/us-options-missing-stock-underlyings-1d.txt`
- `docs/us-options-missing-stock-underlyings-fallback-mapped-1d.txt`
- `docs/us-options-still-missing-stock-underlyings-1d.txt`

## FMP Coverage Results

- Coverage scope: still-missing
- Tested symbols: 148
- Any FMP coverage: 89
- QuoteShort coverage: 89
- Historical EOD coverage: 59
- Intraday coverage: 54
- Resolved directly with raw underlying: 52
- Resolved via dotted fallback alias: 0
- Resolved via FMP search suggestions: 22
- Resolved via index candidates: 15
- No FMP coverage in tested set: 59
- EOD window: 2026-04-07 .. 2026-05-07
- Intraday window: 2026-05-02 .. 2026-05-07

Top results:

| underlying | candidate | via | quote | eod | intraday | fallback_exists |
| --- | --- | --- | --- | --- | --- | --- |
| ABBNY | ABBNY | direct | true | true | true | false |
| ACHHY | ACHHY | direct | true | false | false | false |
| ADAPY | ADAPY | direct | true | true | true | false |
| AFIIQ | AFIIQ | direct | true | true | true | false |
| ALLGF | ALLGF | direct | true | true | false | false |
| AMRSQ | AMRSQ | direct | false | false | false | false |
| ATEST | ATEST | direct | true | false | false | false |
| AZULQ | AZULQ | direct | false | false | false | false |
| BBBYQ | BBBYQ | direct | false | false | false | false |
| BIGGQ | BIGGQ | direct | true | true | true | false |
| BKX | PBKX | search | true | true | true | false |
| BKXPM | BKXPM | direct | false | false | false | false |
| BOXDQ | BOXDQ | direct | false | false | false | false |
| CBO | CBON | search | true | true | true | false |
| CBTXW | CBTXW | direct | false | false | false | false |
| CBX | CBXUSD | search | true | true | true | false |
| CLVSQ | CLVSQ | direct | false | false | false | false |
| CMBMF | CMBMF | direct | false | false | false | false |
| CONNQ | CONNQ | direct | false | false | false | false |
| CORZQ | CORZQ | direct | true | false | false | false |

Artifacts:
- `docs/us-options-missing-stock-underlyings-fmp-coverage.csv`

## No-Coverage Breakdown

- delisted-or-bankruptcy-otc: 20
- warrant-or-spac-derivative: 5
- foreign-adr-or-otc: 5
- custom-or-unsupported-index: 15
- test-or-placeholder: 11
- other-unresolved: 3

Artifacts:
- `docs/us-options-no-fmp-coverage-categories.md`


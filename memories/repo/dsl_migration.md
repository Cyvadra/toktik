# DSL Migration Progress

## Status: Phase 5 Complete (Testing)

### Parser Enhancement
- Added `IndexAssignStmt` (arr[i] = value) to parser + interpreter
- AST node: `ast.IndexAssignStmt{Token, Left, Index, Value}`
- Parser: in `parseExprOrAssignStmt()`, after parsing expr, checks for `IndexExpr` + `=`
- Interpreter: `execIndexAssign()` mutates array slice element in place

### DSL Strategies (all 10 parse clean)
- golden-cross-dsl, ema-atr-spot-dsl, delta-filter-dsl
- buy-flash-low-dsl, lvol-scalper-dsl, covered-call-0330-tvsig-dsl
- dual-spreads-btc-volatility-dsl, retracement-ratio-protective-spread-dsl
- ma-deviation-spread-outer-source-dsl, turtle-trend-simp-dsl

### Key DSL Modules Added
- signal.* (active/count/direction/action/qty/name/remarks/ref/group_ref/consumed/consume)
- event.* (pending/peek/next/consume_all/is_init/is_add/is_close/is_roll)
- order.* (market/market_notional/limit/stop/stop_limit/twap/immediate)
- config.* (get/string)
- ref.* (set/get/has/clear/inc/dec)
- strategy.equity, strategy.cash builtins

### Bridge Enhancements
- `Options.Params`, `Options.Config` for parameter/config overrides
- `catalogToConfigMap()` maps catalog.Config → map for config.get()
- SignalEvent integration: LoadEvents + BuildEventSeries + EventsAtTime per bar

### Test Coverage
- `pkg/dsl/catalog/catalog_test.go`: TestDSLStrategiesParse validates all 10 scripts

### Fix Notes
- `OptionPriceMode` (int) → store as `int()` not `string()` in config map
- Plain `=` must keep normal per-bar declaration/series behavior, but when the target name already belongs to `var`/`varip`, runtime must treat it as a persistent-state update instead of a new local binding.
- Array containers must snapshot `SeriesVal` members when building `[x]` or mutating `arr[i]`; otherwise loop-built arrays alias the live series pointer and collapse to the last iterated value, which can silently corrupt strategy state like `spread_ids` / `open_spreads`.
- Retracement DSL signal source defaults must resolve to actual CSV paths, not the literal token `12h`; `bridge.Preload()` prioritizes `Options.SignalSource`, so a token string silently produces an all-zero entry series.

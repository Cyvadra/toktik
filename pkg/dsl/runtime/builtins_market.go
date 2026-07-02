package runtime

import "math"

type MarketContext struct {
	TrendState     string
	HVState        string
	IVState        string
	ValuationState string
	ScenarioLabel  string
	RiskLabel      string
	ReasonCodes    []string
	Warnings       []string
}

func RegisterMarketBuiltins(ip *Interpreter) {
	ip.RegisterBuiltinWithParams("market.context", []string{"rsi14", "cci20", "hv_percentile", "iv_percentile", "valuation_percentile"}, func(args []Value) Value {
		rsi14 := argFloat(args, 0, math.NaN())
		cci20 := argFloat(args, 1, math.NaN())
		hvPercentile := argFloat(args, 2, math.NaN())
		ivPercentile := argFloat(args, 3, math.NaN())
		valuationPercentile := argFloat(args, 4, math.NaN())
		ctx := MarketContext{
			TrendState:     classifyTrend(rsi14),
			HVState:        classifyVolPercentile(hvPercentile),
			IVState:        classifyVolPercentile(ivPercentile),
			ValuationState: classifyValuationPercentile(valuationPercentile),
		}
		ctx.ReasonCodes = append(ctx.ReasonCodes, "trend:"+ctx.TrendState, "hv:"+ctx.HVState, "iv:"+ctx.IVState, "valuation:"+ctx.ValuationState)
		if math.IsNaN(rsi14) {
			ctx.Warnings = append(ctx.Warnings, "missing_rsi14")
		}
		if math.IsNaN(cci20) {
			ctx.Warnings = append(ctx.Warnings, "missing_cci20")
		}
		if math.IsNaN(hvPercentile) {
			ctx.Warnings = append(ctx.Warnings, "missing_hv_percentile")
		}
		if math.IsNaN(ivPercentile) {
			ctx.Warnings = append(ctx.Warnings, "missing_iv_percentile")
		}
		if math.IsNaN(valuationPercentile) {
			ctx.Warnings = append(ctx.Warnings, "missing_valuation_percentile")
		}
		if ctx.TrendState == "range" && !math.IsNaN(cci20) && math.Abs(cci20) >= 101 {
			ctx.Warnings = append(ctx.Warnings, "range_state_rejected_by_cci")
			ctx.ReasonCodes = append(ctx.ReasonCodes, "cci:not_range")
			ctx.TrendState = "unknown"
		}
		ctx.ScenarioLabel, ctx.RiskLabel = classifyMarketScenario(ctx, ivPercentile)
		return ObjVal(ctx)
	})

	ip.RegisterBuiltin("market.trend_state", func(args []Value) Value { return StringVal(marketContextArg(args).TrendState) })
	ip.RegisterBuiltin("market.hv_state", func(args []Value) Value { return StringVal(marketContextArg(args).HVState) })
	ip.RegisterBuiltin("market.iv_state", func(args []Value) Value { return StringVal(marketContextArg(args).IVState) })
	ip.RegisterBuiltin("market.valuation_state", func(args []Value) Value { return StringVal(marketContextArg(args).ValuationState) })
	ip.RegisterBuiltin("market.scenario_label", func(args []Value) Value { return StringVal(marketContextArg(args).ScenarioLabel) })
	ip.RegisterBuiltin("market.risk_label", func(args []Value) Value { return StringVal(marketContextArg(args).RiskLabel) })
	ip.RegisterBuiltin("market.reason_codes", func(args []Value) Value { return stringArrayVal(marketContextArg(args).ReasonCodes) })
	ip.RegisterBuiltin("market.warnings", func(args []Value) Value { return stringArrayVal(marketContextArg(args).Warnings) })
}

func marketContextArg(args []Value) MarketContext {
	if len(args) == 0 {
		return MarketContext{TrendState: "unknown", HVState: "unknown", IVState: "unknown", ValuationState: "unknown", ScenarioLabel: "unknown", RiskLabel: "unknown"}
	}
	ctx, ok := args[0].Obj().(MarketContext)
	if !ok {
		if ptr, ok := args[0].Obj().(*MarketContext); ok && ptr != nil {
			return *ptr
		}
		return MarketContext{TrendState: "unknown", HVState: "unknown", IVState: "unknown", ValuationState: "unknown", ScenarioLabel: "unknown", RiskLabel: "unknown"}
	}
	return ctx
}

func classifyTrend(rsi float64) string {
	if math.IsNaN(rsi) {
		return "unknown"
	}
	if rsi > 60 {
		return "up"
	}
	if rsi < 40 {
		return "down"
	}
	return "range"
}

func classifyVolPercentile(value float64) string {
	if math.IsNaN(value) {
		return "unknown"
	}
	if value >= 70 {
		return "high"
	}
	if value >= 35 {
		return "mid"
	}
	return "low"
}

func classifyValuationPercentile(value float64) string {
	if math.IsNaN(value) {
		return "unknown"
	}
	if value < 35 {
		return "undervalued"
	}
	if value >= 80 {
		return "overvalued"
	}
	return "fair"
}

func classifyMarketScenario(ctx MarketContext, ivPercentile float64) (string, string) {
	if !math.IsNaN(ivPercentile) && ivPercentile >= 88 && ctx.TrendState == "up" {
		return "high_iv_melt_up", "elevated"
	}
	if !math.IsNaN(ivPercentile) && ivPercentile >= 88 {
		return "panic_selloff", "high"
	}
	if !math.IsNaN(ivPercentile) && ivPercentile <= 30 && ctx.ValuationState == "overvalued" {
		return "overvalued_low_vol", "elevated"
	}
	if !math.IsNaN(ivPercentile) && ivPercentile <= 30 {
		return "low_vol_pre_breakout", "mid"
	}
	if ctx.TrendState == "unknown" || ctx.HVState == "unknown" || ctx.IVState == "unknown" || ctx.ValuationState == "unknown" {
		return "unknown", "unknown"
	}
	return ctx.TrendState + "_" + ctx.IVState + "_iv", "normal"
}

func stringArrayVal(values []string) Value {
	out := make([]Value, len(values))
	for index, value := range values {
		out[index] = StringVal(value)
	}
	return ArrayVal(out)
}

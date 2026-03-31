package catalog

import "strings"

// RegularTradeMode describes how meaningful the non-options trading leg is.
type RegularTradeMode string

const (
	RegularTradeNone       RegularTradeMode = "none"
	RegularTradeSignalOnly RegularTradeMode = "signal_only"
	RegularTradeMaterial   RegularTradeMode = "material"
)

// StrategyProfile describes whether a strategy uses options and how large any
// regular trading leg is expected to be.
type StrategyProfile struct {
	UsesOptions  bool             `json:"uses_options"`
	RegularTrade RegularTradeMode `json:"regular_trade"`
}

// Normalized returns the profile with defaulted fields.
func (p StrategyProfile) Normalized() StrategyProfile {
	p.RegularTrade = normalizeRegularTradeMode(p.RegularTrade)
	return p
}

// Label returns a concise Chinese label for report display.
func (p StrategyProfile) Label() string {
	p = p.Normalized()
	switch {
	case p.UsesOptions && p.RegularTrade == RegularTradeSignalOnly:
		return "期权主导 + 微量常规信号"
	case p.UsesOptions && p.RegularTrade == RegularTradeMaterial:
		return "期权 + 常规交易"
	case p.UsesOptions:
		return "纯期权"
	case p.RegularTrade == RegularTradeSignalOnly:
		return "微量常规信号"
	default:
		return "常规交易"
	}
}

// UsesMeaningfulRegularTrades reports whether the strategy relies on material
// non-options position sizing.
func (p StrategyProfile) UsesMeaningfulRegularTrades() bool {
	return p.Normalized().RegularTrade == RegularTradeMaterial
}

// UsesAnyRegularTrades reports whether the strategy issues any non-options leg.
func (p StrategyProfile) UsesAnyRegularTrades() bool {
	return p.Normalized().RegularTrade != RegularTradeNone
}

// UsesSignalOnlyRegularTrades reports whether regular trades are effectively a
// signal-tracking sidecar rather than a meaningful capital consumer.
func (p StrategyProfile) UsesSignalOnlyRegularTrades() bool {
	return p.Normalized().RegularTrade == RegularTradeSignalOnly
}

// CapitalMode is the denomination mode used to interpret -capital.
type CapitalMode string

const (
	CapitalModeUSD       CapitalMode = "usd"
	CapitalModeBaseAsset CapitalMode = "base_asset"
)

// StrategyRuntimeProfile is the per-strategy resolved profile used by the CLI.
type StrategyRuntimeProfile struct {
	CanonicalName       string          `json:"canonical_name"`
	DisplayName         string          `json:"display_name"`
	Profile             StrategyProfile `json:"profile"`
	CapitalMode         CapitalMode     `json:"capital_mode"`
	CapitalUnit         string          `json:"capital_unit"`
	CapitalExplanation  string          `json:"capital_explanation"`
	ProfileLabel        string          `json:"profile_label"`
	OptionsUnit         string          `json:"options_unit"`
	RegularTradeSummary string          `json:"regular_trade_summary"`
}

func buildRuntimeProfile(canonicalName string, profile StrategyProfile, baseAsset string) StrategyRuntimeProfile {
	profile = profile.Normalized()
	baseAsset = strings.ToUpper(strings.TrimSpace(baseAsset))
	if baseAsset == "" {
		baseAsset = "BTC"
	}

	runtime := StrategyRuntimeProfile{
		CanonicalName: canonicalName,
		DisplayName:   canonicalName,
		Profile:       profile,
		ProfileLabel:  profile.Label(),
		OptionsUnit:   baseAsset,
	}

	switch profile.RegularTrade {
	case RegularTradeSignalOnly:
		runtime.RegularTradeSummary = "常规仓位仅用于信号跟踪，不作为主要资金暴露。"
	case RegularTradeMaterial:
		runtime.RegularTradeSummary = "常规交易会消耗主要资金头寸。"
	default:
		runtime.RegularTradeSummary = "不包含常规交易腿。"
	}

	if profile.UsesOptions {
		runtime.CapitalMode = CapitalModeBaseAsset
		runtime.CapitalUnit = baseAsset
		switch profile.RegularTrade {
		case RegularTradeSignalOnly:
			runtime.CapitalExplanation = "该策略包含期权逻辑，且常规交易仓位仅用于信号跟踪，-capital 按 BTC 计价。"
		case RegularTradeMaterial:
			runtime.CapitalExplanation = "该策略包含期权逻辑。为保持期权 premium、价差盈亏与账户权益口径一致，-capital 按 BTC 计价。"
		default:
			runtime.CapitalExplanation = "该策略为纯期权回测，-capital 按 BTC 计价。"
		}
		return runtime
	}

	runtime.CapitalMode = CapitalModeUSD
	runtime.CapitalUnit = "USD"
	runtime.CapitalExplanation = "该策略不包含期权逻辑，-capital 按 USD 计价。"
	return runtime
}

func normalizeRegularTradeMode(mode RegularTradeMode) RegularTradeMode {
	switch mode {
	case RegularTradeNone, RegularTradeSignalOnly, RegularTradeMaterial:
		return mode
	default:
		return RegularTradeMaterial
	}
}

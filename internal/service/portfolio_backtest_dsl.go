package service

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/dsl/configmap"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

func resolveRequestedStrategies(req dto.StrategyBacktestRunRequest, cfg strategies.Config, asset string) ([]strategies.ResolvedStrategy, string, error) {
	dslSource := strings.TrimSpace(req.DSL)
	if dslSource == "" {
		strategyRequest := defaultString(req.Strategy, "both")
		resolved, err := strategies.ResolveDetailed(strategyRequest, cfg, asset)
		if err != nil {
			return nil, "", err
		}
		return resolved, strategyRequest, nil
	}

	resolved, err := buildDynamicDSLResolvedStrategy(req, cfg)
	if err != nil {
		return nil, "", err
	}
	return []strategies.ResolvedStrategy{resolved}, resolved.CanonicalName, nil
}

func buildDynamicDSLResolvedStrategy(req dto.StrategyBacktestRunRequest, cfg strategies.Config) (strategies.ResolvedStrategy, error) {
	dslSource := strings.TrimSpace(req.DSL)
	params, err := normalizeBacktestDSLParams(req.DSLParams)
	if err != nil {
		return strategies.ResolvedStrategy{}, err
	}

	config := backtestDSLConfigMap(cfg, req)
	signalSource := strings.TrimSpace(req.SignalSource)
	baseOpts := bridge.Options{
		SignalSource: signalSource,
		Config:       config,
	}
	parsed := bridge.NewWithOptions(dslSource, baseOpts)
	if errs := parsed.ParseErrors(); len(errs) > 0 {
		return strategies.ResolvedStrategy{}, dto.NewValidationError("invalid dsl: %s", strings.Join(errs, "; "))
	}
	manifest := parsed.Manifest()

	validatedParams, err := validateBacktestDSLParams(manifest.Inputs, params)
	if err != nil {
		return strategies.ResolvedStrategy{}, err
	}
	profile, err := resolveDynamicDSLProfile(manifest, req.DSLProfile)
	if err != nil {
		return strategies.ResolvedStrategy{}, err
	}

	newStrategy := func() (*bridge.DslStrategy, error) {
		strategy := bridge.NewWithOptions(dslSource, bridge.Options{
			SignalSource: signalSource,
			Params:       validatedParams,
			Config:       config,
		})
		if errs := strategy.ParseErrors(); len(errs) > 0 {
			return nil, dto.NewValidationError("invalid dsl: %s", strings.Join(errs, "; "))
		}
		return strategy, nil
	}

	strategy, err := newStrategy()
	if err != nil {
		return strategies.ResolvedStrategy{}, err
	}

	canonicalName := slugify(strategy.Name())
	if canonicalName == "" {
		canonicalName = "dynamic-dsl"
	}
	return strategies.ResolvedStrategy{
		CanonicalName: canonicalName,
		Strategy:      strategy,
		Factory: func() (backtest.Strategy, error) {
			return newStrategy()
		},
		Profile: profile,
		Runtime: strategies.StrategyRuntimeProfile{
			CanonicalName: canonicalName,
			DisplayName:   strategy.Name(),
			Profile:       profile,
			ProfileLabel:  profile.Label(),
		},
	}, nil
}

func normalizeBacktestDSLParams(params map[string]interface{}) (map[string]interface{}, error) {
	if len(params) == 0 {
		return nil, nil
	}
	converted := make(map[string]interface{}, len(params))
	for key, raw := range params {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case float64:
			converted[trimmed] = value
		case float32:
			converted[trimmed] = float64(value)
		case int:
			converted[trimmed] = value
		case int32:
			converted[trimmed] = int(value)
		case int64:
			converted[trimmed] = int(value)
		case bool:
			converted[trimmed] = value
		case string:
			converted[trimmed] = value
		default:
			return nil, dto.NewValidationError("unsupported dsl param type for %q", trimmed)
		}
	}
	if len(converted) == 0 {
		return nil, nil
	}
	return converted, nil
}

func validateBacktestDSLParams(schema []bridge.ParamSchema, params map[string]interface{}) (map[string]interface{}, error) {
	if len(params) == 0 {
		return nil, nil
	}

	allowed := make(map[string]bridge.ParamSchema, len(schema)*2)
	canonicalByKey := make(map[string]string, len(schema)*2)
	allowedKeys := make([]string, 0, len(schema)*2)
	for _, item := range schema {
		canonical := item.LookupKey()
		for _, key := range []string{canonical, item.Name} {
			trimmed := strings.TrimSpace(key)
			if trimmed == "" {
				continue
			}
			normalized := strings.ToLower(trimmed)
			if _, exists := allowed[normalized]; exists {
				continue
			}
			allowed[normalized] = item
			canonicalByKey[normalized] = canonical
			allowedKeys = append(allowedKeys, trimmed)
		}
	}
	sort.Strings(allowedKeys)

	validated := make(map[string]interface{}, len(params))
	for key, raw := range params {
		normalized := strings.ToLower(strings.TrimSpace(key))
		item, ok := allowed[normalized]
		if !ok {
			if len(allowedKeys) == 0 {
				return nil, dto.NewValidationError("dsl declares no input parameters, but dsl_params were provided")
			}
			return nil, dto.NewValidationError("unknown dsl param %q; allowed: %s", key, strings.Join(allowedKeys, ", "))
		}
		coerced, err := coerceDSLParamValue(item, raw)
		if err != nil {
			return nil, err
		}
		validated[canonicalByKey[normalized]] = coerced
	}
	if len(validated) == 0 {
		return nil, nil
	}
	return validated, nil
}

func coerceDSLParamValue(item bridge.ParamSchema, raw interface{}) (interface{}, error) {
	label := item.LookupKey()
	if strings.TrimSpace(label) == "" {
		label = item.Name
	}

	switch item.Type {
	case bridge.ParamFloat:
		value, ok := numericDSLParamValue(raw)
		if !ok {
			return nil, dto.NewValidationError("dsl param %q must be a number", label)
		}
		if err := validateDSLNumericBounds(label, value, item); err != nil {
			return nil, err
		}
		return value, nil
	case bridge.ParamInt:
		value, ok := numericDSLParamValue(raw)
		if !ok {
			return nil, dto.NewValidationError("dsl param %q must be an integer", label)
		}
		if math.Abs(value-math.Round(value)) > 1e-9 {
			return nil, dto.NewValidationError("dsl param %q must be an integer", label)
		}
		if err := validateDSLNumericBounds(label, value, item); err != nil {
			return nil, err
		}
		return int(math.Round(value)), nil
	case bridge.ParamBool:
		switch value := raw.(type) {
		case bool:
			return value, nil
		case int:
			if value == 0 || value == 1 {
				return value == 1, nil
			}
		case float64:
			if value == 0 || value == 1 {
				return value == 1, nil
			}
		}
		return nil, dto.NewValidationError("dsl param %q must be a boolean", label)
	case bridge.ParamString:
		value, ok := raw.(string)
		if !ok {
			return nil, dto.NewValidationError("dsl param %q must be a string", label)
		}
		if len(item.Options) > 0 {
			matched := false
			for _, option := range item.Options {
				if value == option {
					matched = true
					break
				}
			}
			if !matched {
				return nil, dto.NewValidationError("dsl param %q must be one of: %s", label, strings.Join(item.Options, ", "))
			}
		}
		return value, nil
	default:
		return nil, dto.NewValidationError("unsupported dsl param type for %q", label)
	}
}

func numericDSLParamValue(raw interface{}) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func validateDSLNumericBounds(label string, value float64, item bridge.ParamSchema) error {
	if item.Min != nil && value < *item.Min-1e-9 {
		return dto.NewValidationError("dsl param %q must be >= %v", label, *item.Min)
	}
	if item.Max != nil && value > *item.Max+1e-9 {
		return dto.NewValidationError("dsl param %q must be <= %v", label, *item.Max)
	}
	if item.Step != nil && *item.Step > 0 {
		base := 0.0
		if item.Min != nil {
			base = *item.Min
		}
		steps := (value - base) / *item.Step
		if math.Abs(steps-math.Round(steps)) > 1e-9 {
			return dto.NewValidationError("dsl param %q must align to step %v", label, *item.Step)
		}
	}
	return nil
}

func resolveDynamicDSLProfile(manifest bridge.Manifest, hint *dto.StrategyBacktestDSLProfile) (strategies.StrategyProfile, error) {
	profile := inferDynamicDSLProfile(manifest)
	if hint == nil {
		return profile.Normalized(), nil
	}
	if hint.UsesOptions != nil {
		profile.UsesOptions = *hint.UsesOptions
	}
	if strings.TrimSpace(hint.RegularTrade) != "" {
		switch mode := strategies.RegularTradeMode(strings.ToLower(strings.TrimSpace(hint.RegularTrade))); mode {
		case strategies.RegularTradeNone, strategies.RegularTradeSignalOnly, strategies.RegularTradeMaterial:
			profile.RegularTrade = mode
		default:
			return strategies.StrategyProfile{}, dto.NewValidationError("dsl_profile.regular_trade %q is invalid; want none|signal_only|material", hint.RegularTrade)
		}
	}
	return profile.Normalized(), nil
}

func inferDynamicDSLProfile(manifest bridge.Manifest) strategies.StrategyProfile {
	profile := strategies.StrategyProfile{UsesOptions: manifest.UsesOptions}
	switch {
	case manifest.UsesOptions && manifest.UsesRegularOrders:
		profile.RegularTrade = strategies.RegularTradeSignalOnly
	case manifest.UsesOptions:
		profile.RegularTrade = strategies.RegularTradeNone
	default:
		profile.RegularTrade = strategies.RegularTradeMaterial
	}
	return profile.Normalized()
}

func backtestDSLConfigMap(cfg strategies.Config, req dto.StrategyBacktestRunRequest) map[string]interface{} {
	return configmap.FromStrategyConfig(cfg, backtestDSLPortfolioItems(req))
}

func backtestDSLPortfolioItems(req dto.StrategyBacktestRunRequest) []configmap.PortfolioItem {
	primary := resolvePrimaryBacktestAsset(req)
	items := make([]configmap.PortfolioItem, 0, len(req.Portfolio)+len(req.Symbols)+1)
	if primary != "" && len(req.Portfolio) == 0 && len(req.Symbols) == 0 {
		items = append(items, configmap.PortfolioItem{Symbol: primary, Weight: 1})
	}
	for _, leg := range req.Portfolio {
		items = append(items, configmap.PortfolioItem{Market: leg.Market, Symbol: leg.Asset, Weight: leg.Weight})
	}
	for index, symbol := range req.Symbols {
		weight := 0.0
		if index < len(req.Weights) {
			weight = req.Weights[index]
		}
		items = append(items, configmap.PortfolioItem{Symbol: symbol, Weight: weight})
	}
	return items
}

func describeResolvedStrategies(items []strategies.ResolvedStrategy, fallback string) string {
	if len(items) == 1 {
		name := resolvedStrategyName(items[0])
		if name != "" {
			return name
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return fmt.Sprintf("%d strategies", len(items))
}

func resolvedStrategyName(item strategies.ResolvedStrategy) string {
	if name := strings.TrimSpace(item.Runtime.DisplayName); name != "" {
		return name
	}
	if item.Strategy != nil {
		if name := strings.TrimSpace(item.Strategy.Name()); name != "" {
			return name
		}
	}
	return strings.TrimSpace(item.CanonicalName)
}

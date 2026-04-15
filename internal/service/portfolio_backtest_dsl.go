package service

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/ast"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
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

	baseOpts := bridge.Options{
		SignalSource: strings.TrimSpace(req.SignalSource),
		Config:       backtestDSLConfigMap(cfg),
	}
	parsed := bridge.NewWithOptions(dslSource, baseOpts)
	if errs := parsed.ParseErrors(); len(errs) > 0 {
		return strategies.ResolvedStrategy{}, dto.NewValidationError("invalid dsl: %s", strings.Join(errs, "; "))
	}

	validatedParams, err := validateBacktestDSLParams(parsed.ParamSchema(), params)
	if err != nil {
		return strategies.ResolvedStrategy{}, err
	}
	profile, err := resolveDynamicDSLProfile(dslSource, req.DSLProfile)
	if err != nil {
		return strategies.ResolvedStrategy{}, err
	}

	strategy := bridge.NewWithOptions(dslSource, bridge.Options{
		SignalSource: strings.TrimSpace(req.SignalSource),
		Params:       validatedParams,
		Config:       backtestDSLConfigMap(cfg),
	})
	if errs := strategy.ParseErrors(); len(errs) > 0 {
		return strategies.ResolvedStrategy{}, dto.NewValidationError("invalid dsl: %s", strings.Join(errs, "; "))
	}

	canonicalName := slugify(strategy.Name())
	if canonicalName == "" {
		canonicalName = "dynamic-dsl"
	}
	return strategies.ResolvedStrategy{
		CanonicalName: canonicalName,
		Strategy:      strategy,
		Profile:       profile,
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

func resolveDynamicDSLProfile(source string, hint *dto.StrategyBacktestDSLProfile) (strategies.StrategyProfile, error) {
	profile := inferDynamicDSLProfile(source)
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

func inferDynamicDSLProfile(source string) strategies.StrategyProfile {
	program, errs := parser.Parse(source)
	if len(errs) > 0 || program == nil {
		return inferDynamicDSLProfileFallback(source)
	}

	analysis := analyzeDynamicDSLProgram(program)

	profile := strategies.StrategyProfile{UsesOptions: analysis.usesOptions}
	switch {
	case analysis.usesOptions && analysis.usesRegularTrades:
		profile.RegularTrade = strategies.RegularTradeSignalOnly
	case analysis.usesOptions:
		profile.RegularTrade = strategies.RegularTradeNone
	default:
		profile.RegularTrade = strategies.RegularTradeMaterial
	}
	return profile.Normalized()
}

func inferDynamicDSLProfileFallback(source string) strategies.StrategyProfile {
	lower := strings.ToLower(source)
	usesOptions := containsAny(lower, "options.", "spread.", "leg.", "contract.")
	usesRegularTrades := containsAny(lower, "buy(", "sell(", "strategy.entry", "strategy.close", "strategy.exit", "strategy.order")

	profile := strategies.StrategyProfile{UsesOptions: usesOptions}
	switch {
	case usesOptions && usesRegularTrades:
		profile.RegularTrade = strategies.RegularTradeSignalOnly
	case usesOptions:
		profile.RegularTrade = strategies.RegularTradeNone
	default:
		profile.RegularTrade = strategies.RegularTradeMaterial
	}
	return profile.Normalized()
}

type dynamicDSLProgramAnalysis struct {
	usesOptions       bool
	usesRegularTrades bool
}

func analyzeDynamicDSLProgram(program *ast.Program) dynamicDSLProgramAnalysis {
	var analysis dynamicDSLProgramAnalysis
	if program == nil {
		return analysis
	}
	for _, stmt := range program.Stmts {
		walkDynamicDSLStmt(stmt, &analysis)
	}
	return analysis
}

func walkDynamicDSLStmt(stmt ast.Stmt, analysis *dynamicDSLProgramAnalysis) {
	if stmt == nil || analysis == nil {
		return
	}
	switch node := stmt.(type) {
	case *ast.StrategyDecl:
		for _, arg := range node.Args {
			walkDynamicDSLExpr(arg.Value, analysis)
		}
	case *ast.InputDecl:
		for _, arg := range node.Args {
			walkDynamicDSLExpr(arg.Value, analysis)
		}
	case *ast.VarDecl:
		walkDynamicDSLExpr(node.Value, analysis)
	case *ast.AssignStmt:
		walkDynamicDSLExpr(node.Value, analysis)
	case *ast.IndexAssignStmt:
		walkDynamicDSLExpr(node.Left, analysis)
		walkDynamicDSLExpr(node.Index, analysis)
		walkDynamicDSLExpr(node.Value, analysis)
	case *ast.TupleAssign:
		walkDynamicDSLExpr(node.Value, analysis)
	case *ast.ExprStmt:
		walkDynamicDSLExpr(node.Expression, analysis)
	case *ast.IfStmt:
		walkDynamicDSLExpr(node.Condition, analysis)
		walkDynamicDSLBlock(node.Body, analysis)
		for _, branch := range node.ElseIfs {
			walkDynamicDSLExpr(branch.Condition, analysis)
			walkDynamicDSLBlock(branch.Body, analysis)
		}
		walkDynamicDSLBlock(node.Else, analysis)
	case *ast.ForStmt:
		walkDynamicDSLExpr(node.Start, analysis)
		walkDynamicDSLExpr(node.End, analysis)
		walkDynamicDSLExpr(node.Step, analysis)
		walkDynamicDSLBlock(node.Body, analysis)
	case *ast.ForInStmt:
		walkDynamicDSLExpr(node.Collection, analysis)
		walkDynamicDSLBlock(node.Body, analysis)
	case *ast.WhileStmt:
		walkDynamicDSLExpr(node.Condition, analysis)
		walkDynamicDSLBlock(node.Body, analysis)
	case *ast.SwitchStmt:
		walkDynamicDSLExpr(node.Tag, analysis)
		for _, switchCase := range node.Cases {
			walkDynamicDSLExpr(switchCase.Value, analysis)
			walkDynamicDSLBlock(switchCase.Body, analysis)
		}
		walkDynamicDSLBlock(node.Default, analysis)
	case *ast.FnDecl:
		for _, param := range node.Params {
			walkDynamicDSLExpr(param.Default, analysis)
		}
		walkDynamicDSLBlock(node.Body, analysis)
	case *ast.ReturnStmt:
		walkDynamicDSLExpr(node.Value, analysis)
	case *ast.Block:
		walkDynamicDSLBlock(node, analysis)
	}
}

func walkDynamicDSLBlock(block *ast.Block, analysis *dynamicDSLProgramAnalysis) {
	if block == nil {
		return
	}
	for _, stmt := range block.Stmts {
		walkDynamicDSLStmt(stmt, analysis)
	}
}

func walkDynamicDSLExpr(expr ast.Expr, analysis *dynamicDSLProgramAnalysis) {
	if expr == nil || analysis == nil {
		return
	}
	switch node := expr.(type) {
	case *ast.BinaryExpr:
		walkDynamicDSLExpr(node.Left, analysis)
		walkDynamicDSLExpr(node.Right, analysis)
	case *ast.UnaryExpr:
		walkDynamicDSLExpr(node.Operand, analysis)
	case *ast.CallExpr:
		name := dynamicDSLQualifiedName(node.Callee)
		if isDynamicDSLOptionsReference(name) {
			analysis.usesOptions = true
		}
		if isDynamicDSLRegularTradeCall(name) {
			analysis.usesRegularTrades = true
		}
		walkDynamicDSLExpr(node.Callee, analysis)
		for _, arg := range node.Args {
			walkDynamicDSLExpr(arg.Value, analysis)
		}
	case *ast.DotExpr:
		name := dynamicDSLQualifiedName(node)
		if isDynamicDSLOptionsReference(name) {
			analysis.usesOptions = true
		}
		walkDynamicDSLExpr(node.Object, analysis)
	case *ast.IndexExpr:
		walkDynamicDSLExpr(node.Left, analysis)
		walkDynamicDSLExpr(node.Index, analysis)
	case *ast.TernaryExpr:
		walkDynamicDSLExpr(node.Condition, analysis)
		walkDynamicDSLExpr(node.Then, analysis)
		walkDynamicDSLExpr(node.Else, analysis)
	case *ast.ArrayLit:
		for _, element := range node.Elements {
			walkDynamicDSLExpr(element, analysis)
		}
	case *ast.LambdaExpr:
		walkDynamicDSLExpr(node.Body, analysis)
	}
}

func dynamicDSLQualifiedName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.IdentExpr:
		return strings.ToLower(strings.TrimSpace(node.Name))
	case *ast.DotExpr:
		left := dynamicDSLQualifiedName(node.Object)
		field := strings.ToLower(strings.TrimSpace(node.Field))
		if left == "" {
			return field
		}
		if field == "" {
			return left
		}
		return left + "." + field
	default:
		return ""
	}
}

func isDynamicDSLOptionsReference(name string) bool {
	switch {
	case strings.HasPrefix(name, "options."), strings.HasPrefix(name, "spread."), strings.HasPrefix(name, "leg."), strings.HasPrefix(name, "contract."):
		return true
	default:
		return false
	}
}

func isDynamicDSLRegularTradeCall(name string) bool {
	switch name {
	case "buy", "sell", "strategy.entry", "strategy.close", "strategy.exit", "strategy.order":
		return true
	default:
		return false
	}
}

func containsAny(source string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(source, needle) {
			return true
		}
	}
	return false
}

func backtestDSLConfigMap(cfg strategies.Config) map[string]interface{} {
	config := make(map[string]interface{})
	if cfg.FastPeriod != 0 {
		config["fast_period"] = cfg.FastPeriod
	}
	if cfg.SlowPeriod != 0 {
		config["slow_period"] = cfg.SlowPeriod
	}
	if cfg.MAPeriod != 0 {
		config["ma_period"] = cfg.MAPeriod
	}
	if cfg.PThreshold != 0 {
		config["p_threshold"] = cfg.PThreshold
	}
	if cfg.PositionSize != 0 {
		config["position_size"] = cfg.PositionSize
	}
	if cfg.EntryTWAPBars != 0 {
		config["entry_twap_bars"] = cfg.EntryTWAPBars
	}
	if cfg.TargetExpiryDays != 0 {
		config["target_expiry_days"] = cfg.TargetExpiryDays
	}
	if cfg.MinExpiryDays != 0 {
		config["min_expiry_days"] = cfg.MinExpiryDays
	}
	if cfg.MinPremium != 0 {
		config["min_premium"] = cfg.MinPremium
	}
	if cfg.ShortDeltaMin != 0 {
		config["short_delta_min"] = cfg.ShortDeltaMin
	}
	if cfg.ShortDeltaMax != 0 {
		config["short_delta_max"] = cfg.ShortDeltaMax
	}
	if cfg.LongDeltaMin != 0 {
		config["long_delta_min"] = cfg.LongDeltaMin
	}
	if cfg.LongDeltaMax != 0 {
		config["long_delta_max"] = cfg.LongDeltaMax
	}
	if cfg.MaxHoldTime != 0 {
		config["max_hold_hours"] = cfg.MaxHoldTime.Hours()
	}
	if cfg.Direction != "" {
		config["direction"] = string(cfg.Direction)
	}
	config["entry_price_mode"] = int(cfg.EntryPriceMode)
	config["exit_price_mode"] = int(cfg.ExitPriceMode)
	config["valuation_price_mode"] = int(cfg.ValuationPriceMode)
	return config
}

func describeResolvedStrategies(items []strategies.ResolvedStrategy, fallback string) string {
	if len(items) == 1 {
		name := strings.TrimSpace(items[0].Strategy.Name())
		if name != "" {
			return name
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return fmt.Sprintf("%d strategies", len(items))
}

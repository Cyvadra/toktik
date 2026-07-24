package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/analysis"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

type resolvedBacktestPlan struct {
	from             time.Time
	to               time.Time
	asset            string
	interval         string
	portfolioSymbols []string
	universeSymbols  []string
	universeCodes    []string
	universe         *bridge.UniverseSnapshot
	universeCoverage []dto.StrategyBacktestUniverseCoverage
	minDTE           int
	targetDTE        int
	strategyLabel    string
	profileSource    string
	warnings         []string
	primaryMarket    marketSpec
	tradeScope       instrumentScope
	resolved         []strategies.ResolvedStrategy
	commissionModel  backtest.CommissionModel
	chainProvider    backtest.OptionsChainProvider
	chainTargets     []optionChainTarget
	runtimeWarnings  []dto.StrategyBacktestRuntimeWarning
}

type optionChainTarget struct {
	market   string
	asset    string
	weight   float64
	required bool
}

func (s *PortfolioBacktestService) resolveBacktestPlan(ctx context.Context, run *portfolioBacktestRun, req dto.StrategyBacktestRunRequest, loadChains bool) (*resolvedBacktestPlan, error) {
	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	if err := validateStrategyBacktestRunRequest(req); err != nil {
		return nil, err
	}

	strategyCfg, err := buildBacktestStrategyConfig(req)
	if err != nil {
		return nil, err
	}

	primaryMarket, err := parsePrimaryMarket(defaultString(req.Market, marketCrypto))
	if err != nil {
		return nil, err
	}
	tradeScope, err := parseInstrumentScope(defaultString(req.Instrument, string(instrumentAuto)))
	if err != nil {
		return nil, err
	}
	asset := resolvePrimaryBacktestAsset(req)
	if asset == "" && strings.TrimSpace(req.DSL) == "" {
		return nil, dto.NewValidationError("asset is required unless portfolio or symbols are provided")
	}
	interval := defaultString(req.Interval, "1h")
	strategyAsset := asset
	if strategyAsset == "" {
		strategyAsset = "UNIVERSE"
	}
	resolved, strategyLabel, err := resolveRequestedStrategies(req, strategyCfg, strategyAsset)
	if err != nil {
		return nil, err
	}
	if err := validatePreloadableDSLRequests(resolved); err != nil {
		return nil, err
	}
	injectedConfig := make(map[string]interface{})
	var universeSymbols []string
	var universeCodes []string
	var universe *bridge.UniverseSnapshot
	universeSymbols, universeCodes, universe, err = s.resolveDSLUniverses(ctx, req, resolved, from, to)
	if err != nil {
		return nil, err
	}
	if asset == "" && len(universeSymbols) > 0 {
		asset = universeSymbols[0]
	}
	if len(universeCodes) > 0 {
		if universe != nil {
			strategyCfg.UniverseProvider = universe.Provider
			strategyCfg.UniverseMembers = universe.Members
		}
		resolved, strategyLabel, err = resolveRequestedStrategiesWithConfig(req, strategyCfg, asset, injectedConfig, universe)
		if err != nil {
			return nil, err
		}
		if err := validatePreloadableDSLRequests(resolved); err != nil {
			return nil, err
		}
	}
	if asset == "" {
		return nil, dto.NewValidationError("asset is required unless portfolio, symbols, or a non-empty universe are provided")
	}
	if err := validateInstrumentScope(tradeScope, resolved); err != nil {
		return nil, err
	}
	if err := validateMarketStrategyCompatibility(primaryMarket, resolved); err != nil {
		return nil, err
	}

	commissionModel, err := parseCommissionModel(defaultString(req.CommissionModel, "none"))
	if err != nil {
		return nil, err
	}

	var chainProvider backtest.OptionsChainProvider
	var targets []optionChainTarget
	var runtimeWarnings []dto.StrategyBacktestRuntimeWarning
	if shouldLoadOptionChain(tradeScope, resolved) {
		var targetSymbols []string
		targets, targetSymbols, err = collectOptionChainTargets(req, primaryMarket.name, asset, universeSymbols, resolved)
		if err != nil {
			return nil, err
		}
		if loadChains && run != nil {
			run.setProgress(&dto.StrategyBacktestProgress{
				Phase:     string(backtest.ProgressPhasePrepare),
				Current:   0,
				Total:     maxInt(len(targets), 1),
				Percent:   0,
				Message:   fmt.Sprintf("loading options chain for %s [%s/%s]", strings.Join(targetSymbols, ","), primaryMarket.name, interval),
				StartedAt: derefTime(run.startedAt),
				Timestamp: s.now().UTC(),
			})
		}
		if loadChains {
			chainProvider, runtimeWarnings, err = s.loadOptionChainUniverse(ctx, run, interval, from, to, targets)
			if err != nil {
				return nil, fmt.Errorf("load options chain: %w", err)
			}
		}
	}

	return &resolvedBacktestPlan{
		from:             from,
		to:               to,
		asset:            asset,
		interval:         interval,
		portfolioSymbols: collectPortfolioSymbols(req, asset),
		universeSymbols:  universeSymbols,
		universeCodes:    universeCodes,
		universe:         universe,
		minDTE:           req.MinExpiryDays,
		targetDTE:        req.TargetExpiryDays,
		strategyLabel:    strategyLabel,
		profileSource:    validationProfileSource(req),
		warnings:         validationWarnings(req),
		primaryMarket:    primaryMarket,
		tradeScope:       tradeScope,
		resolved:         resolved,
		commissionModel:  commissionModel,
		chainProvider:    chainProvider,
		chainTargets:     targets,
		runtimeWarnings:  runtimeWarnings,
	}, nil
}

func resolveRequestedStrategiesWithConfig(req dto.StrategyBacktestRunRequest, cfg strategies.Config, asset string, injectedConfig map[string]interface{}, universe *bridge.UniverseSnapshot) ([]strategies.ResolvedStrategy, string, error) {
	if strings.TrimSpace(req.DSL) == "" {
		return resolveRequestedStrategies(req, cfg, asset)
	}
	resolved, err := buildDynamicDSLResolvedStrategyWithConfig(req, cfg, injectedConfig, universe)
	if err != nil {
		return nil, "", err
	}
	return []strategies.ResolvedStrategy{resolved}, resolved.CanonicalName, nil
}

func (s *PortfolioBacktestService) resolveDSLUniverses(ctx context.Context, req dto.StrategyBacktestRunRequest, resolved []strategies.ResolvedStrategy, from, to time.Time) ([]string, []string, *bridge.UniverseSnapshot, error) {
	if s.universes == nil {
		for _, item := range resolved {
			if ds, ok := item.Strategy.(*bridge.DslStrategy); ok && len(ds.Manifest().UniverseRequests()) > 0 {
				return nil, nil, nil, dto.NewValidationError("dsl uses universe.symbols but universe service is not configured")
			}
		}
		return nil, nil, nil, nil
	}
	seenSymbols := make(map[string]struct{})
	seenMembers := make(map[string]map[string]struct{})
	membersByCode := make(map[string][]string)
	intervalsByCode := make(map[string][]dto.UniverseMember)
	symbols := make([]string, 0)
	seenCodes := make(map[string]struct{})
	codes := make([]string, 0)
	addUniverse := func(code, market string) error {
		code = normalizeUniverseCode(code)
		if code == "" {
			return nil
		}
		if _, ok := seenCodes[code]; ok {
			return nil
		}
		resp, err := s.universes.MemberIntervals(ctx, dto.UniverseMembersRequest{Market: market, Code: code, From: from, To: to, Limit: maxUniverseIntervalMembers})
		if err != nil {
			return err
		}
		if seenMembers[code] == nil {
			seenMembers[code] = make(map[string]struct{})
		}
		intervalsByCode[code] = append([]dto.UniverseMember(nil), resp.Data...)
		for _, member := range resp.Data {
			symbol := normalizeSymbol(member.Symbol)
			if symbol == "" {
				continue
			}
			if _, ok := seenSymbols[symbol]; !ok {
				seenSymbols[symbol] = struct{}{}
				symbols = append(symbols, symbol)
			}
			if _, ok := seenMembers[code][symbol]; !ok {
				seenMembers[code][symbol] = struct{}{}
				membersByCode[code] = append(membersByCode[code], symbol)
			}
		}
		seenCodes[code] = struct{}{}
		codes = append(codes, code)
		return nil
	}
	if strings.TrimSpace(req.UniverseCode) != "" {
		if err := addUniverse(req.UniverseCode, req.UniverseMarket); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, item := range resolved {
		dslStrategy, ok := item.Strategy.(*bridge.DslStrategy)
		if !ok {
			continue
		}
		manifest := dslStrategy.Manifest()
		if manifest.HasDynamicUniverseRequest() {
			return nil, nil, nil, dto.NewValidationError("dsl uses dynamic universe.symbols arguments; use literal universe codes so membership can be expanded before replay")
		}
		for _, universeReq := range manifest.UniverseRequests() {
			if err := addUniverse(universeReq.Code, req.UniverseMarket); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	if len(codes) == 0 {
		return symbols, codes, nil, nil
	}
	provider := &UniverseIntervalProvider{members: intervalsByCode}
	return symbols, codes, &bridge.UniverseSnapshot{Provider: provider, Members: membersByCode}, nil
}

func buildBacktestStrategyConfig(req dto.StrategyBacktestRunRequest) (strategies.Config, error) {
	strategyCfg := strategies.DefaultConfig()
	strategyCfg.PositionSize = req.PositionSize
	strategyCfg.MaxHoldTime = time.Duration(req.MaxHoldHours * float64(time.Hour))
	strategyCfg.TargetExpiryDays = req.TargetExpiryDays
	strategyCfg.MinExpiryDays = req.MinExpiryDays
	strategyCfg.MinPremium = req.MinPremium
	strategyCfg.ShortDeltaMin = req.ShortDeltaMin
	strategyCfg.ShortDeltaMax = req.ShortDeltaMax
	strategyCfg.LongDeltaMin = req.LongDeltaMin
	strategyCfg.LongDeltaMax = req.LongDeltaMax
	strategyCfg.SignalSource = strings.TrimSpace(req.SignalSource)
	var err error
	strategyCfg.EntryPriceMode, err = parseOptionPriceMode(defaultString(req.SpreadEntryPriceMode, "mark_close"), "spread_entry_price_mode")
	if err != nil {
		return strategies.Config{}, err
	}
	strategyCfg.ExitPriceMode, err = parseOptionPriceMode(defaultString(req.SpreadExitPriceMode, "mark_close"), "spread_exit_price_mode")
	if err != nil {
		return strategies.Config{}, err
	}
	strategyCfg.ValuationPriceMode, err = parseOptionPriceMode(defaultString(req.SpreadValuationPriceMode, "mark_close"), "spread_valuation_price_mode")
	if err != nil {
		return strategies.Config{}, err
	}
	strategyCfg.MAPeriod = req.MAPeriod
	strategyCfg.PThreshold = req.PThreshold
	strategyCfg.Direction, err = parseTradeDirection(defaultString(req.Direction, "both"))
	if err != nil {
		return strategies.Config{}, err
	}
	return strategyCfg, nil
}

func resolvePrimaryBacktestAsset(req dto.StrategyBacktestRunRequest) string {
	if asset := strings.ToUpper(strings.TrimSpace(req.Asset)); asset != "" {
		return asset
	}
	for _, leg := range req.Portfolio {
		if asset := strings.ToUpper(strings.TrimSpace(leg.Asset)); asset != "" {
			return asset
		}
	}
	for _, symbol := range req.Symbols {
		if symbol = strings.ToUpper(strings.TrimSpace(symbol)); symbol != "" {
			return symbol
		}
	}
	return ""
}

func collectPortfolioSymbols(req dto.StrategyBacktestRunRequest, primaryAsset string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(req.Portfolio)+len(req.Symbols)+1)
	appendSymbol := func(symbol string) {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			return
		}
		if _, ok := seen[symbol]; ok {
			return
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	appendSymbol(primaryAsset)
	for _, leg := range req.Portfolio {
		appendSymbol(leg.Asset)
	}
	for _, symbol := range req.Symbols {
		appendSymbol(symbol)
	}
	return out
}

func mergeSymbols(existing, extra []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	appendSymbol := func(symbol string) {
		symbol = normalizeSymbol(symbol)
		if symbol == "" {
			return
		}
		if _, ok := seen[symbol]; ok {
			return
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	for _, symbol := range existing {
		appendSymbol(symbol)
	}
	for _, symbol := range extra {
		appendSymbol(symbol)
	}
	return out
}

func collectOptionChainTargets(req dto.StrategyBacktestRunRequest, primaryMarket, primaryAsset string, universeSymbols []string, resolved []strategies.ResolvedStrategy) ([]optionChainTarget, []string, error) {
	seen := make(map[string]optionChainTarget)
	ordered := make([]string, 0, len(req.Portfolio)+len(req.Symbols)+len(resolved)+1)
	add := func(rawMarket, rawAsset string, weight float64, required bool) error {
		asset := strings.ToUpper(strings.TrimSpace(rawAsset))
		if asset == "" {
			return nil
		}
		marketName := strings.TrimSpace(rawMarket)
		if marketName == "" {
			marketName = primaryMarket
		}
		marketSpec, err := parsePrimaryMarket(marketName)
		if err != nil {
			return err
		}
		key := backtest.ChainLookupKey(marketSpec.name, asset)
		if _, ok := seen[key]; !ok {
			ordered = append(ordered, key)
		}
		target := seen[key]
		target.market = marketSpec.name
		target.asset = asset
		target.weight = weight
		target.required = target.required || required
		seen[key] = target
		return nil
	}
	primaryExplicit := strings.TrimSpace(req.Asset) != "" || len(req.Portfolio) > 0 || len(req.Symbols) > 0
	if err := add(primaryMarket, primaryAsset, 1, primaryExplicit); err != nil {
		return nil, nil, err
	}
	for _, leg := range req.Portfolio {
		if err := add(leg.Market, leg.Asset, leg.Weight, true); err != nil {
			return nil, nil, err
		}
	}
	for index, symbol := range req.Symbols {
		weight := 0.0
		if index < len(req.Weights) {
			weight = req.Weights[index]
		}
		if err := add(primaryMarket, symbol, weight, true); err != nil {
			return nil, nil, err
		}
	}
	for _, symbol := range universeSymbols {
		if err := add(primaryMarket, symbol, 0, false); err != nil {
			return nil, nil, err
		}
	}
	for _, item := range resolved {
		dslStrategy, ok := item.Strategy.(*bridge.DslStrategy)
		if !ok {
			continue
		}
		manifest := dslStrategy.Manifest()
		if manifest.HasDynamicOptionChainRequest() && len(req.Portfolio) == 0 && len(req.Symbols) == 0 && len(universeSymbols) == 0 {
			return nil, nil, dto.NewValidationError("dsl uses dynamic options.chain arguments; provide symbols or portfolio so option chains can be preloaded")
		}
		for _, chainReq := range dslStrategy.OptionChainRequests() {
			if err := add(chainReq.Market, chainReq.Symbol, 0, true); err != nil {
				return nil, nil, err
			}
		}
	}
	targets := make([]optionChainTarget, 0, len(ordered))
	symbols := make([]string, 0, len(ordered))
	for _, key := range ordered {
		target := seen[key]
		targets = append(targets, target)
		symbols = append(symbols, target.asset)
	}
	return targets, symbols, nil
}

func (s *PortfolioBacktestService) preflightBacktestPlan(ctx context.Context, run *portfolioBacktestRun, plan *resolvedBacktestPlan) error {
	if plan == nil {
		return nil
	}
	coverageChecked := false
	for index, item := range plan.resolved {
		if run != nil {
			total := len(plan.resolved)
			run.setProgress(&dto.StrategyBacktestProgress{
				Phase:     string(backtest.ProgressPhasePrepare),
				Current:   index,
				Total:     total,
				Percent:   float64(index) / float64(maxInt(total, 1)) * 100,
				Message:   fmt.Sprintf("preflight prepare for %s", strings.TrimSpace(item.Strategy.Name())),
				StartedAt: derefTime(run.startedAt),
				Timestamp: s.now().UTC(),
			})
		}
		capitalProfile := resolveCapitalProfile(plan.primaryMarket, item.Profile, plan.asset)
		strategy, err := item.NewStrategy()
		if err != nil {
			return fmt.Errorf("build strategy %s: %w", resolvedStrategyName(item), err)
		}
		engine := s.engineBuilder(backtest.Config{
			InitialCapital:  1,
			AccountUnit:     capitalProfile.unit,
			CommissionModel: plan.commissionModel,
			CommissionValue: 0,
			SlippagePct:     0,
			ExecutionMode:   backtest.ExecutionPriceCanonical,
			ValuationMode:   backtest.ValuationPriceClose,
			TriggerMode:     backtest.TriggerPriceCanonical,
		}, plan.chainProvider, item.Profile.UsesOptions)
		prepared, err := engine.Prepare(ctx, plan.primaryMarket.underlyingFeed, plan.asset, plan.interval, plan.from, plan.to, strategy, nil)
		if err != nil {
			if errors.Is(err, backtest.ErrUnknownIndicatorSeries) {
				return dto.NewValidationError("prepare strategy %s: %v", strategy.Name(), err)
			}
			return fmt.Errorf("prepare strategy %s: %w", strategy.Name(), err)
		}
		if !coverageChecked && len(plan.universeCodes) > 0 {
			if prepared == nil || prepared.PrimaryDS == nil {
				return fmt.Errorf("prepare strategy %s: primary replay data is unavailable", strategy.Name())
			}
			coverage, warnings, err := validateUniverseReplayCoverage(plan.universe, plan.universeCodes, prepared.PrimaryDS.Timestamps)
			if err != nil {
				return err
			}
			plan.universeCoverage = coverage
			plan.warnings = append(plan.warnings, warnings...)
			coverageChecked = true
		}
	}
	return nil
}

func validateUniverseReplayCoverage(snapshot *bridge.UniverseSnapshot, codes []string, timestamps []time.Time) ([]dto.StrategyBacktestUniverseCoverage, []string, error) {
	if len(codes) == 0 {
		return nil, nil, nil
	}
	if snapshot == nil || snapshot.Provider == nil {
		return nil, nil, dto.NewValidationError("dsl uses universe.symbols but runtime universe provider is unavailable")
	}
	coverage := make([]dto.StrategyBacktestUniverseCoverage, 0, len(codes))
	warnings := make([]string, 0)
	for _, code := range codes {
		item := dto.StrategyBacktestUniverseCoverage{Code: code, ReplayBars: len(timestamps)}
		for index, ts := range timestamps {
			memberCount := len(snapshot.Provider.SymbolsAt(code, ts))
			if index == 0 || memberCount < item.MinMembersPerBar {
				item.MinMembersPerBar = memberCount
			}
			if memberCount > item.MaxMembersPerBar {
				item.MaxMembersPerBar = memberCount
			}
			if memberCount == 0 {
				continue
			}
			item.BarsWithMembers++
			coveredDate := ts.UTC().Format("2006-01-02")
			if item.FirstCoveredDate == "" {
				item.FirstCoveredDate = coveredDate
			}
			item.LastCoveredDate = coveredDate
		}
		if item.ReplayBars > 0 && item.BarsWithMembers == 0 {
			return nil, nil, dto.NewValidationError("universe %q has no members on any of the %d replay bars", code, item.ReplayBars)
		}
		if item.BarsWithMembers < item.ReplayBars {
			warnings = append(warnings, fmt.Sprintf("universe %q has members on %d of %d replay bars", code, item.BarsWithMembers, item.ReplayBars))
		}
		coverage = append(coverage, item)
	}
	return coverage, warnings, nil
}

// classifyDSLRequests is the single pass over each resolved DSL strategy's
// manifest that both validatePreloadableDSLRequests (hard-fail gate) and
// buildStrategyBacktestResourcePlan (informational counts) rely on, so the
// definition of "preloadable" only lives in one place.
func classifyDSLRequests(resolved []strategies.ResolvedStrategy) (staticRequests, dynamicRequests int, firstDynamicKind string) {
	for _, item := range resolved {
		dslStrategy, ok := item.Strategy.(*bridge.DslStrategy)
		if !ok {
			continue
		}
		staticRequests += dslStrategy.ConcreteDataRequestCount()
		for _, request := range dslStrategy.Manifest().Requests {
			if request.Kind != "security" && request.Kind != "factor" && request.Kind != "fundamental" {
				continue
			}
			if request.IsUniverseExpanded() {
				continue
			}
			if request.Dynamic || request.Key == "" {
				dynamicRequests++
				if firstDynamicKind == "" {
					firstDynamicKind = request.Kind
				}
			}
		}
	}
	return staticRequests, dynamicRequests, firstDynamicKind
}

func validatePreloadableDSLRequests(resolved []strategies.ResolvedStrategy) error {
	_, dynamicRequests, firstDynamicKind := classifyDSLRequests(resolved)
	if dynamicRequests == 0 {
		return nil
	}
	return dto.NewValidationError("dsl %s uses runtime-dynamic arguments; this request cannot be preloaded deterministically", requestDiagnosticName(firstDynamicKind))
}

func requestDiagnosticName(kind string) string {
	return analysis.RequestDiagnosticFunction(kind)
}

func buildStrategyBacktestValidationResponse(plan *resolvedBacktestPlan) *dto.StrategyBacktestValidationResponse {
	if plan == nil {
		return &dto.StrategyBacktestValidationResponse{Strategies: []dto.StrategyBacktestValidationItem{}}
	}
	items := make([]dto.StrategyBacktestValidationItem, 0, len(plan.resolved))
	for _, item := range plan.resolved {
		runtime := buildStrategyBacktestValidationRuntime(plan, item)
		entry := dto.StrategyBacktestValidationItem{
			CanonicalName: item.CanonicalName,
			DisplayName:   item.Strategy.Name(),
			ProfileLabel:  item.Profile.Label(),
			ProfileSource: plan.profileSource,
			UsesOptions:   item.Profile.UsesOptions,
			RegularTrade:  string(item.Profile.RegularTrade),
			Runtime:       &runtime,
			Warnings:      sliceOrEmpty(append([]string(nil), plan.warnings...)),
		}
		if ds, ok := item.Strategy.(*bridge.DslStrategy); ok {
			schema := ds.ParamSchema()
			entry.DSLParams = make([]dto.StrategyBacktestDSLParam, 0, len(schema))
			for _, param := range schema {
				entry.DSLParams = append(entry.DSLParams, dto.StrategyBacktestDSLParam{
					Name:    param.Name,
					Title:   param.Title,
					Type:    string(param.Type),
					Default: param.Default,
					Min:     param.Min,
					Max:     param.Max,
					Step:    param.Step,
					Options: sliceOrEmpty(param.Options),
				})
			}
			entry.DSLDiagnostics = dslDiagnosticsToDTO(ds.Diagnostics())
			for _, diagnostic := range ds.Diagnostics() {
				if diagnostic.Severity == diagnostics.SeverityWarning || diagnostic.Severity == diagnostics.SeverityError {
					entry.Warnings = append(entry.Warnings, diagnostic.String())
				}
			}
		}
		items = append(items, entry)
	}
	return &dto.StrategyBacktestValidationResponse{
		StrategyLabel: describeResolvedStrategies(plan.resolved, plan.strategyLabel),
		StrategyCount: len(items),
		Strategies:    sliceOrEmpty(items),
		ResourcePlan:  buildStrategyBacktestResourcePlan(plan),
	}
}

func buildStrategyBacktestResourcePlan(plan *resolvedBacktestPlan) *dto.StrategyBacktestResourcePlan {
	if plan == nil {
		return nil
	}
	warnings := append([]string(nil), plan.warnings...)
	staticRequests, runtimeDynamicRequests, _ := classifyDSLRequests(plan.resolved)
	if len(plan.universeCodes) > 0 && len(plan.universeSymbols) == 0 {
		warnings = append(warnings, "universe resolved to zero symbols")
	}
	if runtimeDynamicRequests > 0 {
		warnings = append(warnings, fmt.Sprintf("%d DSL data request(s) use runtime-dynamic arguments and cannot use the static preload path", runtimeDynamicRequests))
	}
	estimatedContracts := 0
	if len(plan.chainTargets) > 0 {
		estimatedContracts = len(plan.chainTargets) * 200
	}
	return &dto.StrategyBacktestResourcePlan{
		UniverseSize:           len(plan.universeSymbols),
		UniverseCodes:          sliceOrEmpty(append([]string(nil), plan.universeCodes...)),
		UniverseCoverage:       sliceOrEmpty(append([]dto.StrategyBacktestUniverseCoverage(nil), plan.universeCoverage...)),
		OptionChainUnderlyings: len(plan.chainTargets),
		MinDTE:                 plan.minDTE,
		TargetDTE:              plan.targetDTE,
		EstimatedContracts:     estimatedContracts,
		StaticDataRequests:     staticRequests,
		RuntimeDynamicRequests: runtimeDynamicRequests,
		From:                   plan.from.Format("2006-01-02"),
		To:                     plan.to.Format("2006-01-02"),
		Interval:               plan.interval,
		Warnings:               sliceOrEmpty(warnings),
	}
}

func dslDiagnosticsToDTO(items []diagnostics.Diagnostic) []dto.StrategyBacktestDSLDiagnostic {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.StrategyBacktestDSLDiagnostic, 0, len(items))
	for _, item := range items {
		out = append(out, dto.StrategyBacktestDSLDiagnostic{
			Severity: string(item.Severity),
			Code:     item.Code,
			Message:  item.Message,
			Function: item.Function,
			BarIndex: item.BarIndex,
			Hint:     item.Hint,
		})
	}
	return out
}

func buildStrategyBacktestValidationRuntime(plan *resolvedBacktestPlan, item strategies.ResolvedStrategy) dto.StrategyBacktestValidationRuntime {
	capital := resolveCapitalProfile(plan.primaryMarket, item.Profile, plan.asset)
	optionsUnit := ""
	if item.Profile.UsesOptions {
		optionsUnit = strings.ToUpper(strings.TrimSpace(plan.asset))
		if optionsUnit == "" {
			optionsUnit = "BTC"
		}
	}
	return dto.StrategyBacktestValidationRuntime{
		Market:               plan.primaryMarket.name,
		Instrument:           string(plan.tradeScope),
		CapitalMode:          capital.mode,
		CapitalUnit:          capital.unit,
		CapitalExplanation:   capital.note,
		OptionsChainRequired: strategyNeedsValidationOptionChain(plan.tradeScope, item),
		OptionsUnit:          optionsUnit,
		RegularTradeSummary:  validationRegularTradeSummary(item.Profile),
	}
}

func strategyNeedsValidationOptionChain(scope instrumentScope, item strategies.ResolvedStrategy) bool {
	if scope == instrumentSpot {
		return false
	}
	return item.Profile.UsesOptions
}

func validationRegularTradeSummary(profile strategies.StrategyProfile) string {
	profile = profile.Normalized()
	switch profile.RegularTrade {
	case strategies.RegularTradeSignalOnly:
		return "Regular trades are treated as signal-tracking sidecars rather than primary capital exposure."
	case strategies.RegularTradeMaterial:
		return "Regular trades consume meaningful capital alongside any option legs."
	default:
		return "No regular trading leg is expected."
	}
}

func validationProfileSource(req dto.StrategyBacktestRunRequest) string {
	if strings.TrimSpace(req.DSL) == "" {
		return "registered"
	}
	if req.DSLProfile != nil {
		return "explicit"
	}
	return "inferred"
}

func validationWarnings(req dto.StrategyBacktestRunRequest) []string {
	if strings.TrimSpace(req.DSL) == "" {
		return nil
	}
	if req.DSLProfile != nil {
		return nil
	}
	return []string{"dsl_profile not provided; strategy profile was inferred from the DSL AST and may need an explicit override for scripts with indirect option helpers or atypical execution wrappers."}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
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
	strategyLabel    string
	profileSource    string
	warnings         []string
	primaryMarket    marketSpec
	tradeScope       instrumentScope
	resolved         []strategies.ResolvedStrategy
	commissionModel  backtest.CommissionModel
	chainProvider    backtest.OptionsChainProvider
}

type optionChainTarget struct {
	market string
	asset  string
	weight float64
}

func (s *PortfolioBacktestService) resolveBacktestPlan(ctx context.Context, run *portfolioBacktestRun, req dto.StrategyBacktestRunRequest) (*resolvedBacktestPlan, error) {
	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	if err := validateStrategyBacktestRunRequest(req); err != nil {
		return nil, err
	}

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
	strategyCfg.EntryPriceMode, err = parseOptionPriceMode(defaultString(req.SpreadEntryPriceMode, "mark_close"), "spread_entry_price_mode")
	if err != nil {
		return nil, err
	}
	strategyCfg.ExitPriceMode, err = parseOptionPriceMode(defaultString(req.SpreadExitPriceMode, "mark_close"), "spread_exit_price_mode")
	if err != nil {
		return nil, err
	}
	strategyCfg.ValuationPriceMode, err = parseOptionPriceMode(defaultString(req.SpreadValuationPriceMode, "mark_close"), "spread_valuation_price_mode")
	if err != nil {
		return nil, err
	}
	strategyCfg.MAPeriod = req.MAPeriod
	strategyCfg.PThreshold = req.PThreshold

	tradeDirection, err := parseTradeDirection(defaultString(req.Direction, "both"))
	if err != nil {
		return nil, err
	}
	strategyCfg.Direction = tradeDirection

	primaryMarket, err := parsePrimaryMarket(defaultString(req.Market, marketCrypto))
	if err != nil {
		return nil, err
	}
	tradeScope, err := parseInstrumentScope(defaultString(req.Instrument, string(instrumentAuto)))
	if err != nil {
		return nil, err
	}
	asset := resolvePrimaryBacktestAsset(req)
	if asset == "" {
		return nil, dto.NewValidationError("asset is required unless portfolio or symbols are provided")
	}
	interval := defaultString(req.Interval, "1h")
	resolved, strategyLabel, err := resolveRequestedStrategies(req, strategyCfg, asset)
	if err != nil {
		return nil, err
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
	if shouldLoadOptionChain(tradeScope, resolved) {
		targets, targetSymbols, err := collectOptionChainTargets(req, primaryMarket.name, asset, resolved)
		if err != nil {
			return nil, err
		}
		if run != nil {
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
		chainProvider, err = s.loadOptionChainUniverse(ctx, interval, from, to, targets)
		if err != nil {
			return nil, fmt.Errorf("load options chain: %w", err)
		}
	}

	return &resolvedBacktestPlan{
		from:             from,
		to:               to,
		asset:            asset,
		interval:         interval,
		portfolioSymbols: collectPortfolioSymbols(req, asset),
		strategyLabel:    strategyLabel,
		profileSource:    validationProfileSource(req),
		warnings:         validationWarnings(req),
		primaryMarket:    primaryMarket,
		tradeScope:       tradeScope,
		resolved:         resolved,
		commissionModel:  commissionModel,
		chainProvider:    chainProvider,
	}, nil
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

func collectOptionChainTargets(req dto.StrategyBacktestRunRequest, primaryMarket, primaryAsset string, resolved []strategies.ResolvedStrategy) ([]optionChainTarget, []string, error) {
	seen := make(map[string]optionChainTarget)
	ordered := make([]string, 0, len(req.Portfolio)+len(req.Symbols)+len(resolved)+1)
	add := func(rawMarket, rawAsset string, weight float64) error {
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
		seen[key] = optionChainTarget{market: marketSpec.name, asset: asset, weight: weight}
		return nil
	}
	if err := add(primaryMarket, primaryAsset, 1); err != nil {
		return nil, nil, err
	}
	for _, leg := range req.Portfolio {
		if err := add(leg.Market, leg.Asset, leg.Weight); err != nil {
			return nil, nil, err
		}
	}
	for index, symbol := range req.Symbols {
		weight := 0.0
		if index < len(req.Weights) {
			weight = req.Weights[index]
		}
		if err := add(primaryMarket, symbol, weight); err != nil {
			return nil, nil, err
		}
	}
	for _, item := range resolved {
		dslStrategy, ok := item.Strategy.(*bridge.DslStrategy)
		if !ok {
			continue
		}
		manifest := dslStrategy.Manifest()
		if manifest.HasDynamicOptionChainRequest() && len(req.Portfolio) == 0 && len(req.Symbols) == 0 {
			return nil, nil, dto.NewValidationError("dsl uses dynamic options.chain arguments; provide symbols or portfolio so option chains can be preloaded")
		}
		for _, chainReq := range dslStrategy.OptionChainRequests() {
			if err := add(chainReq.Market, chainReq.Symbol, 0); err != nil {
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
		if _, err := engine.Prepare(ctx, plan.primaryMarket.underlyingFeed, plan.asset, plan.interval, plan.from, plan.to, item.Strategy, nil); err != nil {
			return fmt.Errorf("prepare strategy %s: %w", item.Strategy.Name(), err)
		}
	}
	return nil
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

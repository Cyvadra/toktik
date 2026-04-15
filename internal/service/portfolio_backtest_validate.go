package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

type resolvedBacktestPlan struct {
	from            time.Time
	to              time.Time
	asset           string
	interval        string
	strategyLabel   string
	profileSource   string
	warnings        []string
	primaryMarket   marketSpec
	tradeScope      instrumentScope
	resolved        []strategies.ResolvedStrategy
	commissionModel backtest.CommissionModel
	chainProvider   backtest.OptionsChainProvider
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
	asset := strings.ToUpper(strings.TrimSpace(req.Asset))
	interval := defaultString(req.Interval, "1h")
	resolved, strategyLabel, err := resolveRequestedStrategies(req, strategyCfg, asset)
	if err != nil {
		return nil, err
	}
	if err := validateInstrumentScope(tradeScope, resolved); err != nil {
		return nil, err
	}

	commissionModel, err := parseCommissionModel(defaultString(req.CommissionModel, "none"))
	if err != nil {
		return nil, err
	}

	var chainProvider backtest.OptionsChainProvider
	if shouldLoadOptionChain(tradeScope, resolved) {
		if run != nil {
			run.setProgress(&dto.StrategyBacktestProgress{
				Phase:     string(backtest.ProgressPhasePrepare),
				Current:   0,
				Total:     1,
				Percent:   0,
				Message:   fmt.Sprintf("loading options chain for %s [%s/%s]", asset, primaryMarket.name, interval),
				StartedAt: derefTime(run.startedAt),
				Timestamp: s.now().UTC(),
			})
		}
		chainProvider, err = s.loadOptionsChainProvider(ctx, primaryMarket.name, asset, interval, from, to)
		if err != nil {
			return nil, fmt.Errorf("load options chain: %w", err)
		}
	}

	return &resolvedBacktestPlan{
		from:            from,
		to:              to,
		asset:           asset,
		interval:        interval,
		strategyLabel:   strategyLabel,
		profileSource:   validationProfileSource(req),
		warnings:        validationWarnings(req),
		primaryMarket:   primaryMarket,
		tradeScope:      tradeScope,
		resolved:        resolved,
		commissionModel: commissionModel,
		chainProvider:   chainProvider,
	}, nil
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
		}
		items = append(items, entry)
	}
	return &dto.StrategyBacktestValidationResponse{
		StrategyLabel: describeResolvedStrategies(plan.resolved, plan.strategyLabel),
		StrategyCount: len(items),
		Strategies:    sliceOrEmpty(items),
	}
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

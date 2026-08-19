package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/datafeed"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/report"
	"github.com/Cyvadra/toktik/internal/requestpriority"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
	"github.com/Cyvadra/toktik/pkg/feeds"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

const (
	defaultBacktestHTMLDir  = "reports/backtests"
	defaultAPIRunHTMLSubdir = "api"
	backtestStatusQueued    = "queued"
	backtestStatusRunning   = "running"
	backtestStatusCompleted = "completed"
	backtestStatusFailed    = "failed"
	backtestStatusCanceled  = "canceled"
	marketCrypto            = "crypto"
	marketUS                = "us"
	marketForex             = "forex"
	cryptoUnderlyingFeed    = "crypto-underlying"
	usUnderlyingFeed        = "us-underlying"
	usStocksFeed            = "us-stocks"
	forexUnderlyingFeed     = "forex-underlying"
	defaultChainProviderTTL = 55 * time.Minute
	maxChainProviderEntries = 8
	defaultRunTTL           = 2 * time.Hour
	runEvictionInterval     = 10 * time.Minute
)

type marketSpec struct {
	name           string
	underlyingFeed string
}

type instrumentScope string

type capitalProfile struct {
	mode string
	unit string
	note string
}

const (
	instrumentAuto     instrumentScope = "auto"
	instrumentSpot     instrumentScope = "spot"
	instrumentContract instrumentScope = "contract"
	instrumentMixed    instrumentScope = "mixed"
)

type PortfolioBacktestService struct {
	repo          *chrepo.Repo
	factorStore   *feeds.Store
	universes     portfolioUniverseResolver
	now           func() time.Time
	chainLoader   func(context.Context, string, string, string, time.Time, time.Time) (backtest.OptionsChainProvider, error)
	engineBuilder func(backtest.Config, backtest.OptionsChainProvider, bool) *backtest.Engine
	chainCache    *optionsChainProviderCache
	runTTL        time.Duration
	reportsRoot   string
	stopEviction  chan struct{}
	evictionDone  chan struct{}
	closeOnce     sync.Once

	mu   sync.RWMutex
	runs map[string]*portfolioBacktestRun
}

type portfolioUniverseResolver interface {
	MemberIntervals(ctx context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error)
}

type optionsChainProviderCache struct {
	mu      sync.Mutex
	now     func() time.Time
	ttl     time.Duration
	maxSize int
	entries map[string]*optionsChainProviderCacheEntry
	loads   map[string]*optionsChainProviderLoad
}

type optionsChainProviderCacheEntry struct {
	provider  backtest.OptionsChainProvider
	expiresAt time.Time
	lastUsed  time.Time
}

type optionsChainProviderLoad struct {
	done     chan struct{}
	provider backtest.OptionsChainProvider
	err      error
}

type portfolioBacktestRun struct {
	id     string
	cancel context.CancelFunc

	mu          sync.RWMutex
	request     dto.StrategyBacktestRunRequest
	status      string
	createdAt   time.Time
	updatedAt   time.Time
	startedAt   *time.Time
	completedAt *time.Time
	progress    *dto.StrategyBacktestProgress
	result      *dto.StrategyBacktestRunResult
	errText     string
	subscribers map[chan dto.StrategyBacktestSSEvent]struct{}
	lastSent    time.Time
	closed      bool
	finished    bool
	dirty       bool
}

func NewPortfolioBacktestService(repo *chrepo.Repo, factorStore *feeds.Store) *PortfolioBacktestService {
	svc := &PortfolioBacktestService{
		repo:         repo,
		factorStore:  factorStore,
		now:          time.Now,
		runs:         make(map[string]*portfolioBacktestRun),
		runTTL:       defaultRunTTL,
		reportsRoot:  defaultBacktestHTMLDir,
		stopEviction: make(chan struct{}),
		evictionDone: make(chan struct{}),
	}
	svc.chainLoader = svc.defaultChainLoader
	svc.engineBuilder = func(cfg backtest.Config, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
		return newPortfolioBacktestEngine(cfg, svc.repo.Conn, svc.factorStore, chainProvider, usesOptions)
	}
	svc.chainCache = newOptionsChainProviderCache(svc.now, defaultChainProviderTTL, maxChainProviderEntries)
	go svc.evictionLoop()
	return svc
}

func (s *PortfolioBacktestService) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		close(s.stopEviction)
		<-s.evictionDone
	})
	return nil
}

func (s *PortfolioBacktestService) WithReportsRoot(root string) *PortfolioBacktestService {
	if s == nil {
		return nil
	}
	if trimmed := strings.TrimSpace(root); trimmed != "" {
		s.reportsRoot = trimmed
	}
	return s
}

func (s *PortfolioBacktestService) WithUniverseService(universes *UniverseService) *PortfolioBacktestService {
	if s == nil {
		return nil
	}
	s.universes = universes
	return s
}

// evictionLoop runs in the background and removes finished runs older than runTTL.
func (s *PortfolioBacktestService) evictionLoop() {
	ticker := time.NewTicker(runEvictionInterval)
	defer ticker.Stop()
	defer close(s.evictionDone)
	for {
		select {
		case <-ticker.C:
			s.evictOldRuns()
		case <-s.stopEviction:
			return
		}
	}
}

func (s *PortfolioBacktestService) evictOldRuns() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, run := range s.runs {
		run.mu.RLock()
		finished := run.finished
		updatedAt := run.updatedAt
		run.mu.RUnlock()
		if finished && now.Sub(updatedAt) > s.runTTL {
			delete(s.runs, id)
		}
	}
}

func newOptionsChainProviderCache(now func() time.Time, ttl time.Duration, maxSize int) *optionsChainProviderCache {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = defaultChainProviderTTL
	}
	if maxSize <= 0 {
		maxSize = maxChainProviderEntries
	}
	return &optionsChainProviderCache{
		now:     now,
		ttl:     ttl,
		maxSize: maxSize,
		entries: make(map[string]*optionsChainProviderCacheEntry),
		loads:   make(map[string]*optionsChainProviderLoad),
	}
}

func (s *PortfolioBacktestService) defaultChainLoader(ctx context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
	if marketName == marketUS {
		return datafeed.NewUSOptionsChainProvider(ctx, s.repo.Conn, asset, interval, from, to)
	}
	if marketName != marketCrypto {
		return nil, dto.NewValidationError("market %q does not support option chain loading", marketName)
	}
	return datafeed.NewCryptoOptionsChainProvider(ctx, s.repo.Conn, asset, interval, from, to)
}

func (s *PortfolioBacktestService) loadOptionsChainProvider(ctx context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
	if s.chainCache == nil {
		return s.chainLoader(ctx, marketName, asset, interval, from, to)
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%s",
		strings.ToLower(strings.TrimSpace(marketName)),
		strings.ToUpper(strings.TrimSpace(asset)),
		strings.TrimSpace(interval),
		from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339),
	)
	return s.chainCache.GetOrLoad(ctx, key, func(ctx context.Context) (backtest.OptionsChainProvider, error) {
		return s.chainLoader(ctx, marketName, asset, interval, from, to)
	})
}

func (s *PortfolioBacktestService) loadOptionChainUniverse(ctx context.Context, run *portfolioBacktestRun, interval string, from, to time.Time, targets []optionChainTarget) (backtest.OptionsChainProvider, []dto.StrategyBacktestRuntimeWarning, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	if s.repo == nil || s.repo.Conn == nil {
		provider, err := s.loadEagerOptionChainUniverse(ctx, interval, from, to, targets)
		return provider, nil, err
	}
	providers := make(map[string]backtest.OptionsChainProvider, len(targets))
	warnings := make([]dto.StrategyBacktestRuntimeWarning, 0)
	for index, target := range targets {
		if run != nil {
			run.setProgress(&dto.StrategyBacktestProgress{
				Phase:     string(backtest.ProgressPhasePrepare),
				Current:   index,
				Total:     maxInt(len(targets), 1),
				Percent:   float64(index) / float64(maxInt(len(targets), 1)) * 100,
				Message:   fmt.Sprintf("precomputing options for %s [%s/%s]", target.asset, target.market, interval),
				StartedAt: derefTime(run.startedAt),
				Timestamp: s.now().UTC(),
			})
		}
		timestamps, err := s.loadOptionPrecomputeTimestamps(ctx, target.market, target.asset, interval, from, to)
		if err != nil {
			if errors.Is(err, errOptionPrecomputeNoData) {
				warning, policyErr := optionPrecomputeNoDataPolicy(target, interval, from, to, err)
				if policyErr != nil {
					return nil, nil, policyErr
				}
				warnings = append(warnings, warning)
				continue
			}
			return nil, nil, err
		}
		rawProvider, err := s.loadOptionsChainProvider(ctx, target.market, target.asset, interval, from, to)
		if err != nil {
			return nil, nil, err
		}
		snapshot := backtest.NewOptionsChainSnapshot(rawProvider, target.market, target.asset, timestamps)
		providers[backtest.ChainLookupKey(target.market, target.asset)] = backtest.NewSnapshotOptionsChainProvider(snapshot)
	}
	return backtest.NewMultiOptionsChainProvider(providers), warnings, nil
}

func optionPrecomputeNoDataPolicy(target optionChainTarget, interval string, from, to time.Time, cause error) (dto.StrategyBacktestRuntimeWarning, error) {
	if target.required {
		return dto.StrategyBacktestRuntimeWarning{}, cause
	}
	return dto.StrategyBacktestRuntimeWarning{
		Severity: "warning",
		Code:     "options.underlying_data_omitted",
		Message:  fmt.Sprintf("universe symbol %s omitted because no underlying bars were available", target.asset),
		Symbol:   target.asset,
		Details:  map[string]string{"market": target.market, "interval": interval, "from": from.UTC().Format(time.RFC3339), "to": to.UTC().Format(time.RFC3339)},
	}, nil
}

func (s *PortfolioBacktestService) loadEagerOptionChainUniverse(ctx context.Context, interval string, from, to time.Time, targets []optionChainTarget) (backtest.OptionsChainProvider, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	if len(targets) == 1 {
		target := targets[0]
		return s.loadOptionsChainProvider(ctx, target.market, target.asset, interval, from, to)
	}
	providers := make(map[string]backtest.OptionsChainProvider, len(targets))
	for _, target := range targets {
		provider, err := s.loadOptionsChainProvider(ctx, target.market, target.asset, interval, from, to)
		if err != nil {
			return nil, err
		}
		providers[backtest.ChainLookupKey(target.market, target.asset)] = provider
	}
	return backtest.NewMultiOptionsChainProvider(providers), nil
}

var errOptionPrecomputeNoData = errors.New("option precompute has no underlying data")

func (s *PortfolioBacktestService) loadOptionPrecomputeTimestamps(ctx context.Context, marketName, asset, interval string, from, to time.Time) ([]time.Time, error) {
	marketSpec, err := parsePrimaryMarket(marketName)
	if err != nil {
		return nil, err
	}
	if s.repo == nil || s.repo.Conn == nil {
		return nil, fmt.Errorf("option precompute timestamps require ClickHouse repo")
	}
	var feed backtest.DataFeed
	switch marketSpec.name {
	case marketUS:
		feed = datafeed.NewUSUnderlyingDataFeed(s.repo.Conn)
	case marketCrypto:
		feed = datafeed.NewCryptoUnderlyingDataFeed(s.repo.Conn)
	case marketForex:
		feed = datafeed.NewForexUnderlyingDataFeed(s.repo.Conn)
	default:
		return nil, dto.NewValidationError("market %q does not support option precompute", marketName)
	}
	ds, err := feed.Load(ctx, backtest.DataRequest{Market: marketSpec.underlyingFeed, Symbol: asset, Interval: interval, From: from, To: to})
	if err != nil {
		return nil, fmt.Errorf("load option precompute timestamps for %s/%s: %w", marketName, asset, err)
	}
	if ds == nil || len(ds.Timestamps) == 0 {
		return nil, fmt.Errorf("load option precompute timestamps for %s/%s: %w", marketName, asset, errOptionPrecomputeNoData)
	}
	return append([]time.Time(nil), ds.Timestamps...), nil
}

func (c *optionsChainProviderCache) GetOrLoad(ctx context.Context, key string, loader func(context.Context) (backtest.OptionsChainProvider, error)) (backtest.OptionsChainProvider, error) {
	now := c.now()

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok {
		if entry.expiresAt.IsZero() || now.Before(entry.expiresAt) {
			entry.lastUsed = now
			provider := entry.provider
			c.mu.Unlock()
			return provider, nil
		}
		delete(c.entries, key)
	}
	if load, ok := c.loads[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-load.done:
			if load.err != nil {
				return nil, load.err
			}
			return load.provider, nil
		}
	}
	load := &optionsChainProviderLoad{done: make(chan struct{})}
	c.loads[key] = load
	c.mu.Unlock()

	provider, err := loader(ctx)

	c.mu.Lock()
	delete(c.loads, key)
	if err == nil && provider != nil {
		c.pruneExpiredLocked(now)
		if len(c.entries) >= c.maxSize {
			c.evictOldestLocked()
		}
		c.entries[key] = &optionsChainProviderCacheEntry{
			provider:  provider,
			expiresAt: now.Add(c.ttl),
			lastUsed:  now,
		}
	}
	load.provider = provider
	load.err = err
	close(load.done)
	c.mu.Unlock()

	return provider, err
}

func (c *optionsChainProviderCache) pruneExpiredLocked(now time.Time) {
	for key, entry := range c.entries {
		if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

func (c *optionsChainProviderCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range c.entries {
		if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.lastUsed
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (s *PortfolioBacktestService) StartStrategyBacktest(_ context.Context, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestRunAccepted, error) {
	if err := s.validateBacktestSubmission(req); err != nil {
		return nil, err
	}

	runID, err := newBacktestRunID()
	if err != nil {
		return nil, fmt.Errorf("generate run id: %w", err)
	}
	now := s.now().UTC()
	runCtx, cancel := context.WithCancel(requestpriority.WithBackground(context.Background()))
	run := &portfolioBacktestRun{
		id:          runID,
		cancel:      cancel,
		request:     req,
		status:      backtestStatusQueued,
		createdAt:   now,
		updatedAt:   now,
		subscribers: make(map[chan dto.StrategyBacktestSSEvent]struct{}),
		dirty:       true,
	}

	s.mu.Lock()
	s.runs[runID] = run
	s.mu.Unlock()

	go run.publishLoop()
	go s.executeRun(runCtx, run)

	return &dto.StrategyBacktestRunAccepted{
		RunID:     runID,
		Status:    backtestStatusQueued,
		CreatedAt: now,
		StatusURL: fmt.Sprintf("/api/v1/backtests/runs/%s", runID),
		EventsURL: fmt.Sprintf("/api/v1/backtests/runs/%s/events", runID),
	}, nil
}

func (s *PortfolioBacktestService) validateBacktestSubmission(req dto.StrategyBacktestRunRequest) error {
	if err := validateStrategyBacktestRunRequest(req); err != nil {
		return err
	}
	if _, _, err := dto.ParseTimeRange(req.From, req.To); err != nil {
		return err
	}
	strategyCfg, err := buildBacktestStrategyConfig(req)
	if err != nil {
		return err
	}
	primaryMarket, err := parsePrimaryMarket(defaultString(req.Market, marketCrypto))
	if err != nil {
		return err
	}
	tradeScope, err := parseInstrumentScope(defaultString(req.Instrument, string(instrumentAuto)))
	if err != nil {
		return err
	}
	if _, err := parseCommissionModel(defaultString(req.CommissionModel, "none")); err != nil {
		return err
	}
	asset := resolvePrimaryBacktestAsset(req)
	resolved, _, err := resolveRequestedStrategies(req, strategyCfg, asset)
	if err != nil {
		return err
	}
	if err := validateInstrumentScope(tradeScope, resolved); err != nil {
		return err
	}
	if err := validateMarketStrategyCompatibility(primaryMarket, resolved); err != nil {
		return err
	}
	return nil
}

func (s *PortfolioBacktestService) GetStrategyBacktestRun(_ context.Context, runID string) (*dto.StrategyBacktestRunStatus, error) {
	run, err := s.lookupRun(runID)
	if err != nil {
		return nil, err
	}
	return run.snapshot(), nil
}

func (s *PortfolioBacktestService) CancelStrategyBacktest(_ context.Context, runID string) (*dto.StrategyBacktestRunStatus, error) {
	run, err := s.lookupRun(runID)
	if err != nil {
		return nil, err
	}
	run.cancelRun(s.now().UTC(), "backtest run canceled")
	s.persistRunManifest(run)
	return run.snapshot(), nil
}

func (s *PortfolioBacktestService) SubscribeStrategyBacktest(_ context.Context, runID string) (<-chan dto.StrategyBacktestSSEvent, func(), error) {
	run, err := s.lookupRun(runID)
	if err != nil {
		return nil, nil, err
	}
	stream, unsubscribe := run.subscribe()
	return stream, unsubscribe, nil
}

func (s *PortfolioBacktestService) lookupRun(runID string) (*portfolioBacktestRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, dto.NewValidationError("run id is required")
	}

	s.mu.RLock()
	run := s.runs[runID]
	s.mu.RUnlock()
	if run == nil {
		if !validBacktestRunID(runID) {
			return nil, dto.NewNotFoundError("backtest run %q not found", runID)
		}
		manifest, err := report.ReadRunManifest(s.runDirectory(runID))
		if err != nil || manifest.Status == nil || manifest.Status.RunID != runID {
			return nil, dto.NewNotFoundError("backtest run %q not found", runID)
		}
		return &portfolioBacktestRun{
			id:          runID,
			request:     manifest.Status.Request,
			status:      manifest.Status.Status,
			createdAt:   manifest.Status.CreatedAt,
			updatedAt:   manifest.Status.UpdatedAt,
			startedAt:   manifest.Status.StartedAt,
			completedAt: manifest.Status.CompletedAt,
			progress:    manifest.Status.Progress,
			result:      manifest.Status.Result,
			errText:     manifest.Status.Error,
			subscribers: make(map[chan dto.StrategyBacktestSSEvent]struct{}),
			closed:      true,
			finished:    true,
		}, nil
	}
	return run, nil
}

func (s *PortfolioBacktestService) executeRun(ctx context.Context, run *portfolioBacktestRun) {
	startedAt := s.now().UTC()
	if !run.markRunning(startedAt) {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			run.markFailed(s.now().UTC(), fmt.Sprintf("panic: %v", recovered))
			s.persistRunManifest(run)
		}
	}()

	result, err := s.runBacktest(ctx, run, run.request)
	completedAt := s.now().UTC()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			run.cancelRun(completedAt, "backtest run canceled")
			s.persistRunManifest(run)
			return
		}
		run.markFailed(completedAt, err.Error())
		s.persistRunManifest(run)
		return
	}
	run.markCompleted(completedAt, result)
	s.persistRunManifest(run)
}

func (s *PortfolioBacktestService) runDirectory(runID string) string {
	return filepath.Join(defaultString(s.reportsRoot, defaultBacktestHTMLDir), defaultAPIRunHTMLSubdir, runID)
}

func (s *PortfolioBacktestService) persistRunManifest(run *portfolioBacktestRun) {
	if run == nil {
		return
	}
	status := run.snapshot()
	if err := report.WriteRunManifest(s.runDirectory(run.id), report.NewRunManifest(status)); err != nil {
		slog.Error("persist backtest run manifest", "run_id", run.id, "error", err)
	}
}

func (s *PortfolioBacktestService) ValidateStrategyBacktest(ctx context.Context, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestValidationResponse, error) {
	plan, err := s.resolveBacktestPlan(ctx, nil, req, false)
	if err != nil {
		return nil, err
	}
	if err := s.preflightBacktestPlan(ctx, nil, plan); err != nil {
		return nil, err
	}
	return buildStrategyBacktestValidationResponse(plan), nil
}

func (s *PortfolioBacktestService) runBacktest(ctx context.Context, run *portfolioBacktestRun, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestRunResult, error) {
	plan, err := s.resolveBacktestPlan(ctx, run, req, true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.DSL) != "" || len(plan.universeCodes) > 0 {
		if err := s.preflightBacktestPlan(ctx, run, plan); err != nil {
			return nil, err
		}
	}

	runDir := s.runDirectory(run.id)
	// Intentionally ignore req.HTMLOutput for API runs: a user-controlled
	// path would let the API write reports anywhere on disk and would
	// later be served back by the report endpoint, opening a path
	// traversal / arbitrary file read vector. API runs are pinned to
	// runDir; the CLI binary still honours its own --output-html flag.
	htmlBase := ""
	htmlMeta := buildBacktestReportHTMLMeta(req, plan, s.now())
	resultSet := make([]dto.StrategyBacktestSummary, 0, len(plan.resolved))
	runWarnings := append([]dto.StrategyBacktestRuntimeWarning(nil), plan.runtimeWarnings...)
	overviewItems := make([]report.OverviewItem, 0, len(plan.resolved))

	for index, item := range plan.resolved {
		capitalProfile := resolveCapitalProfile(plan.primaryMarket, item.Profile, plan.asset)
		strategy, err := item.NewStrategy()
		if err != nil {
			return nil, fmt.Errorf("build strategy %s: %w", resolvedStrategyName(item), err)
		}
		engine := s.engineBuilder(backtest.Config{
			InitialCapital:  req.Capital,
			AccountUnit:     capitalProfile.unit,
			CommissionModel: plan.commissionModel,
			CommissionValue: req.CommissionValue,
			SlippagePct:     req.SlippagePct,
			ExecutionMode:   backtest.ExecutionPriceCanonical,
			ValuationMode:   backtest.ValuationPriceClose,
			TriggerMode:     backtest.TriggerPriceCanonical,
		}, plan.chainProvider, item.Profile.UsesOptions)
		engine.SetProgressFunc(func(update backtest.ProgressUpdate) {
			run.setProgress(progressFromUpdate(update))
		})

		result, runErr := engine.Run(ctx, plan.primaryMarket.underlyingFeed, plan.asset, plan.interval, plan.from, plan.to, strategy, nil)
		if runErr != nil {
			return nil, fmt.Errorf("run strategy %s: %w", strategy.Name(), runErr)
		}
		result.CapitalMode = strings.ToUpper(capitalProfile.unit)
		result.CapitalProfile = item.Runtime.ProfileLabel
		result.CapitalNote = capitalProfile.note

		htmlPath := resolveAPIHTMLOutputPath(htmlBase, runDir, result.StrategyName, plan.asset, plan.interval, plan.from, plan.to, index, len(plan.resolved))
		if err := report.WriteBacktestHTML(htmlPath, result, htmlMeta); err != nil {
			return nil, fmt.Errorf("write html report for %s: %w", result.StrategyName, err)
		}
		overviewItems = append(overviewItems, report.OverviewItem{Result: result, HTMLPath: htmlPath})
		summary := buildStrategyBacktestSummary(result, htmlPath)
		if dslStrategy, ok := strategy.(*bridge.DslStrategy); ok {
			dslDiagnostics := dslStrategy.Diagnostics()
			if diagnostics.List(dslDiagnostics).HasErrors() {
				return nil, fmt.Errorf("run strategy %s: %s", strategy.Name(), firstErrorDiagnosticMessage(dslDiagnostics))
			}
			summary.Warnings = append(summary.Warnings, dslDiagnosticsToRuntimeWarnings(dslDiagnostics)...)
		}
		runWarnings = append(runWarnings, summary.Warnings...)
		resultSet = append(resultSet, summary)
	}

	resp := &dto.StrategyBacktestRunResult{Summaries: resultSet, Warnings: runWarnings}
	if len(resultSet) > 1 {
		overviewPath := resolveAPIOverviewHTMLOutputPath(htmlBase, runDir, describeResolvedStrategies(plan.resolved, plan.strategyLabel), plan.asset, plan.interval, plan.from, plan.to)
		if err := report.WriteBacktestOverviewHTML(overviewPath, overviewItems, htmlMeta); err != nil {
			return nil, fmt.Errorf("write overview html report: %w", err)
		}
		resp.OverviewHTMLPath = overviewPath
	}

	return resp, nil
}

func newPortfolioBacktestEngine(cfg backtest.Config, conn driver.Conn, factorStore *feeds.Store, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
	engine := backtest.NewEngine(cfg)
	engine.RegisterDataFeed(cryptoUnderlyingFeed, datafeed.NewCryptoUnderlyingDataFeed(conn))
	usFeed := datafeed.NewUSUnderlyingDataFeed(conn)
	engine.RegisterDataFeed(marketUS, usFeed)
	engine.RegisterDataFeed(usUnderlyingFeed, usFeed)
	engine.RegisterDataFeed(usStocksFeed, usFeed)
	engine.RegisterDataFeed(forexUnderlyingFeed, datafeed.NewForexUnderlyingDataFeed(conn))
	dvolFeed := datafeed.NewDVOLMacroFactorFeed(NewMacroService(chrepo.NewRepo(conn)))
	engine.RegisterFactorFeed("dvol", dvolFeed)
	engine.RegisterFactorFeed("dvol_macro", dvolFeed)
	engine.RegisterFactorFeed("volatility", datafeed.NewFeatureVolatilityFactorFeed(conn))
	fundamentalsSvc := NewFundamentalsService(chrepo.NewRepo(conn))
	registerFundamentalFactorFeeds(engine, fundamentalsSvc)
	if usesOptions && chainProvider != nil {
		engine.SetOptionsChainProvider(chainProvider)
	}
	return engine
}

func registerFundamentalFactorFeeds(engine *backtest.Engine, fundamentalsSvc *FundamentalsService) {
	if engine == nil || fundamentalsSvc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	catalog, err := fundamentalsSvc.ListFactors(ctx, dto.FundamentalFactorCatalogRequest{})
	if err != nil {
		slog.Warn("register fundamentals factor feeds: list factors failed", "error", err)
		return
	}
	seen := make(map[string]struct{}, len(catalog.Data))
	for _, entry := range catalog.Data {
		factorCode := strings.TrimSpace(entry.FactorCode)
		if factorCode == "" || !entry.Active {
			continue
		}
		if _, ok := seen[factorCode]; ok {
			continue
		}
		seen[factorCode] = struct{}{}
		engine.RegisterFactorFeed(factorCode, datafeed.NewFundamentalsFactorFeed(fundamentalsSvc, factorCode))
	}
}

func validateStrategyBacktestRunRequest(req dto.StrategyBacktestRunRequest) error {
	if strings.TrimSpace(resolvePrimaryBacktestAsset(req)) == "" && strings.TrimSpace(req.DSL) == "" {
		return dto.NewValidationError("asset is required unless portfolio or symbols are provided")
	}
	if len(req.Weights) > 0 && len(req.Symbols) != len(req.Weights) {
		return dto.NewValidationError("weights length must match symbols length")
	}
	for _, leg := range req.Portfolio {
		if strings.TrimSpace(leg.Asset) == "" {
			return dto.NewValidationError("portfolio legs require asset")
		}
		if leg.Weight < 0 {
			return dto.NewValidationError("portfolio leg weight must be >= 0")
		}
	}
	for _, weight := range req.Weights {
		if weight < 0 {
			return dto.NewValidationError("weights must be >= 0")
		}
	}
	if req.Capital <= 0 {
		return dto.NewValidationError("capital must be > 0")
	}
	if req.PositionSize < 0 {
		return dto.NewValidationError("position_size must be >= 0")
	}
	if req.MaxHoldHours < 0 {
		return dto.NewValidationError("max_hold_hours must be >= 0")
	}
	if req.TargetExpiryDays < 0 {
		return dto.NewValidationError("target_expiry_days must be >= 0")
	}
	if req.MinExpiryDays < 0 {
		return dto.NewValidationError("min_expiry_days must be >= 0")
	}
	if req.MinPremium < 0 {
		return dto.NewValidationError("min_premium must be >= 0")
	}
	if req.CommissionValue < 0 {
		return dto.NewValidationError("commission_value must be >= 0")
	}
	if req.SlippagePct < 0 {
		return dto.NewValidationError("slippage_pct must be >= 0")
	}
	if req.SlippagePct > 1 {
		return dto.NewValidationError("slippage_pct must be <= 1")
	}
	if strings.TrimSpace(req.ReportChartPrefix) != "" {
		if strings.TrimSpace(req.ReportChartMarket) != "" || strings.TrimSpace(req.ReportChartSymbol) != "" || strings.TrimSpace(req.ReportChartInterval) != "" {
			return dto.NewValidationError("report_chart_prefix cannot be combined with report_chart_market, report_chart_symbol, or report_chart_interval")
		}
	}
	if strings.TrimSpace(req.ReportChartSymbol) == "" && (strings.TrimSpace(req.ReportChartMarket) != "" || strings.TrimSpace(req.ReportChartInterval) != "") {
		return dto.NewValidationError("report_chart_symbol is required when report_chart_market or report_chart_interval is provided")
	}
	if req.ShortDeltaMin < 0 || req.ShortDeltaMax < 0 || req.LongDeltaMin < 0 || req.LongDeltaMax < 0 {
		return dto.NewValidationError("delta bounds must be >= 0")
	}
	if req.TargetExpiryDays > 0 && req.MinExpiryDays > 0 && req.TargetExpiryDays < req.MinExpiryDays {
		return dto.NewValidationError("target_expiry_days must be >= min_expiry_days")
	}
	if req.ShortDeltaMax > 0 && req.ShortDeltaMin > req.ShortDeltaMax {
		return dto.NewValidationError("short_delta_min must be <= short_delta_max")
	}
	if req.LongDeltaMax > 0 && req.LongDeltaMin > req.LongDeltaMax {
		return dto.NewValidationError("long_delta_min must be <= long_delta_max")
	}
	if strings.TrimSpace(req.DSL) != "" && strings.TrimSpace(req.Strategy) != "" {
		return dto.NewValidationError("strategy and dsl are mutually exclusive")
	}
	if strings.TrimSpace(req.DSL) == "" {
		if len(req.DSLParams) > 0 {
			return dto.NewValidationError("dsl_params requires dsl")
		}
		if req.DSLProfile != nil {
			return dto.NewValidationError("dsl_profile requires dsl")
		}
	}
	return nil
}

func progressFromUpdate(update backtest.ProgressUpdate) *dto.StrategyBacktestProgress {
	percent := 0.0
	if update.Total > 0 {
		percent = float64(update.Current) / float64(update.Total) * 100
	}
	return &dto.StrategyBacktestProgress{
		Phase:     string(update.Phase),
		Current:   update.Current,
		Total:     update.Total,
		Percent:   percent,
		Message:   strings.TrimSpace(update.Message),
		StartedAt: update.StartedAt,
		Timestamp: update.Timestamp,
		Completed: update.Completed,
	}
}

func buildStrategyBacktestSummary(result *backtest.Result, htmlPath string) dto.StrategyBacktestSummary {
	summary := dto.StrategyBacktestSummary{
		StrategyName:     result.StrategyName,
		StartTime:        result.StartTime,
		EndTime:          result.EndTime,
		BarsCount:        result.BarsCount,
		InitialCapital:   result.InitialCapital,
		FinalEquity:      result.FinalEquity,
		AccountUnit:      result.AccountUnit,
		CapitalMode:      result.CapitalMode,
		CapitalProfile:   result.CapitalProfile,
		CapitalNote:      result.CapitalNote,
		TotalReturn:      result.TotalReturn,
		AnnualizedReturn: result.AnnualizedReturn,
		SharpeRatio:      result.SharpeRatio,
		CalmarRatio:      result.CalmarRatio,
		MaxDrawdown:      result.MaxDrawdown,
		TotalTrades:      summaryTotalTrades(result),
		TotalFills:       result.TotalFills,
		ClosedTrades:     result.ClosedTrades,
		OpenEntries:      result.OpenEntries,
		WinningTrades:    result.WinningTrades,
		LosingTrades:     result.LosingTrades,
		WinRate:          result.WinRate,
		ProfitFactor:     result.ProfitFactor,
		AvgWin:           result.AvgWin,
		AvgLoss:          result.AvgLoss,
		TotalFees:        result.TotalFees,
		HTMLPath:         htmlPath,
	}
	if result.SpreadSummary != nil {
		summary.SpreadSummary = &dto.StrategyBacktestSpreadSummary{
			TotalSpreads:   result.SpreadSummary.TotalSpreads,
			ClosedSpreads:  result.SpreadSummary.ClosedSpreads,
			OpenSpreads:    result.SpreadSummary.OpenSpreads,
			TotalPnL:       result.SpreadSummary.TotalPnL,
			WinningSpreads: result.SpreadSummary.WinningSpreads,
			LosingSpreads:  result.SpreadSummary.LosingSpreads,
			WinRate:        result.SpreadSummary.WinRate,
		}
	}
	if len(result.Warnings) > 0 {
		summary.Warnings = backtestWarningsToDTO(result.Warnings)
	}
	return summary
}

func summaryTotalTrades(result *backtest.Result) int {
	if result == nil {
		return 0
	}
	if result.TotalTrades > 0 {
		return result.TotalTrades
	}
	if result.SpreadSummary != nil && result.SpreadSummary.TotalSpreads > 0 {
		return result.SpreadSummary.TotalSpreads
	}
	return 0
}

func backtestWarningsToDTO(warnings []backtest.BacktestWarning) []dto.StrategyBacktestRuntimeWarning {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]dto.StrategyBacktestRuntimeWarning, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, dto.StrategyBacktestRuntimeWarning{
			Severity:  string(warning.Severity),
			Code:      warning.Code,
			Message:   warning.Message,
			BarIndex:  warning.BarIndex,
			Timestamp: warning.Timestamp,
			Symbol:    warning.Symbol,
			Policy:    warning.Policy,
			SpreadID:  warning.SpreadID,
			LegIndex:  warning.LegIndex,
			Details:   warning.Details,
		})
	}
	return out
}

func dslDiagnosticsToRuntimeWarnings(items []diagnostics.Diagnostic) []dto.StrategyBacktestRuntimeWarning {
	if len(items) == 0 {
		return nil
	}
	out := make([]dto.StrategyBacktestRuntimeWarning, 0, len(items))
	for _, item := range items {
		out = append(out, dto.StrategyBacktestRuntimeWarning{
			Severity: string(item.Severity),
			Code:     item.Code,
			Message:  item.Message,
			BarIndex: item.BarIndex,
		})
	}
	return out
}

func firstErrorDiagnosticMessage(items []diagnostics.Diagnostic) string {
	for _, item := range items {
		if item.Severity == diagnostics.SeverityError {
			return item.String()
		}
	}
	return "dsl diagnostic error"
}

func buildBacktestReportHTMLMeta(req dto.StrategyBacktestRunRequest, plan *resolvedBacktestPlan, generatedAt time.Time) report.HTMLMeta {
	meta := report.HTMLMeta{GeneratedAt: generatedAt}
	if plan != nil {
		meta.Asset = plan.asset
		meta.Interval = plan.interval
	}
	meta.ChartSeriesPrefix = strings.TrimSpace(req.ReportChartPrefix)
	meta.ChartMarket = strings.TrimSpace(req.ReportChartMarket)
	meta.ChartSymbol = strings.ToUpper(strings.TrimSpace(req.ReportChartSymbol))
	meta.ChartInterval = strings.TrimSpace(req.ReportChartInterval)
	if meta.ChartInterval == "" {
		meta.ChartInterval = meta.Interval
	}
	if meta.ChartMarket == "" && meta.ChartSymbol != "" && plan != nil {
		meta.ChartMarket = plan.primaryMarket.underlyingFeed
	}
	if meta.ChartMarket != "" && meta.ChartSymbol != "" && meta.ChartInterval != "" {
		meta.ChartSeriesPrefix = strings.Join([]string{"request.security", meta.ChartMarket + "|" + meta.ChartSymbol + "|" + meta.ChartInterval}, ".")
	}
	if meta.ChartSelectionLabel == "" && meta.ChartSymbol != "" {
		parts := []string{meta.ChartSymbol}
		if meta.ChartMarket != "" {
			parts = append(parts, meta.ChartMarket)
		}
		if meta.ChartInterval != "" {
			parts = append(parts, meta.ChartInterval)
		}
		meta.ChartSelectionLabel = strings.Join(parts, " / ")
	}
	return meta
}

func parsePrimaryMarket(raw string) (marketSpec, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", marketCrypto, cryptoUnderlyingFeed:
		return marketSpec{name: marketCrypto, underlyingFeed: cryptoUnderlyingFeed}, nil
	case marketUS, usUnderlyingFeed, usStocksFeed:
		return marketSpec{name: marketUS, underlyingFeed: usUnderlyingFeed}, nil
	case marketForex, forexUnderlyingFeed:
		return marketSpec{name: marketForex, underlyingFeed: forexUnderlyingFeed}, nil
	default:
		return marketSpec{}, dto.NewValidationError("market %q is invalid; want crypto|us|forex", raw)
	}
}

func parseInstrumentScope(raw string) (instrumentScope, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(instrumentAuto):
		return instrumentAuto, nil
	case string(instrumentSpot), "underlying":
		return instrumentSpot, nil
	case string(instrumentContract), "contracts", "option", "options":
		return instrumentContract, nil
	case string(instrumentMixed), "both":
		return instrumentMixed, nil
	default:
		return "", dto.NewValidationError("instrument %q is invalid; want auto|spot|contract|mixed", raw)
	}
}

func validateInstrumentScope(scope instrumentScope, items []strategies.ResolvedStrategy) error {
	switch scope {
	case instrumentSpot:
		if strategiesNeedOptions(items) {
			return dto.NewValidationError("instrument=spot does not support option-contract strategies")
		}
	case instrumentContract:
		for _, item := range items {
			if item.Profile.UsesOptions {
				continue
			}
			name := strings.TrimSpace(item.CanonicalName)
			if name == "" {
				name = "selected strategy"
			}
			return dto.NewValidationError("instrument=contract requires option-contract strategies only; %s uses regular underlying trades", name)
		}
	}
	return nil
}

func shouldLoadOptionChain(scope instrumentScope, items []strategies.ResolvedStrategy) bool {
	if scope == instrumentSpot {
		return false
	}
	return strategiesNeedOptions(items)
}

func resolveCapitalProfile(market marketSpec, profile strategies.StrategyProfile, underlyingSymbol string) capitalProfile {
	underlyingSymbol = strings.ToUpper(strings.TrimSpace(underlyingSymbol))
	if underlyingSymbol == "" {
		underlyingSymbol = "BTC"
	}
	if !profile.UsesOptions {
		return capitalProfile{mode: "usd", unit: "USD", note: "该策略不包含合约逻辑，capital 按 USD 计价。"}
	}
	if market.name == marketUS {
		note := "该策略包含期权合约逻辑；在美股市场，capital 按 USD 计价。"
		if profile.UsesSignalOnlyRegularTrades() {
			note = "该策略包含期权合约逻辑，且现货腿仅用于信号跟踪；在美股市场，capital 按 USD 计价。"
		}
		return capitalProfile{mode: "usd", unit: "USD", note: note}
	}
	if market.name == marketForex {
		note := "该策略包含合约逻辑；在外汇市场，capital 按 USD 计价。"
		if profile.UsesSignalOnlyRegularTrades() {
			note = "该策略包含合约逻辑，且现货腿仅用于信号跟踪；在外汇市场，capital 按 USD 计价。"
		}
		return capitalProfile{mode: "usd", unit: "USD", note: note}
	}
	note := fmt.Sprintf("该策略包含期权合约逻辑；在加密市场，capital 按 %s 计价。", underlyingSymbol)
	if profile.UsesSignalOnlyRegularTrades() {
		note = fmt.Sprintf("该策略包含期权合约逻辑，且现货腿仅用于信号跟踪；在加密市场，capital 按 %s 计价。", underlyingSymbol)
	}
	return capitalProfile{mode: "base_asset", unit: underlyingSymbol, note: note}
}

func strategiesNeedOptions(items []strategies.ResolvedStrategy) bool {
	for _, item := range items {
		if item.Profile.UsesOptions {
			return true
		}
	}
	return false
}

func parseOptionPriceMode(value, fieldName string) (backtest.OptionPriceMode, error) {
	mode, err := strategies.ParseOptionPriceMode(value)
	if err != nil {
		return backtest.OptionPriceModeUnspecified, dto.NewValidationError("unsupported %s: %v", fieldName, err)
	}
	return mode, nil
}

func parseCommissionModel(s string) (backtest.CommissionModel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "":
		return backtest.CommissionNone, nil
	case "flat":
		return backtest.CommissionFlat, nil
	case "percent":
		return backtest.CommissionPercent, nil
	case "per-unit", "perunit":
		return backtest.CommissionPerUnit, nil
	default:
		return backtest.CommissionNone, dto.NewValidationError("commission_model %q is invalid; want none|flat|percent|per-unit", s)
	}
}

func parseTradeDirection(raw string) (strategies.TradeDirection, error) {
	direction := strategies.TradeDirection(strings.ToLower(strings.TrimSpace(raw)))
	switch direction {
	case strategies.DirectionBoth, strategies.DirectionLongOnly, strategies.DirectionShortOnly:
		return direction, nil
	default:
		return strategies.DirectionBoth, dto.NewValidationError("direction %q is invalid; want both|long_only|short_only", raw)
	}
}

func resolveAPIHTMLOutputPath(base, runDir, strategyName, asset, interval string, from, to time.Time, index, total int) string {
	if strings.TrimSpace(base) != "" {
		return resolveOutputPath(base, index, total)
	}
	fileName := fmt.Sprintf("%s_%s_%s_%s_%s.html", slugify(strategyName), strings.ToLower(asset), slugify(interval), from.Format("20060102"), to.Format("20060102"))
	return filepath.Join(runDir, fileName)
}

func resolveAPIOverviewHTMLOutputPath(base, runDir, strategyName, asset, interval string, from, to time.Time) string {
	if strings.TrimSpace(base) != "" {
		return base
	}
	name := slugify(strategyName)
	if name == "" {
		name = "overview"
	}
	fileName := fmt.Sprintf("%s_%s_%s_%s_%s.html", name, strings.ToLower(asset), slugify(interval), from.Format("20060102"), to.Format("20060102"))
	return filepath.Join(runDir, fileName)
}

func resolveOutputPath(base string, index, total int) string {
	if total == 1 {
		return base
	}
	dot := strings.LastIndex(base, ".")
	if dot < 0 {
		return fmt.Sprintf("%s_%d", base, index+1)
	}
	return fmt.Sprintf("%s_%d%s", base[:dot], index+1, base[dot:])
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-", ":", "-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func newBacktestRunID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validBacktestRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	_, err := hex.DecodeString(runID)
	return err == nil
}

func (r *portfolioBacktestRun) markRunning(startedAt time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return false
	}
	r.status = backtestStatusRunning
	r.startedAt = &startedAt
	r.updatedAt = startedAt
	r.dirty = true
	return true
}

func (r *portfolioBacktestRun) setProgress(progress *dto.StrategyBacktestProgress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if progress != nil && progress.Timestamp.IsZero() {
		progress.Timestamp = time.Now().UTC()
	}
	r.progress = progress
	r.updatedAt = time.Now().UTC()
	r.dirty = true
}

func (r *portfolioBacktestRun) markCompleted(completedAt time.Time, result *dto.StrategyBacktestRunResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.status = backtestStatusCompleted
	r.completedAt = &completedAt
	r.updatedAt = completedAt
	r.result = result
	if r.progress != nil {
		r.progress.Completed = true
		r.progress.Percent = 100
		r.progress.Timestamp = completedAt
	}
	r.dirty = true
	r.finished = true
}

func (r *portfolioBacktestRun) markFailed(completedAt time.Time, errText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.status = backtestStatusFailed
	r.completedAt = &completedAt
	r.updatedAt = completedAt
	r.errText = strings.TrimSpace(errText)
	if r.progress != nil {
		r.progress.Completed = true
		r.progress.Timestamp = completedAt
	}
	r.dirty = true
	r.finished = true
}

func (r *portfolioBacktestRun) cancelRun(completedAt time.Time, errText string) {
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.status = backtestStatusCanceled
	r.completedAt = &completedAt
	r.updatedAt = completedAt
	r.errText = strings.TrimSpace(errText)
	if r.progress != nil {
		r.progress.Completed = true
		r.progress.Timestamp = completedAt
	}
	r.dirty = true
	r.finished = true
}

func (r *portfolioBacktestRun) snapshot() *dto.StrategyBacktestRunStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := &dto.StrategyBacktestRunStatus{
		RunID:     r.id,
		Status:    r.status,
		Request:   r.request,
		CreatedAt: r.createdAt,
		UpdatedAt: r.updatedAt,
		Error:     r.errText,
	}
	if r.startedAt != nil {
		startedAt := *r.startedAt
		status.StartedAt = &startedAt
	}
	if r.completedAt != nil {
		completedAt := *r.completedAt
		status.CompletedAt = &completedAt
	}
	if r.progress != nil {
		progressCopy := *r.progress
		status.Progress = &progressCopy
	}
	if r.result != nil {
		resultCopy := *r.result
		resultCopy.Summaries = append([]dto.StrategyBacktestSummary(nil), r.result.Summaries...)
		status.Result = &resultCopy
	}
	return status
}

func (r *portfolioBacktestRun) subscribe() (<-chan dto.StrategyBacktestSSEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		ch := make(chan dto.StrategyBacktestSSEvent, 1)
		ch <- dto.StrategyBacktestSSEvent{Event: terminalEventName(r.status), Status: r.snapshotNoLock()}
		close(ch)
		return ch, func() {}
	}

	ch := make(chan dto.StrategyBacktestSSEvent, 8)
	r.subscribers[ch] = struct{}{}
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.subscribers[ch]; ok {
			delete(r.subscribers, ch)
			close(ch)
		}
	}
}

func (r *portfolioBacktestRun) snapshotNoLock() *dto.StrategyBacktestRunStatus {
	status := &dto.StrategyBacktestRunStatus{
		RunID:     r.id,
		Status:    r.status,
		Request:   r.request,
		CreatedAt: r.createdAt,
		UpdatedAt: r.updatedAt,
		Error:     r.errText,
	}
	if r.startedAt != nil {
		startedAt := *r.startedAt
		status.StartedAt = &startedAt
	}
	if r.completedAt != nil {
		completedAt := *r.completedAt
		status.CompletedAt = &completedAt
	}
	if r.progress != nil {
		progressCopy := *r.progress
		status.Progress = &progressCopy
	}
	if r.result != nil {
		resultCopy := *r.result
		resultCopy.Summaries = append([]dto.StrategyBacktestSummary(nil), r.result.Summaries...)
		status.Result = &resultCopy
	}
	return status
}

func (r *portfolioBacktestRun) publishLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		event, snapshot, subscribers, done := r.drainEvent()
		if snapshot != nil {
			for subscriber := range subscribers {
				select {
				case subscriber <- dto.StrategyBacktestSSEvent{Event: event, Status: snapshot}:
				default:
				}
			}
		}
		if done {
			r.closeSubscribers()
			return
		}
	}
}

func (r *portfolioBacktestRun) drainEvent() (string, *dto.StrategyBacktestRunStatus, map[chan dto.StrategyBacktestSSEvent]struct{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.dirty {
		return "", nil, nil, r.closed
	}
	snapshot := r.snapshotNoLock()
	subscribers := make(map[chan dto.StrategyBacktestSSEvent]struct{}, len(r.subscribers))
	for subscriber := range r.subscribers {
		subscribers[subscriber] = struct{}{}
	}
	r.dirty = false
	r.lastSent = time.Now().UTC()
	event := "progress"
	if r.finished {
		event = terminalEventName(r.status)
		r.closed = true
	}
	return event, snapshot, subscribers, r.closed
}

func (r *portfolioBacktestRun) closeSubscribers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for subscriber := range r.subscribers {
		close(subscriber)
		delete(r.subscribers, subscriber)
	}
}

func terminalEventName(status string) string {
	switch status {
	case backtestStatusCompleted:
		return backtestStatusCompleted
	case backtestStatusCanceled:
		return backtestStatusCanceled
	default:
		return backtestStatusFailed
	}
}

func isTerminalBacktestStatus(status string) bool {
	return status == backtestStatusCompleted || status == backtestStatusFailed || status == backtestStatusCanceled
}

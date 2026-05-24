package usmarket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
	"github.com/Cyvadra/toktik/pkg/tigerapi"
)

const (
	usStocksFundamentalsMarket = "us-stocks"
	usStocksPEFactorCode       = "pe"
	usStocksPBFactorCode       = "pb"
	usStocksPECatalogSource    = "external_fundamentals_provider"
	tigerKlineFundamentalSrc   = "tiger_kline_fundamental"
	tigerTTMPEField            = "ttmPeRate"
	maxFMPDiscoveryPages       = 1000
)

// PEBackfillProvider abstracts the upstream market-data source used to fetch
// PE observations. Tiger is currently the only implementation, but it is kept
// behind this seam so a later FMP provider can replace it cleanly.
type PEBackfillProvider interface {
	Name() string
	Validate() error
	NewWorker(context.Context) (PEBackfillWorker, error)
}

// PEBackfillWorker is a per-worker fetcher instance created by a provider.
// Keeping it separate avoids sharing provider client state across goroutines.
type PEBackfillWorker interface {
	FetchSymbolPE(ctx context.Context, symbol string, startDate, endDate time.Time, pageSize int, limiter backfillRateLimiter) (PEFetchResult, error)
}

// backfillRateLimiter is the small contract a provider needs for global QPS
// coordination across workers.
type backfillRateLimiter interface {
	Wait(context.Context) error
	Backoff(context.Context, time.Duration) error
}

// PEFetchResult is the provider-normalized output for one symbol fetch.
type PEFetchResult struct {
	ScannedBars    int
	Observations   []fundamentalObservationInsert
	ProviderName   string
	ProviderSource string
	Diagnostics    PEFetchDiagnostics
}

type PEFetchDiagnostics struct {
	NoQuarterInputs  int
	MissingPrice     int
	MissingTTMEPS    int
	MissingBookValue int
}

// TigerPEBackfillProvider seals Tiger-specific fetch logic behind the generic
// PEBackfillProvider interface. Tiger access is intentionally not the default
// sync path because account entitlements and subscription quotas often make it
// unsuitable for bulk market-wide backfills.
type TigerPEBackfillProvider struct {
	config tigerapi.Config
}

type tigerPEBackfillWorker struct {
	client *tigerapi.Client
}

func NewTigerPEBackfillProvider(cfg tigerapi.Config) *TigerPEBackfillProvider {
	return &TigerPEBackfillProvider{config: cfg}
}

func (p *TigerPEBackfillProvider) Name() string {
	return "tiger"
}

func (p *TigerPEBackfillProvider) Validate() error {
	return p.config.Validate()
}

func (p *TigerPEBackfillProvider) NewWorker(_ context.Context) (PEBackfillWorker, error) {
	client, err := tigerapi.New(p.config)
	if err != nil {
		return nil, err
	}
	return &tigerPEBackfillWorker{client: client}, nil
}

func (w *tigerPEBackfillWorker) FetchSymbolPE(ctx context.Context, symbol string, startDate, endDate time.Time, pageSize int, limiter backfillRateLimiter) (PEFetchResult, error) {
	bars, err := fetchTigerStockPEBars(ctx, w.client, symbol, startDate, endDate, pageSize, limiter)
	if err != nil {
		return PEFetchResult{}, err
	}
	observations := extractPEObservationsFromTigerBars(symbol, bars)
	return PEFetchResult{
		ScannedBars:    len(bars),
		Observations:   observations,
		ProviderName:   "tiger",
		ProviderSource: tigerKlineFundamentalSrc,
	}, nil
}

type USFundamentalsBackfillConfig struct {
	Conn               driver.Conn
	DSN                string
	Provider           PEBackfillProvider
	StartDate          time.Time
	EndDate            time.Time
	Symbols            []string
	IncrementalMode    string
	DiscoveryPageSize  int
	DiscoveryPageLimit int
	Workers            int
	BatchSize          int
	PageSize           int
	QPS                int
	LimitSymbols       int
	DryRun             bool
	DistributedLimiter DistributedRateLimitConfig
}

type DistributedRateLimitConfig struct {
	Enabled      bool
	Addr         string
	Password     string
	DB           int
	KeyPrefix    string
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type USFundamentalsBackfillResult struct {
	ProcessedSymbols int64
	FailedSymbols    int64
	ScannedBars      int64
	CandidateRows    int64
	InsertedRows     int64
	SkippedRows      int64
	FilteredSymbols  int64
	DiscoveryPages   int64
	DiscoveryRows    int64
	DiscoverySymbols int64
	SkippedFresh     int64
	DecodeErrors     int64
	NoQuarterInputs  int64
	MissingPrice     int64
	MissingTTMEPS    int64
	MissingBookValue int64
	ThrottledSymbols []string
}

type usFundamentalsBackfillStats struct {
	ScannedBars   int
	CandidateRows int
	InsertedRows  int
	SkippedRows   int
	Diagnostics   PEFetchDiagnostics
}

type fundamentalObservationKey struct {
	FactorCode string
	EventTS    int64
	KnownAt    int64
}

type existingFundamentalObservation struct {
	Value    float64
	Revision uint32
}

type existingFundamentalFreshness struct {
	LatestEventTS time.Time
	LatestKnownAt time.Time
}

type fmpFundamentalsDiscoveryCandidate struct {
	Symbol     string
	PeriodDate time.Time
	KnownAt    time.Time
}

type fmpFundamentalsDiscoveryResult struct {
	Mode             string
	Symbols          []string
	PagesFetched     int
	RowsScanned      int
	CandidateSymbols int
	SkippedFresh     int
	MinPeriodDate    time.Time
}

type fundamentalObservationInsert struct {
	Market     string
	Symbol     string
	FactorCode string
	EventTS    time.Time
	KnownAt    time.Time
	Source     string
	Value      float64
	Revision   uint32
}

func normalizeFMPFundamentalsIncrementalMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "off", "false", "none", "disabled":
		return ""
	case "sec", "sec-filings", "sec-filings-financials", "sec_filings_financials":
		return "sec-filings-financials"
	case "earnings", "earnings-calendar", "earnings_calendar":
		return "earnings-calendar"
	case "latest", "latest-financial-statements", "latest_financial_statements":
		return "latest-financial-statements"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func newFMPFundamentalsDiscoveryClient(provider PEBackfillProvider) (*fmp.Client, error) {
	fmpProvider, ok := provider.(*FMPPEBackfillProvider)
	if !ok || strings.TrimSpace(fmpProvider.apiKey) == "" {
		return nil, fmt.Errorf("FMP incremental discovery requires FMP provider")
	}
	return fmp.New(fmpProvider.apiKey), nil
}

func discoverFMPFundamentalsUpdateSymbols(ctx context.Context, conn driver.Conn, client *fmp.Client, mode string, startDate, endDate time.Time, pageSize, pageLimit, limitSymbols int) (fmpFundamentalsDiscoveryResult, error) {
	if client == nil {
		return fmpFundamentalsDiscoveryResult{}, fmt.Errorf("FMP discovery client is required")
	}
	mode = normalizeFMPFundamentalsIncrementalMode(mode)
	if mode == "" {
		mode = "sec-filings-financials"
	}
	if pageSize <= 0 {
		pageSize = 250
	}
	pageLimit = normalizeFMPDiscoveryPageLimit(pageLimit)
	localSymbols, err := queryUSStockSymbolSet(ctx, conn)
	if err != nil {
		return fmpFundamentalsDiscoveryResult{}, err
	}
	chain := fmpFundamentalsDiscoveryModeChain(mode)
	var lastErr error
	for index, discoveryMode := range chain {
		result, err := discoverFMPFundamentalsUpdateSymbolsMode(ctx, conn, client, localSymbols, discoveryMode, startDate, endDate, pageSize, pageLimit, limitSymbols)
		if err != nil {
			lastErr = err
			if index < len(chain)-1 {
				log.Printf("FMP fundamentals discovery mode=%s failed: %v; falling back to %s", discoveryMode, err, chain[index+1])
				continue
			}
			return fmpFundamentalsDiscoveryResult{}, err
		}
		if result.RowsScanned == 0 && index < len(chain)-1 {
			log.Printf("FMP fundamentals discovery mode=%s returned no rows; falling back to %s", discoveryMode, chain[index+1])
			continue
		}
		return result, nil
	}
	if lastErr != nil {
		return fmpFundamentalsDiscoveryResult{}, lastErr
	}
	return fmpFundamentalsDiscoveryResult{}, nil
}

func fmpFundamentalsDiscoveryModeChain(mode string) []string {
	switch normalizeFMPFundamentalsIncrementalMode(mode) {
	case "latest-financial-statements":
		return []string{"latest-financial-statements", "sec-filings-financials", "earnings-calendar"}
	case "earnings-calendar":
		return []string{"earnings-calendar"}
	case "sec-filings-financials", "":
		return []string{"sec-filings-financials", "earnings-calendar"}
	default:
		return []string{mode}
	}
}

func discoverFMPFundamentalsUpdateSymbolsMode(ctx context.Context, conn driver.Conn, client *fmp.Client, localSymbols map[string]struct{}, mode string, startDate, endDate time.Time, pageSize, pageLimit, limitSymbols int) (fmpFundamentalsDiscoveryResult, error) {
	candidatesBySymbol := map[string]fmpFundamentalsDiscoveryCandidate{}
	result := fmpFundamentalsDiscoveryResult{Mode: mode}
	from := normalizeDateOnly(startDate).Format("2006-01-02")
	to := normalizeDateOnly(endDate).Format("2006-01-02")
	appendCandidate := func(candidate fmpFundamentalsDiscoveryCandidate) {
		if !endDate.IsZero() && !candidate.KnownAt.IsZero() && candidate.KnownAt.After(normalizeDateOnly(endDate).AddDate(0, 0, 1)) {
			return
		}
		if len(localSymbols) > 0 {
			if _, ok := localSymbols[candidate.Symbol]; !ok {
				return
			}
		}
		current, exists := candidatesBySymbol[candidate.Symbol]
		if !exists || candidate.KnownAt.After(current.KnownAt) || (candidate.KnownAt.Equal(current.KnownAt) && candidate.PeriodDate.After(current.PeriodDate)) {
			candidatesBySymbol[candidate.Symbol] = candidate
		}
	}
	switch mode {
	case "latest-financial-statements":
		for page := 0; page < pageLimit; page++ {
			rows, err := client.LatestFinancialStatements(ctx, page, pageSize)
			if err != nil {
				return result, fmt.Errorf("FMP latest financial statements page %d: %w", page, err)
			}
			result.PagesFetched++
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				result.RowsScanned++
				candidate, ok := fmpLatestStatementCandidate(row)
				if !ok {
					continue
				}
				appendCandidate(candidate)
			}
			if limitSymbols > 0 && len(candidatesBySymbol) >= limitSymbols {
				break
			}
			if len(rows) < pageSize {
				break
			}
		}
	case "sec-filings-financials":
		for page := 0; page < pageLimit; page++ {
			rows, err := client.SecFilingsFinancials(ctx, from, to, page, pageSize)
			if err != nil {
				return result, fmt.Errorf("FMP sec filings financials page %d: %w", page, err)
			}
			result.PagesFetched++
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				result.RowsScanned++
				candidate, ok := fmpSecFilingsFinancialCandidate(row)
				if !ok {
					continue
				}
				appendCandidate(candidate)
			}
			if limitSymbols > 0 && len(candidatesBySymbol) >= limitSymbols {
				break
			}
			if len(rows) < pageSize {
				break
			}
		}
	case "earnings-calendar":
		rows, err := client.EarningsCalendar(ctx, from, to)
		if err != nil {
			return result, fmt.Errorf("FMP earnings calendar: %w", err)
		}
		result.PagesFetched = 1
		for _, row := range rows {
			result.RowsScanned++
			candidate, ok := fmpEarningsCalendarCandidate(row)
			if !ok {
				continue
			}
			appendCandidate(candidate)
		}
	default:
		return result, fmt.Errorf("unsupported FMP fundamentals incremental mode %q", mode)
	}
	return finalizeFMPFundamentalsDiscoveryResult(ctx, conn, result, candidatesBySymbol, limitSymbols)
}

func normalizeFMPDiscoveryPageLimit(value int) int {
	if value <= 0 {
		return maxFMPDiscoveryPages
	}
	if value > maxFMPDiscoveryPages {
		return maxFMPDiscoveryPages
	}
	return value
}

func fmpLatestStatementCandidate(row fmp.LatestFinancialStatement) (fmpFundamentalsDiscoveryCandidate, bool) {
	symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
	if symbol == "" || !isFMPFundamentalSymbolSupported(symbol) {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	periodDate, ok := parseFMPDate(row.Date)
	if !ok {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	knownAt := parseFMPKnownAt(row.AcceptedDate, row.FilingDate, row.Date)
	if knownAt.IsZero() {
		knownAt = periodDate
	}
	return fmpFundamentalsDiscoveryCandidate{Symbol: symbol, PeriodDate: periodDate, KnownAt: knownAt}, true
}

func fmpSecFilingsFinancialCandidate(row fmp.SecFilingsFinancial) (fmpFundamentalsDiscoveryCandidate, bool) {
	if !row.HasFinancials {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	if !isFMPFundamentalsDiscoveryFormTypeSupported(row.FormType) {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
	if symbol == "" || !isFMPFundamentalSymbolSupported(symbol) {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	knownAt := parseFMPKnownAt(row.AcceptedDate, row.FilingDate)
	if knownAt.IsZero() {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	return fmpFundamentalsDiscoveryCandidate{Symbol: symbol, PeriodDate: approximateFMPDiscoveryPeriodDate(knownAt), KnownAt: knownAt}, true
}

func isFMPFundamentalsDiscoveryFormTypeSupported(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "10-Q", "10-Q/A", "10-K", "10-K/A", "20-F", "20-F/A", "40-F", "40-F/A":
		return true
	default:
		return false
	}
}

func fmpEarningsCalendarCandidate(row fmp.EarningsCalendarEntry) (fmpFundamentalsDiscoveryCandidate, bool) {
	symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
	if symbol == "" || !isFMPFundamentalSymbolSupported(symbol) {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	knownAt := parseFMPKnownAt(row.LastUpdated, row.Date)
	if knownAt.IsZero() {
		return fmpFundamentalsDiscoveryCandidate{}, false
	}
	return fmpFundamentalsDiscoveryCandidate{Symbol: symbol, PeriodDate: approximateFMPDiscoveryPeriodDate(knownAt), KnownAt: knownAt}, true
}

func approximateFMPDiscoveryPeriodDate(knownAt time.Time) time.Time {
	if knownAt.IsZero() {
		return time.Time{}
	}
	return normalizeDateOnly(knownAt.AddDate(0, -6, 0))
}

func candidateSymbols(candidates []fmpFundamentalsDiscoveryCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Symbol)
	}
	return out
}

func fmpFundamentalCandidateIsFresh(candidate fmpFundamentalsDiscoveryCandidate, existing map[string]existingFundamentalFreshness) bool {
	pe, hasPE := existing[usStocksPEFactorCode]
	pb, hasPB := existing[usStocksPBFactorCode]
	if !hasPE || !hasPB {
		return false
	}
	if !candidate.PeriodDate.IsZero() {
		if pe.LatestEventTS.Before(candidate.PeriodDate) || pb.LatestEventTS.Before(candidate.PeriodDate) {
			return false
		}
	}
	if !candidate.KnownAt.IsZero() {
		if pe.LatestKnownAt.Before(candidate.KnownAt) || pb.LatestKnownAt.Before(candidate.KnownAt) {
			return false
		}
	}
	return true
}

func finalizeFMPFundamentalsDiscoveryResult(ctx context.Context, conn driver.Conn, result fmpFundamentalsDiscoveryResult, candidatesBySymbol map[string]fmpFundamentalsDiscoveryCandidate, limitSymbols int) (fmpFundamentalsDiscoveryResult, error) {
	if len(candidatesBySymbol) == 0 {
		return result, nil
	}
	candidates := make([]fmpFundamentalsDiscoveryCandidate, 0, len(candidatesBySymbol))
	for _, candidate := range candidatesBySymbol {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].KnownAt.Equal(candidates[right].KnownAt) {
			return candidates[left].Symbol < candidates[right].Symbol
		}
		return candidates[left].KnownAt.After(candidates[right].KnownAt)
	})
	if limitSymbols > 0 && len(candidates) > limitSymbols {
		candidates = candidates[:limitSymbols]
	}
	result.CandidateSymbols = len(candidates)
	freshness, err := loadExistingFMPFundamentalFreshness(ctx, conn, candidateSymbols(candidates))
	if err != nil {
		return result, err
	}
	for _, candidate := range candidates {
		if existing, ok := freshness[candidate.Symbol]; ok && fmpFundamentalCandidateIsFresh(candidate, existing) {
			result.SkippedFresh++
			continue
		}
		result.Symbols = append(result.Symbols, candidate.Symbol)
		if !candidate.PeriodDate.IsZero() && (result.MinPeriodDate.IsZero() || candidate.PeriodDate.Before(result.MinPeriodDate)) {
			result.MinPeriodDate = candidate.PeriodDate
		}
	}
	sort.Strings(result.Symbols)
	return result, nil
}

func queryUSStockSymbolSet(ctx context.Context, conn driver.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, `SELECT symbol FROM us_stocks_bar_1m GROUP BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("query local US stock symbol set: %w", err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, fmt.Errorf("scan local US stock symbol: %w", err)
		}
		if normalized := strings.ToUpper(strings.TrimSpace(symbol)); normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate local US stock symbols: %w", err)
	}
	return out, nil
}

func loadExistingFMPFundamentalFreshness(ctx context.Context, conn driver.Conn, symbols []string) (map[string]map[string]existingFundamentalFreshness, error) {
	out := map[string]map[string]existingFundamentalFreshness{}
	if len(symbols) == 0 {
		return out, nil
	}
	rows, err := conn.Query(ctx, `SELECT symbol, factor_code, max(event_ts), max(known_at)
FROM fundamental_observation
WHERE market = {market:String}
  AND source = {source:String}
  AND factor_code IN ({factors:Array(String)})
  AND symbol IN ({symbols:Array(String)})
GROUP BY symbol, factor_code`,
		clickhouse.Named("market", usStocksFundamentalsMarket),
		clickhouse.Named("source", fmpQuarterStatementsSource),
		clickhouse.Named("factors", []string{usStocksPEFactorCode, usStocksPBFactorCode}),
		clickhouse.Named("symbols", symbols),
	)
	if err != nil {
		return nil, fmt.Errorf("query existing FMP fundamentals freshness: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var symbol, factorCode string
		var latestEventTS, latestKnownAt time.Time
		if err := rows.Scan(&symbol, &factorCode, &latestEventTS, &latestKnownAt); err != nil {
			return nil, fmt.Errorf("scan existing FMP fundamentals freshness: %w", err)
		}
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if out[symbol] == nil {
			out[symbol] = map[string]existingFundamentalFreshness{}
		}
		out[symbol][factorCode] = existingFundamentalFreshness{LatestEventTS: latestEventTS.UTC(), LatestKnownAt: latestKnownAt.UTC()}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing FMP fundamentals freshness: %w", err)
	}
	return out, nil
}

func hasExistingFMPFundamentalObservations(ctx context.Context, conn driver.Conn) (bool, error) {
	rows, err := conn.Query(ctx, `SELECT count()
FROM fundamental_observation
WHERE market = {market:String}
  AND source = {source:String}
  AND factor_code IN ({factors:Array(String)})
LIMIT 1`,
		clickhouse.Named("market", usStocksFundamentalsMarket),
		clickhouse.Named("source", fmpQuarterStatementsSource),
		clickhouse.Named("factors", []string{usStocksPEFactorCode, usStocksPBFactorCode}),
	)
	if err != nil {
		return false, fmt.Errorf("query existing FMP fundamentals presence: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, fmt.Errorf("scan existing FMP fundamentals presence: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate existing FMP fundamentals presence: %w", err)
	}
	return count > 0, nil
}

type requestLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

// newRequestLimiter implements a simple shared token cadence across workers.
// The goal here is to keep Tiger-like quota-bound providers below a global QPS.
func newRequestLimiter(qps int) *requestLimiter {
	if qps <= 0 {
		return nil
	}
	return &requestLimiter{interval: time.Second / time.Duration(qps)}
}

func (l *requestLimiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	now := time.Now()
	wakeAt := now
	if l.next.After(now) {
		wakeAt = l.next
	}
	l.next = wakeAt.Add(l.interval)
	l.mu.Unlock()

	if !wakeAt.After(now) {
		return nil
	}

	timer := time.NewTimer(time.Until(wakeAt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *requestLimiter) Backoff(_ context.Context, cooldown time.Duration) error {
	if l == nil || cooldown <= 0 {
		return nil
	}
	penaltyUntil := time.Now().Add(cooldown)
	l.mu.Lock()
	if l.next.Before(penaltyUntil) {
		l.next = penaltyUntil
	}
	l.mu.Unlock()
	return nil
}

func newBackfillRateLimiter(ctx context.Context, cfg USFundamentalsBackfillConfig) (backfillRateLimiter, func() error, error) {
	if cfg.QPS <= 0 {
		return newRequestLimiter(0), func() error { return nil }, nil
	}
	if cfg.DistributedLimiter.Enabled {
		limiter, err := newRedisRequestLimiter(ctx, cfg.Provider.Name(), cfg.QPS, cfg.DistributedLimiter)
		if err != nil {
			return nil, nil, err
		}
		return limiter, limiter.Close, nil
	}
	return newRequestLimiter(cfg.QPS), func() error { return nil }, nil
}

func BackfillUSStockPE(ctx context.Context, cfg USFundamentalsBackfillConfig) (USFundamentalsBackfillResult, error) {
	if cfg.Conn == nil {
		return USFundamentalsBackfillResult{}, fmt.Errorf("clickhouse connection is required")
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return USFundamentalsBackfillResult{}, fmt.Errorf("clickhouse DSN is required")
	}
	if cfg.Provider == nil {
		return USFundamentalsBackfillResult{}, fmt.Errorf("fundamentals provider is required")
	}
	if err := cfg.Provider.Validate(); err != nil {
		return USFundamentalsBackfillResult{}, err
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.PageSize <= 0 {
		cfg.PageSize = 251
	}
	if cfg.EndDate.Before(cfg.StartDate) {
		return USFundamentalsBackfillResult{}, fmt.Errorf("end date must be on or after start date")
	}
	discoveryPageSize := cfg.DiscoveryPageSize
	if discoveryPageSize <= 0 {
		discoveryPageSize = 250
	}
	discoveryPageLimit := cfg.DiscoveryPageLimit
	if discoveryPageLimit <= 0 {
		discoveryPageLimit = 8
	}
	incrementalMode := normalizeFMPFundamentalsIncrementalMode(cfg.IncrementalMode)
	if strings.TrimSpace(cfg.IncrementalMode) == "" && strings.EqualFold(cfg.Provider.Name(), "fmp") && len(cfg.Symbols) == 0 {
		incrementalMode = "sec-filings-financials"
	}

	var discovery fmpFundamentalsDiscoveryResult
	var symbols []string
	var err error
	hasExistingFMPFundamentals := false
	if incrementalMode != "" && strings.EqualFold(cfg.Provider.Name(), "fmp") && len(cfg.Symbols) == 0 {
		hasExistingFMPFundamentals, err = hasExistingFMPFundamentalObservations(ctx, cfg.Conn)
		if err != nil {
			return USFundamentalsBackfillResult{}, err
		}
		if hasExistingFMPFundamentals {
			discoveryClient, err := newFMPFundamentalsDiscoveryClient(cfg.Provider)
			if err != nil {
				return USFundamentalsBackfillResult{}, err
			}
			discovery, err = discoverFMPFundamentalsUpdateSymbols(ctx, cfg.Conn, discoveryClient, incrementalMode, cfg.StartDate, cfg.EndDate, discoveryPageSize, discoveryPageLimit, cfg.LimitSymbols)
			if err != nil {
				return USFundamentalsBackfillResult{}, err
			}
			symbols = discovery.Symbols
			log.Printf("FMP fundamentals discovery mode=%s pages=%d rows=%d candidates=%d skipped_fresh=%d tasks=%d", discovery.Mode, discovery.PagesFetched, discovery.RowsScanned, discovery.CandidateSymbols, discovery.SkippedFresh, len(symbols))
		} else {
			log.Printf("FMP fundamentals discovery disabled for cold start: no existing FMP PE/PB observations")
			symbols, err = ResolveUSStockSymbols(ctx, cfg.Conn, cfg.Symbols, cfg.LimitSymbols)
			if err != nil {
				return USFundamentalsBackfillResult{}, err
			}
		}
	} else {
		symbols, err = ResolveUSStockSymbols(ctx, cfg.Conn, cfg.Symbols, cfg.LimitSymbols)
		if err != nil {
			return USFundamentalsBackfillResult{}, err
		}
	}
	filteredSymbols := int64(0)
	if strings.EqualFold(cfg.Provider.Name(), "fmp") {
		originalCount := len(symbols)
		symbols = filterFMPFundamentalSymbols(symbols)
		if dropped := originalCount - len(symbols); dropped > 0 {
			filteredSymbols = int64(dropped)
			log.Printf("Filtered %d non-common-share symbols from FMP fundamentals universe", dropped)
		}
	}
	if len(symbols) == 0 {
		return USFundamentalsBackfillResult{FilteredSymbols: filteredSymbols, DiscoveryPages: int64(discovery.PagesFetched), DiscoveryRows: int64(discovery.RowsScanned), DiscoverySymbols: int64(discovery.CandidateSymbols), SkippedFresh: int64(discovery.SkippedFresh)}, nil
	}

	limiter, closeLimiter, err := newBackfillRateLimiter(ctx, cfg)
	if err != nil {
		return USFundamentalsBackfillResult{}, fmt.Errorf("build request limiter: %w", err)
	}
	defer func() {
		_ = closeLimiter()
	}()

	if !cfg.DryRun {
		if err := ensureFundamentalFactorCatalogRows(ctx, cfg.Conn); err != nil {
			return USFundamentalsBackfillResult{}, err
		}
	}

	log.Printf("Found %d US symbols to backfill fundamentals between %s and %s using provider=%s", len(symbols), cfg.StartDate.Format("2006-01-02"), cfg.EndDate.Format("2006-01-02"), cfg.Provider.Name())
	fetchStartDate := cfg.StartDate
	if !discovery.MinPeriodDate.IsZero() && discovery.MinPeriodDate.Before(fetchStartDate) {
		fetchStartDate = discovery.MinPeriodDate
	}

	taskCh := make(chan string)
	var (
		wg               sync.WaitGroup
		processedSymbols int64
		failedSymbols    int64
		scannedBars      int64
		candidateRows    int64
		insertedRows     int64
		skippedRows      int64
		decodeErrors     int64
		noQuarterInputs  int64
		missingPrice     int64
		missingTTMEPS    int64
		missingBookValue int64
		throttledMu      sync.Mutex
		throttledSymbols = make(map[string]struct{})
	)

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerConn, err := ConnectClickHouse(ctx, cfg.DSN)
			if err != nil {
				log.Printf("[ERROR] worker %d connect ClickHouse: %v", workerID, err)
				atomic.AddInt64(&failedSymbols, int64(len(symbols)))
				return
			}

			providerWorker, err := cfg.Provider.NewWorker(ctx)
			if err != nil {
				log.Printf("[ERROR] worker %d init provider %s: %v", workerID, cfg.Provider.Name(), err)
				atomic.AddInt64(&failedSymbols, int64(len(symbols)))
				return
			}

			for symbol := range taskCh {
				stats, err := backfillUSStockSymbolPE(ctx, workerConn, providerWorker, symbol, fetchStartDate, cfg.EndDate, cfg.PageSize, cfg.BatchSize, limiter, cfg.DryRun)
				if err != nil {
					log.Printf("[ERROR] %s: %v", symbol, err)
					atomic.AddInt64(&failedSymbols, 1)
					if fmp.IsHTTPStatus(err, http.StatusTooManyRequests) {
						throttledMu.Lock()
						throttledSymbols[symbol] = struct{}{}
						throttledMu.Unlock()
						cooldown := fmp.RetryAfterDelay(err)
						if cooldown <= 0 {
							cooldown = 30 * time.Second
						}
						if backoffErr := limiter.Backoff(ctx, cooldown); backoffErr != nil {
							log.Printf("[WARN] %s: apply FMP limiter backoff: %v", symbol, backoffErr)
						}
					}
					if strings.Contains(strings.ToLower(err.Error()), "decode response") {
						atomic.AddInt64(&decodeErrors, 1)
					}
					continue
				}

				atomic.AddInt64(&processedSymbols, 1)
				atomic.AddInt64(&scannedBars, int64(stats.ScannedBars))
				atomic.AddInt64(&candidateRows, int64(stats.CandidateRows))
				atomic.AddInt64(&insertedRows, int64(stats.InsertedRows))
				atomic.AddInt64(&skippedRows, int64(stats.SkippedRows))
				atomic.AddInt64(&noQuarterInputs, int64(stats.Diagnostics.NoQuarterInputs))
				atomic.AddInt64(&missingPrice, int64(stats.Diagnostics.MissingPrice))
				atomic.AddInt64(&missingTTMEPS, int64(stats.Diagnostics.MissingTTMEPS))
				atomic.AddInt64(&missingBookValue, int64(stats.Diagnostics.MissingBookValue))

				mode := "BACKFILLED"
				if cfg.DryRun {
					mode = "DRYRUN"
				}
				log.Printf("[%s] %s: scanned_bars=%d candidate_rows=%d inserted_rows=%d skipped_rows=%d",
					mode,
					symbol,
					stats.ScannedBars,
					stats.CandidateRows,
					stats.InsertedRows,
					stats.SkippedRows,
				)
			}
		}(i + 1)
	}

	for _, symbol := range symbols {
		taskCh <- symbol
	}
	close(taskCh)
	wg.Wait()

	result := USFundamentalsBackfillResult{
		ProcessedSymbols: processedSymbols,
		FailedSymbols:    failedSymbols,
		ScannedBars:      scannedBars,
		CandidateRows:    candidateRows,
		InsertedRows:     insertedRows,
		SkippedRows:      skippedRows,
		FilteredSymbols:  filteredSymbols,
		DiscoveryPages:   int64(discovery.PagesFetched),
		DiscoveryRows:    int64(discovery.RowsScanned),
		DiscoverySymbols: int64(discovery.CandidateSymbols),
		SkippedFresh:     int64(discovery.SkippedFresh),
		DecodeErrors:     decodeErrors,
		NoQuarterInputs:  noQuarterInputs,
		MissingPrice:     missingPrice,
		MissingTTMEPS:    missingTTMEPS,
		MissingBookValue: missingBookValue,
	}
	if len(throttledSymbols) > 0 {
		result.ThrottledSymbols = make([]string, 0, len(throttledSymbols))
		for symbol := range throttledSymbols {
			result.ThrottledSymbols = append(result.ThrottledSymbols, symbol)
		}
		sort.Strings(result.ThrottledSymbols)
	}
	log.Printf("FMP fundamentals summary: processed=%d failed=%d discovery_symbols=%d skipped_fresh=%d scanned_bars=%d candidate_rows=%d inserted_rows=%d skipped_rows=%d no_quarter_inputs=%d missing_ttm_eps=%d missing_book_value=%d missing_price=%d",
		result.ProcessedSymbols,
		result.FailedSymbols,
		result.DiscoverySymbols,
		result.SkippedFresh,
		result.ScannedBars,
		result.CandidateRows,
		result.InsertedRows,
		result.SkippedRows,
		result.NoQuarterInputs,
		result.MissingTTMEPS,
		result.MissingBookValue,
		result.MissingPrice,
	)
	if failedSymbols > 0 {
		return result, fmt.Errorf("US PE backfill finished with %d failed symbols", failedSymbols)
	}
	return result, nil
}

// ResolveUSStockSymbols uses the local stock bar table as the current symbol
// universe. This keeps bulk sync scope aligned with symbols already present in
// ClickHouse. When symbols is non-empty it simply normalizes/deduplicates the
// provided values.
func ResolveUSStockSymbols(ctx context.Context, conn driver.Conn, symbols []string, limit int) ([]string, error) {
	targets, err := ResolveUSStockSyncTargets(ctx, conn, symbols, limit, false)
	if err != nil {
		return nil, err
	}
	return storeSymbolsFromSyncTargets(targets), nil
}

func resolveStoredUSStockSymbols(ctx context.Context, conn driver.Conn, symbols []string, limit int) ([]string, error) {
	if len(symbols) > 0 {
		return normalizeFundamentalSymbols(symbols), nil
	}

	query := `SELECT symbol
FROM us_stocks_bar_1m
GROUP BY symbol
ORDER BY symbol`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query US stock symbols: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, fmt.Errorf("scan US stock symbol: %w", err)
		}
		out = append(out, strings.ToUpper(strings.TrimSpace(symbol)))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock symbols: %w", err)
	}
	prioritized, err := PrioritizeUSSymbols(ctx, conn, out)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(prioritized) > limit {
		prioritized = prioritized[:limit]
	}
	return prioritized, nil
}

func normalizeFundamentalSymbols(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalized := strings.ToUpper(strings.TrimSpace(symbol))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func filterFMPFundamentalSymbols(symbols []string) []string {
	filtered := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if isFMPFundamentalSymbolSupported(symbol) {
			filtered = append(filtered, symbol)
		}
	}
	return filtered
}

func isFMPFundamentalSymbolSupported(symbol string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return false
	}
	unsupportedSuffixes := []string{
		".U", ".UN", ".UT", ".WS", ".W", ".WT", ".R", ".RT",
		".RIGHT", ".P", ".PR", ".CVR",
	}
	for _, suffix := range unsupportedSuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return false
		}
	}
	unsupportedCompactSuffixes := []string{"UN", "UT", "WS", "WT", "RT", "W", "U", "R"}
	for _, suffix := range unsupportedCompactSuffixes {
		if len(normalized) > len(suffix)+2 && strings.HasSuffix(normalized, suffix) {
			return false
		}
	}
	return true
}

func ensureFundamentalFactorCatalogRows(ctx context.Context, conn driver.Conn) error {
	factors := []struct {
		Code        string
		DisplayName string
		Description string
	}{
		{
			Code:        usStocksPEFactorCode,
			DisplayName: "Price-to-Earnings",
			Description: "Trailing twelve month PE from an external fundamentals provider",
		},
		{
			Code:        usStocksPBFactorCode,
			DisplayName: "Price-to-Book",
			Description: "Price-to-book ratio from an external fundamentals provider",
		},
	}

	for _, factor := range factors {
		if err := ensureFundamentalFactorCatalogRow(ctx, conn, factor.Code, factor.DisplayName, factor.Description); err != nil {
			return err
		}
	}
	return nil
}

func ensureFundamentalFactorCatalogRow(ctx context.Context, conn driver.Conn, factorCode, displayName, description string) error {
	rows, err := conn.Query(ctx,
		`SELECT count()
		FROM fundamental_factor_catalog
		WHERE market = {market:String} AND factor_code = {factor:String}`,
		clickhouse.Named("market", usStocksFundamentalsMarket),
		clickhouse.Named("factor", factorCode),
	)
	if err != nil {
		return fmt.Errorf("query %s factor catalog presence: %w", factorCode, err)
	}
	defer rows.Close()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return fmt.Errorf("scan %s factor catalog presence: %w", factorCode, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s factor catalog presence: %w", factorCode, err)
	}
	if count > 0 {
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, `INSERT INTO fundamental_factor_catalog (
		market, factor_code, display_name, description, value_type, unit,
		preferred_frequency, fill_policy, fill_max_days, point_in_time,
		source, active, sla_hours, metadata
	)`)
	if err != nil {
		return fmt.Errorf("prepare %s factor catalog insert: %w", factorCode, err)
	}
	if err := batch.Append(
		usStocksFundamentalsMarket,
		factorCode,
		displayName,
		description,
		"ratio",
		"",
		"quarterly",
		"forward_fill",
		uint16(0),
		uint8(1),
		usStocksPECatalogSource,
		uint8(1),
		uint32(24),
		`{"notes":"provider-specific raw field mapping lives inside each provider adapter"}`,
	); err != nil {
		return fmt.Errorf("append %s factor catalog row: %w", factorCode, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert %s factor catalog row: %w", factorCode, err)
	}
	return nil
}

func backfillUSStockSymbolPE(ctx context.Context, conn driver.Conn, worker PEBackfillWorker, symbol string, startDate, endDate time.Time, pageSize, batchSize int, limiter backfillRateLimiter, dryRun bool) (usFundamentalsBackfillStats, error) {
	fetched, err := worker.FetchSymbolPE(ctx, symbol, startDate, endDate, pageSize, limiter)
	if err != nil {
		return usFundamentalsBackfillStats{}, err
	}

	observations := fetched.Observations
	stats := usFundamentalsBackfillStats{ScannedBars: fetched.ScannedBars}
	stats.Diagnostics = fetched.Diagnostics
	if len(observations) < fetched.ScannedBars {
		stats.SkippedRows = fetched.ScannedBars - len(observations)
	}
	if len(observations) == 0 {
		return stats, nil
	}

	existing, err := loadExistingFundamentalObservations(ctx, conn, symbol, observations[0].Source, startDate, endDate.AddDate(0, 0, 1))
	if err != nil {
		return stats, err
	}

	planned, skippedExisting := planFundamentalObservationInserts(observations, existing)
	stats.CandidateRows = len(planned)
	stats.SkippedRows += skippedExisting
	if dryRun || len(planned) == 0 {
		return stats, nil
	}

	inserted, err := insertFundamentalObservations(ctx, conn, planned, batchSize)
	if err != nil {
		return stats, err
	}
	stats.InsertedRows = int(inserted)
	return stats, nil
}

// fetchTigerStockPEBars is intentionally Tiger-specific. The provider
// abstraction keeps this code sealed so a future FMP adapter can coexist
// without reworking the backfill engine.
func fetchTigerStockPEBars(ctx context.Context, client *tigerapi.Client, symbol string, startDate, endDate time.Time, pageSize int, limiter backfillRateLimiter) ([]tigerapi.KlineBar, error) {
	begin := normalizeDateOnly(startDate).Format("2006-01-02")
	endExclusive := normalizeDateOnly(endDate).AddDate(0, 0, 1).Format("2006-01-02")
	pageToken := ""
	seenTokens := make(map[string]struct{})
	out := make([]tigerapi.KlineBar, 0)

	for {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}
		page, err := client.StockKlinesPage(tigerapi.StockKlineRequest{
			Symbol:          symbol,
			Period:          "day",
			BeginTime:       begin,
			EndTime:         endExclusive,
			Limit:           pageSize,
			PageToken:       pageToken,
			WithFundamental: true,
		})
		if err != nil {
			return nil, fmt.Errorf("fetch Tiger kline page: %w", err)
		}
		out = append(out, page.Bars...)
		nextPageToken := strings.TrimSpace(page.NextPageToken)
		if nextPageToken == "" {
			break
		}
		if _, ok := seenTokens[nextPageToken]; ok {
			return nil, fmt.Errorf("Tiger kline page token repeated for %s: %s", symbol, nextPageToken)
		}
		seenTokens[nextPageToken] = struct{}{}
		pageToken = nextPageToken
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Time == out[j].Time {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Time < out[j].Time
	})
	return out, nil
}

// extractPEObservationsFromTigerBars normalizes Tiger's raw kline payload into
// the generic tall fundamental_observation format used by ClickHouse.
func extractPEObservationsFromTigerBars(symbol string, bars []tigerapi.KlineBar) []fundamentalObservationInsert {
	out := make([]fundamentalObservationInsert, 0, len(bars))
	for _, bar := range bars {
		value, ok := lookupPEValue(bar.Fundamentals)
		if !ok {
			continue
		}
		ts := time.UnixMilli(bar.Time).UTC()
		out = append(out, fundamentalObservationInsert{
			Market:     usStocksFundamentalsMarket,
			Symbol:     strings.ToUpper(strings.TrimSpace(symbol)),
			FactorCode: usStocksPEFactorCode,
			EventTS:    ts,
			KnownAt:    ts,
			Source:     tigerKlineFundamentalSrc,
			Value:      value,
		})
	}
	return out
}

// lookupTigerPEValue knows Tiger's current raw field aliases for TTM PE. This
// intentionally lives inside the Tiger adapter rather than the generic backfill
// path, so alternative providers can bring their own mapping later.
func lookupPEValue(fundamentals map[string]any) (float64, bool) {
	if len(fundamentals) == 0 {
		return 0, false
	}
	for _, key := range []string{tigerTTMPEField, "ttm_pe_rate", "peRatio", "pe_ratio"} {
		value, ok := fundamentals[key]
		if !ok {
			continue
		}
		parsed, ok := numericAnyToFloat64(value)
		if !ok || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func numericAnyToFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func loadExistingFundamentalObservations(ctx context.Context, conn driver.Conn, symbol string, source string, startDate, endDate time.Time) (map[fundamentalObservationKey]existingFundamentalObservation, error) {
	rows, err := conn.Query(ctx,
		`SELECT factor_code, event_ts, known_at, value, revision
		FROM fundamental_observation
		WHERE market = {market:String}
		  AND symbol = {symbol:String}
		  AND source = {source:String}
		  AND event_ts >= toDateTime({from:String}, 'UTC')
		  AND event_ts < toDateTime({to:String}, 'UTC')
		ORDER BY factor_code, event_ts, known_at, revision`,
		clickhouse.Named("market", usStocksFundamentalsMarket),
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("source", source),
		clickhouse.Named("from", normalizeDateOnly(startDate).Format("2006-01-02 15:04:05")),
		clickhouse.Named("to", normalizeDateOnly(endDate).Format("2006-01-02 15:04:05")),
	)
	if err != nil {
		return nil, fmt.Errorf("query existing fundamental observations: %w", err)
	}
	defer rows.Close()

	out := make(map[fundamentalObservationKey]existingFundamentalObservation)
	for rows.Next() {
		var (
			factorCode string
			eventTS    time.Time
			knownAt    time.Time
			value      float64
			revision   uint32
		)
		if err := rows.Scan(&factorCode, &eventTS, &knownAt, &value, &revision); err != nil {
			return nil, fmt.Errorf("scan existing fundamental observation: %w", err)
		}
		key := fundamentalObservationKey{FactorCode: factorCode, EventTS: eventTS.UTC().UnixMilli(), KnownAt: knownAt.UTC().UnixMilli()}
		if current, ok := out[key]; !ok || revision >= current.Revision {
			out[key] = existingFundamentalObservation{Value: value, Revision: revision}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing fundamental observations: %w", err)
	}
	return out, nil
}

func planFundamentalObservationInserts(observations []fundamentalObservationInsert, existing map[fundamentalObservationKey]existingFundamentalObservation) ([]fundamentalObservationInsert, int) {
	planned := make([]fundamentalObservationInsert, 0, len(observations))
	skipped := 0
	for _, observation := range observations {
		key := fundamentalObservationKey{FactorCode: observation.FactorCode, EventTS: observation.EventTS.UTC().UnixMilli(), KnownAt: observation.KnownAt.UTC().UnixMilli()}
		current, ok := existing[key]
		if ok && almostEqualFloat(current.Value, observation.Value) {
			skipped++
			continue
		}
		if ok {
			observation.Revision = current.Revision + 1
		}
		planned = append(planned, observation)
	}
	return planned, skipped
}

func insertFundamentalObservations(ctx context.Context, conn driver.Conn, rows []fundamentalObservationInsert, batchSize int) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = len(rows)
	}

	prepareBatch := func() (driver.Batch, error) {
		return conn.PrepareBatch(ctx, `INSERT INTO fundamental_observation (
			market, symbol, factor_code, event_ts, known_at, source, value, revision
		)`)
	}

	batch, err := prepareBatch()
	if err != nil {
		return 0, fmt.Errorf("prepare fundamentals batch: %w", err)
	}

	var total int64
	pending := 0
	for _, row := range rows {
		if err := batch.Append(
			row.Market,
			row.Symbol,
			row.FactorCode,
			row.EventTS,
			row.KnownAt,
			row.Source,
			row.Value,
			row.Revision,
		); err != nil {
			return total, fmt.Errorf("append fundamentals row: %w", err)
		}
		total++
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return total, fmt.Errorf("send fundamentals batch: %w", err)
			}
			pending = 0
			batch, err = prepareBatch()
			if err != nil {
				return total, fmt.Errorf("prepare next fundamentals batch: %w", err)
			}
		}
	}
	if pending > 0 {
		if err := batch.Send(); err != nil {
			return total, fmt.Errorf("send final fundamentals batch: %w", err)
		}
	}
	return total, nil
}

func almostEqualFloat(left, right float64) bool {
	const epsilon = 1e-9
	return math.Abs(left-right) <= epsilon
}

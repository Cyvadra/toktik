package jobs

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/calendarrepo"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/forexmarket"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/internal/usmarket/macro"
	"github.com/Cyvadra/toktik/pkg/fmp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const defaultForexWatchlistFile = "signal-list/forex-fmp-watchlist.txt"

type FMPCryptoSpotConfig struct {
	APIKey            string
	Symbols           []string
	ResolveAtStartup  bool
	LimitSymbols      int
	Interval          fmp.IntradayInterval
	BatchSize         int
	PriceSource       string
	ColdStartFloorUTC time.Time
}

type fmpCryptoSpot struct{ cfg FMPCryptoSpotConfig }

func NewFMPCryptoSpot(cfg FMPCryptoSpotConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_crypto_spot: APIKey is required")
	}
	cfg.Symbols = normalizeSymbols(cfg.Symbols)
	if len(cfg.Symbols) == 0 && !cfg.ResolveAtStartup {
		return nil, fmt.Errorf("fmp_crypto_spot: Symbols is empty and ResolveAtStartup is false")
	}
	if cfg.Interval == "" {
		cfg.Interval = fmp.Interval1Min
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50000
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpCryptoSpot{cfg: cfg}, nil
}

func (s *fmpCryptoSpot) Name() string { return "fmp_crypto_spot" }
func (s *fmpCryptoSpot) SourceKeys(ctx context.Context, conn driver.Conn) ([]string, error) {
	if len(s.cfg.Symbols) > 0 {
		return s.cfg.Symbols, nil
	}
	return queryDistinctSymbols(ctx, conn, "crypto_spot_bar_1m", "symbol", s.cfg.LimitSymbols, "USD")
}
func (s *fmpCryptoSpot) ResolveCursor(ctx context.Context, conn driver.Conn, sourceKey string) (time.Time, bool, error) {
	base := strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(sourceKey)), "USD")
	return queryLatestDate(ctx, conn, "crypto_spot_bar_1m", "timestamp", "symbol = {symbol:String}", clickhouse.Named("symbol", base))
}
func (s *fmpCryptoSpot) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpCryptoSpot) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	res, err := cryptooptions.SyncFMPSpotBars(ctx, conn, cryptooptions.FMPSpotSyncConfig{APIKey: s.cfg.APIKey, Symbols: []string{req.SourceKey}, From: req.From, To: req.To, Interval: s.cfg.Interval, BatchSize: s.cfg.BatchSize, PriceSource: s.cfg.PriceSource, DryRun: req.DryRun, Replace: !req.DryRun})
	return syncResult(req, res.InsertedRows), err
}
func (s *fmpCryptoSpot) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	base := strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(sourceKey)), "USD")
	return []syncpipeline.AuditTarget{{Table: "crypto_spot_bar_1m", DateColumn: "toDate(timestamp)", KeyColumns: []string{"timestamp", "symbol", "price_source"}, SourceFilter: fmt.Sprintf("symbol = '%s'", escapeStringLiteral(base))}}
}
func (s *fmpCryptoSpot) MaxConcurrency() int { return 1 }

type FMPForexConfig struct {
	APIKey            string
	Symbols           []string
	SymbolsFile       string
	ResolveAtStartup  bool
	LimitSymbols      int
	Interval          fmp.IntradayInterval
	BatchSize         int
	ColdStartFloorUTC time.Time
}

type fmpForex struct{ cfg FMPForexConfig }

func NewFMPForex(cfg FMPForexConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_forex: APIKey is required")
	}
	cfg.Symbols = normalizeSymbols(cfg.Symbols)
	if len(cfg.Symbols) == 0 && !cfg.ResolveAtStartup {
		return nil, fmt.Errorf("fmp_forex: Symbols is empty and ResolveAtStartup is false")
	}
	if cfg.Interval == "" {
		cfg.Interval = fmp.Interval1Min
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50000
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpForex{cfg: cfg}, nil
}

func (s *fmpForex) Name() string { return "fmp_forex" }
func (s *fmpForex) SourceKeys(ctx context.Context, conn driver.Conn) ([]string, error) {
	if len(s.cfg.Symbols) > 0 {
		return s.cfg.Symbols, nil
	}
	if symbols, path, err := resolveForexSymbolsFile(s.cfg.SymbolsFile); err != nil {
		return nil, fmt.Errorf("fmp_forex: resolve symbols file %s: %w", path, err)
	} else if len(symbols) > 0 {
		return symbols, nil
	}
	return queryDistinctSymbols(ctx, conn, "forex_bar_1m", "symbol", s.cfg.LimitSymbols, "")
}
func (s *fmpForex) ResolveCursor(ctx context.Context, conn driver.Conn, sourceKey string) (time.Time, bool, error) {
	return queryLatestDate(ctx, conn, "forex_bar_1m", "market_date", "symbol = {symbol:String}", clickhouse.Named("symbol", strings.ToUpper(strings.TrimSpace(sourceKey))))
}
func (s *fmpForex) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpForex) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	res, err := forexmarket.SyncFMPKlines(ctx, conn, forexmarket.FMPKlineSyncConfig{APIKey: s.cfg.APIKey, Symbols: []string{req.SourceKey}, From: req.From, To: req.To, Interval: s.cfg.Interval, BatchSize: s.cfg.BatchSize, DryRun: req.DryRun, Replace: !req.DryRun})
	return syncResult(req, res.InsertedRows), err
}
func (s *fmpForex) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "forex_bar_1m", DateColumn: "market_date", KeyColumns: []string{"timestamp", "symbol"}, SourceFilter: fmt.Sprintf("symbol = '%s'", escapeStringLiteral(strings.ToUpper(strings.TrimSpace(sourceKey))))}}
}
func (s *fmpForex) MaxConcurrency() int { return 1 }

func resolveForexSymbolsFile(rawPath string) ([]string, string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		path = defaultForexWatchlistFile
	}
	symbols, err := readSymbolFile(path)
	if err == nil {
		return symbols, path, nil
	}
	if strings.TrimSpace(rawPath) != "" || !os.IsNotExist(err) {
		return nil, path, err
	}
	return nil, path, nil
}

func readSymbolFile(path string) ([]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	seen := make(map[string]struct{})
	var symbols []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		symbol := strings.ToUpper(strings.TrimSpace(line))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Strings(symbols)
	return symbols, nil
}

type FMPUSStocksConfig struct {
	APIKey                   string
	Symbols                  []string
	ResolveAtStartup         bool
	IncludeOptionGapMappings bool
	LimitSymbols             int
	Interval                 fmp.IntradayInterval
	BatchSize                int
	ColdStartFloorUTC        time.Time
}

type fmpUSStocks struct{ cfg FMPUSStocksConfig }

func NewFMPUSStocks(cfg FMPUSStocksConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_us_stocks: APIKey is required")
	}
	cfg.Symbols = normalizeSymbols(cfg.Symbols)
	if len(cfg.Symbols) == 0 && !cfg.ResolveAtStartup {
		return nil, fmt.Errorf("fmp_us_stocks: Symbols is empty and ResolveAtStartup is false")
	}
	if cfg.Interval == "" {
		cfg.Interval = fmp.Interval1Min
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50000
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpUSStocks{cfg: cfg}, nil
}

func (s *fmpUSStocks) Name() string { return "fmp_us_stocks" }
func (s *fmpUSStocks) SourceKeys(ctx context.Context, conn driver.Conn) ([]string, error) {
	return resolveUSStockSourceKeys(ctx, conn, s.cfg.Symbols, s.cfg.LimitSymbols, len(s.cfg.Symbols) == 0 && s.cfg.IncludeOptionGapMappings)
}
func (s *fmpUSStocks) ResolveCursor(ctx context.Context, conn driver.Conn, sourceKey string) (time.Time, bool, error) {
	return queryLatestDate(ctx, conn, "us_stocks_bar_1m", "market_date", "symbol = {symbol:String}", clickhouse.Named("symbol", strings.ToUpper(strings.TrimSpace(sourceKey))))
}
func (s *fmpUSStocks) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpUSStocks) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	targets, err := usmarket.ResolveUSStockSyncTargets(ctx, conn, []string{req.SourceKey}, 0, false)
	if err != nil {
		return syncpipeline.SyncResult{}, err
	}
	res, err := usmarket.SyncFMPStockKlines(ctx, conn, usmarket.FMPStockKlineSyncConfig{APIKey: s.cfg.APIKey, Targets: targets, From: req.From, To: req.To, Interval: s.cfg.Interval, BatchSize: s.cfg.BatchSize, DryRun: req.DryRun, Replace: !req.DryRun})
	return syncResult(req, res.InsertedRows), err
}
func (s *fmpUSStocks) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "us_stocks_bar_1m", DateColumn: "market_date", KeyColumns: []string{"timestamp", "symbol"}, SourceFilter: fmt.Sprintf("symbol = '%s'", escapeStringLiteral(strings.ToUpper(strings.TrimSpace(sourceKey))))}}
}
func (s *fmpUSStocks) MaxConcurrency() int { return 4 }

type FMPUSStockSplitsConfig struct {
	APIKey                   string
	Symbols                  []string
	ResolveAtStartup         bool
	IncludeOptionGapMappings bool
	LimitSymbols             int
	BatchSize                int
	ColdStartFloorUTC        time.Time
}

type fmpUSStockSplits struct{ cfg FMPUSStockSplitsConfig }

func NewFMPUSStockSplits(cfg FMPUSStockSplitsConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_us_stock_splits: APIKey is required")
	}
	cfg.Symbols = normalizeSymbols(cfg.Symbols)
	if len(cfg.Symbols) == 0 && !cfg.ResolveAtStartup {
		return nil, fmt.Errorf("fmp_us_stock_splits: Symbols is empty and ResolveAtStartup is false")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpUSStockSplits{cfg: cfg}, nil
}

func (s *fmpUSStockSplits) Name() string { return "fmp_us_stock_splits" }
func (s *fmpUSStockSplits) SourceKeys(ctx context.Context, conn driver.Conn) ([]string, error) {
	return resolveUSStockSourceKeys(ctx, conn, s.cfg.Symbols, s.cfg.LimitSymbols, len(s.cfg.Symbols) == 0 && s.cfg.IncludeOptionGapMappings)
}
func (s *fmpUSStockSplits) ResolveCursor(ctx context.Context, conn driver.Conn, sourceKey string) (time.Time, bool, error) {
	normalizedSource := strings.ToUpper(strings.TrimSpace(sourceKey))
	return queryLatestDate(ctx, conn, "us_stock_splits", "split_date", "symbol = {symbol:String}", clickhouse.Named("symbol", normalizedSource))
}
func (s *fmpUSStockSplits) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpUSStockSplits) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	targets, err := usmarket.ResolveUSStockSyncTargets(ctx, conn, []string{req.SourceKey}, 0, false)
	if err != nil {
		return syncpipeline.SyncResult{}, err
	}
	res, err := usmarket.SyncFMPStockSplits(ctx, conn, usmarket.FMPStockSplitsSyncConfig{APIKey: s.cfg.APIKey, Targets: targets, BatchSize: s.cfg.BatchSize, DryRun: req.DryRun})
	return syncResult(req, res.RowsInserted), err
}
func (s *fmpUSStockSplits) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "us_stock_splits", DateColumn: "split_date", KeyColumns: []string{"symbol", "split_date"}, SourceFilter: fmt.Sprintf("symbol = '%s'", escapeStringLiteral(strings.ToUpper(strings.TrimSpace(sourceKey))))}}
}
func (s *fmpUSStockSplits) MaxConcurrency() int { return 4 }

type FMPUSStockProfilesConfig struct {
	APIKey                   string
	Symbols                  []string
	ResolveAtStartup         bool
	IncludeOptionGapMappings bool
	LimitSymbols             int
	BatchSize                int
	Workers                  int
	ColdStartFloorUTC        time.Time
}

type fmpUSStockProfiles struct{ cfg FMPUSStockProfilesConfig }

func NewFMPUSStockProfiles(cfg FMPUSStockProfilesConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_us_stock_profiles: APIKey is required")
	}
	cfg.Symbols = normalizeSymbols(cfg.Symbols)
	if len(cfg.Symbols) == 0 && !cfg.ResolveAtStartup {
		return nil, fmt.Errorf("fmp_us_stock_profiles: Symbols is empty and ResolveAtStartup is false")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = usStockProfileBatchSize
	}
	if cfg.BatchSize > usStockProfileBatchSize {
		cfg.BatchSize = usStockProfileBatchSize
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Now().UTC().AddDate(0, 0, -7)
	}
	return &fmpUSStockProfiles{cfg: cfg}, nil
}

func (s *fmpUSStockProfiles) Name() string { return "fmp_us_stock_profiles" }
func (s *fmpUSStockProfiles) SourceKeys(ctx context.Context, conn driver.Conn) ([]string, error) {
	return resolveUSStockSourceKeys(ctx, conn, s.cfg.Symbols, s.cfg.LimitSymbols, len(s.cfg.Symbols) == 0 && s.cfg.IncludeOptionGapMappings)
}
func (s *fmpUSStockProfiles) ResolveCursor(ctx context.Context, conn driver.Conn, sourceKey string) (time.Time, bool, error) {
	return queryLatestDate(ctx, conn, "us_stock_company_profile", "ingested_at", "symbol = {symbol:String}", clickhouse.Named("symbol", strings.ToUpper(strings.TrimSpace(sourceKey))))
}
func (s *fmpUSStockProfiles) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpUSStockProfiles) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.SourceKey))
	if symbol == "" || symbol == syncpipeline.SingletonSourceKey {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To}, nil
	}
	client := fmp.New(s.cfg.APIKey)
	profiles, err := client.Profiles(ctx, []string{symbol})
	if err != nil {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To}, err
	}
	rows, err := insertUSStockCompanyProfiles(ctx, conn, symbol, profiles, req.DryRun)
	return syncResult(req, rows), err
}
func (s *fmpUSStockProfiles) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "us_stock_company_profile", DateColumn: "toDate(ingested_at)", KeyColumns: []string{"symbol", "source"}, SourceFilter: fmt.Sprintf("symbol = '%s'", escapeStringLiteral(strings.ToUpper(strings.TrimSpace(sourceKey))))}}
}
func (s *fmpUSStockProfiles) MaxConcurrency() int { return s.cfg.Workers }

const usStockProfileBatchSize = 25

func insertUSStockCompanyProfiles(ctx context.Context, conn driver.Conn, fallbackSymbol string, profiles []fmp.Profile, dryRun bool) (int64, error) {
	if len(profiles) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	rows := make([]dtoProfileInsertRow, 0, len(profiles))
	for _, raw := range profiles {
		symbol := strings.ToUpper(strings.TrimSpace(raw.Symbol))
		if symbol == "" {
			symbol = strings.ToUpper(strings.TrimSpace(fallbackSymbol))
		}
		profile := service.ClassifyFMPUSStockCompanyProfile(symbol, raw)
		if profile == nil {
			continue
		}
		rows = append(rows, dtoProfileInsertRow{Profile: profile, Raw: raw, SourceHash: hashFMPProfile(raw), Timestamp: now})
	}
	if dryRun || len(rows) == 0 {
		return int64(len(rows)), nil
	}
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO us_stock_company_profile (
    symbol,
    ticker,
    name,
    country,
    currency,
    exchange,
    exchange_full_name,
    sector,
    industry,
    ipo,
    market_capitalization,
    share_outstanding,
    weburl,
    logo,
    source,
    source_hash,
    is_etf,
    is_fund,
    known_at,
    updated_at,
    ingested_at
)`)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		profile := row.Profile
		if err := batch.Append(
			profile.Symbol,
			profile.Ticker,
			profile.Name,
			profile.Country,
			profile.Currency,
			profile.Exchange,
			profile.ExchangeFullName,
			profile.Sector,
			profile.Industry,
			profile.IPO,
			profile.MarketCapitalization,
			profile.ShareOutstanding,
			profile.WebURL,
			profile.Logo,
			profile.Source,
			row.SourceHash,
			boolToUInt8(profile.IsETF),
			boolToUInt8(profile.IsFund),
			row.Timestamp,
			row.Timestamp,
			row.Timestamp,
		); err != nil {
			return 0, err
		}
	}
	if err := batch.Send(); err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

type dtoProfileInsertRow struct {
	Profile    *dto.USStockCompanyProfile
	Raw        fmp.Profile
	SourceHash string
	Timestamp  time.Time
}

func hashFMPProfile(profile fmp.Profile) string {
	payload, _ := json.Marshal(profile)
	sum := sha1.Sum(payload)
	return hex.EncodeToString(sum[:])
}

func boolToUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

type FMPUSFundamentalsConfig struct {
	Provider           usmarket.PEBackfillProvider
	DSN                string
	Symbols            []string
	IncrementalMode    string
	DiscoveryPageSize  int
	DiscoveryPageLimit int
	Workers            int
	BatchSize          int
	PageSize           int
	QPS                int
	LimitSymbols       int
	DistributedLimiter usmarket.DistributedRateLimitConfig
	ColdStartFloorUTC  time.Time
}

type fmpUSFundamentals struct{ cfg FMPUSFundamentalsConfig }

func NewFMPUSFundamentals(cfg FMPUSFundamentalsConfig) (syncpipeline.Syncer, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("fmp_us_fundamentals: Provider is required")
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("fmp_us_fundamentals: DSN is required")
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpUSFundamentals{cfg: cfg}, nil
}

func (s *fmpUSFundamentals) Name() string { return "fmp_us_fundamentals" }
func (s *fmpUSFundamentals) SourceKeys(ctx context.Context, conn driver.Conn) ([]string, error) {
	if len(s.cfg.Symbols) > 0 {
		return usmarket.ResolveUSStockSymbols(ctx, conn, s.cfg.Symbols, s.cfg.LimitSymbols)
	}
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *fmpUSFundamentals) ResolveCursor(ctx context.Context, conn driver.Conn, sourceKey string) (time.Time, bool, error) {
	if sourceKey != syncpipeline.SingletonSourceKey {
		return queryLatestDate(ctx, conn, "fundamental_observation", "event_ts", "market = {market:String} AND symbol = {symbol:String} AND factor_code IN ('pe','pb')", clickhouse.Named("market", "us-stocks"), clickhouse.Named("symbol", sourceKey))
	}
	return queryLatestDate(ctx, conn, "fundamental_observation", "event_ts", "market = {market:String} AND factor_code IN ('pe','pb')", clickhouse.Named("market", "us-stocks"))
}
func (s *fmpUSFundamentals) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpUSFundamentals) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	symbols := s.cfg.Symbols
	incrementalMode := s.cfg.IncrementalMode
	if req.SourceKey != syncpipeline.SingletonSourceKey {
		symbols = []string{req.SourceKey}
		incrementalMode = ""
	}
	res, err := usmarket.BackfillUSStockPE(ctx, usmarket.USFundamentalsBackfillConfig{Conn: conn, DSN: s.cfg.DSN, Provider: s.cfg.Provider, StartDate: req.From, EndDate: req.To, Symbols: symbols, IncrementalMode: incrementalMode, DiscoveryPageSize: s.cfg.DiscoveryPageSize, DiscoveryPageLimit: s.cfg.DiscoveryPageLimit, Workers: s.cfg.Workers, BatchSize: s.cfg.BatchSize, PageSize: s.cfg.PageSize, QPS: s.cfg.QPS, LimitSymbols: s.cfg.LimitSymbols, DryRun: req.DryRun, DistributedLimiter: s.cfg.DistributedLimiter})
	return syncResult(req, res.InsertedRows), err
}
func (s *fmpUSFundamentals) AuditTargets(string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "fundamental_observation", DateColumn: "toDate(event_ts)", KeyColumns: []string{"market", "symbol", "factor_code", "event_ts", "known_at", "source", "revision"}, SourceFilter: "market = 'us-stocks' AND factor_code IN ('pe','pb')"}}
}
func (s *fmpUSFundamentals) MaxConcurrency() int { return 1 }

type FMPETFFundamentalsConfig struct {
	APIKey             string
	DSN                string
	Symbols            []string
	SymbolMappings     map[string]string
	BatchSize          int
	QPS                int
	MinCoverage        float64
	DistributedLimiter usmarket.DistributedRateLimitConfig
	ColdStartFloorUTC  time.Time
}

type fmpETFFundamentals struct{ cfg FMPETFFundamentalsConfig }

func NewFMPETFFundamentals(cfg FMPETFFundamentalsConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_etf_fundamentals: APIKey is required")
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("fmp_etf_fundamentals: DSN is required")
	}
	cfg.Symbols = normalizeSymbols(cfg.Symbols)
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpETFFundamentals{cfg: cfg}, nil
}

func (s *fmpETFFundamentals) Name() string { return "fmp_etf_fundamentals" }
func (s *fmpETFFundamentals) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *fmpETFFundamentals) ResolveCursor(ctx context.Context, conn driver.Conn, _ string) (time.Time, bool, error) {
	return queryLatestDate(ctx, conn, "fundamental_observation", "event_ts", "market = {market:String} AND source = {source:String}", clickhouse.Named("market", "us-stocks"), clickhouse.Named("source", "fmp_etf_fundamentals"))
}
func (s *fmpETFFundamentals) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpETFFundamentals) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	targets, err := usmarket.ResolveUSStockSyncTargetsWithOptions(ctx, conn, s.cfg.Symbols, 0, usmarket.USStockSyncTargetResolverOptions{
		Provider:       usmarket.USStockSyncTargetProviderFMP,
		FetchOverrides: s.cfg.SymbolMappings,
	})
	if err != nil {
		return syncpipeline.SyncResult{}, err
	}
	res, err := usmarket.BackfillUSStockPE(ctx, usmarket.USFundamentalsBackfillConfig{Conn: conn, DSN: s.cfg.DSN, Provider: usmarket.NewFMPPEBackfillProvider(s.cfg.APIKey, 40), StartDate: req.From, EndDate: req.To, Targets: targets, Workers: 1, BatchSize: s.cfg.BatchSize, PageSize: 251, QPS: s.cfg.QPS, DryRun: req.DryRun, DistributedLimiter: s.cfg.DistributedLimiter})
	return syncResult(req, res.InsertedRows), err
}
func (s *fmpETFFundamentals) AuditTargets(string) []syncpipeline.AuditTarget {
	return (&fmpUSFundamentals{}).AuditTargets("")
}
func (s *fmpETFFundamentals) MaxConcurrency() int { return 1 }

type PolygonUSFlatFilesConfig struct {
	Downloader        usmarket.FlatFileDownloader
	Sessions          usmarket.SessionMap
	DSN               string
	BatchSize         int
	Workers           int
	RiskFreeRate      float64
	ForceDownload     bool
	SyncStocks        bool
	SyncOptions       bool
	SourceInterval    string // "1m" (default, minute aggregates) or "1d" (day aggregates)
	ColdStartFloorUTC time.Time
}

type polygonUSFlatFiles struct{ cfg PolygonUSFlatFilesConfig }

func NewPolygonUSFlatFiles(cfg PolygonUSFlatFilesConfig) (syncpipeline.Syncer, error) {
	if cfg.Downloader == nil {
		return nil, fmt.Errorf("polygon_us_flatfiles: Downloader is required")
	}
	if cfg.Sessions == nil {
		return nil, fmt.Errorf("polygon_us_flatfiles: Sessions is required")
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("polygon_us_flatfiles: DSN is required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &polygonUSFlatFiles{cfg: cfg}, nil
}

func (s *polygonUSFlatFiles) Name() string { return "polygon_us_flatfiles" }
func (s *polygonUSFlatFiles) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *polygonUSFlatFiles) ResolveCursor(ctx context.Context, conn driver.Conn, _ string) (time.Time, bool, error) {
	if s.cfg.SourceInterval == "1d" {
		options, ok, err := usmarket.LatestOptionDailyMarketDate(ctx, conn)
		if err != nil || !ok {
			return options, ok, err
		}
		if s.cfg.SyncStocks {
			stocks, sOk, err := usmarket.LatestStockDailyMarketDate(ctx, conn)
			if err != nil || !sOk {
				return stocks, sOk, err
			}
			if stocks.Before(options) {
				return stocks.UTC(), true, nil
			}
		}
		return options.UTC(), true, nil
	}
	options, ok, err := usmarket.LatestOptionMarketDate(ctx, conn)
	if err != nil || !ok {
		return options, ok, err
	}
	if s.cfg.SyncStocks {
		stocks, sOk, err := usmarket.LatestStockMarketDate(ctx, conn)
		if err != nil || !sOk {
			return stocks, sOk, err
		}
		if stocks.Before(options) {
			return stocks.UTC(), true, nil
		}
	}
	return options.UTC(), true, nil
}
func (s *polygonUSFlatFiles) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *polygonUSFlatFiles) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	res, err := usmarket.SyncPolygonFlatFiles(ctx, usmarket.FlatFileSyncConfig{
		Downloader:     s.cfg.Downloader,
		Conn:           conn,
		Sessions:       s.cfg.Sessions,
		ForceDownload:  s.cfg.ForceDownload,
		SkipStocks:     !s.cfg.SyncStocks,
		SkipOptions:    !s.cfg.SyncOptions,
		DryRun:         req.DryRun,
		ColdStartDate:  s.cfg.ColdStartFloorUTC,
		StartDate:      req.From,
		EndDate:        req.To,
		SourceInterval: s.cfg.SourceInterval,
		Import: usmarket.ImportConfig{
			DSN:          s.cfg.DSN,
			BatchSize:    s.cfg.BatchSize,
			Workers:      s.cfg.Workers,
			ReplaceDates: !req.DryRun,
			SkipExisting: req.DryRun,
			RiskFreeRate: s.cfg.RiskFreeRate,
		},
	})
	out := syncResult(req, res.Import.OptionRows+res.Import.StockRows)
	out.Notes = usmarket.FormatFlatFileSyncSummary(res)
	return out, err
}
func (s *polygonUSFlatFiles) AuditTargets(string) []syncpipeline.AuditTarget {
	if s.cfg.SourceInterval == "1d" {
		targets := []syncpipeline.AuditTarget{{Table: "us_options_bar_1d_direct", DateColumn: "market_date", KeyColumns: []string{"market_date", "underlying", "expiration", "strike", "option_type", "timestamp"}}}
		if s.cfg.SyncStocks {
			targets = append(targets, syncpipeline.AuditTarget{Table: "us_stocks_bar_1d_direct", DateColumn: "market_date", KeyColumns: []string{"timestamp", "symbol"}})
		}
		return targets
	}
	targets := []syncpipeline.AuditTarget{{Table: "us_options_bar_1m", DateColumn: "market_date", KeyColumns: []string{"market_date", "underlying", "expiration", "strike", "option_type", "timestamp"}}}
	if s.cfg.SyncStocks {
		targets = append(targets, syncpipeline.AuditTarget{Table: "us_stocks_bar_1m", DateColumn: "market_date", KeyColumns: []string{"timestamp", "symbol"}})
	}
	return targets
}
func (s *polygonUSFlatFiles) MaxConcurrency() int { return 1 }

type PolygonUSGreeksConfig struct {
	DSN               string
	BatchSize         int
	Workers           int
	RiskFreeRate      float64
	Underlyings       []string
	LimitTasks        int
	RebuildAggregates bool
	ColdStartFloorUTC time.Time
}

type polygonUSGreeks struct{ cfg PolygonUSGreeksConfig }

func NewPolygonUSGreeks(cfg PolygonUSGreeksConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("polygon_us_greeks: DSN is required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &polygonUSGreeks{cfg: cfg}, nil
}

func (s *polygonUSGreeks) Name() string { return "polygon_us_greeks" }
func (s *polygonUSGreeks) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *polygonUSGreeks) ResolveCursor(ctx context.Context, conn driver.Conn, _ string) (time.Time, bool, error) {
	return usmarket.LatestOptionMarketDate(ctx, conn)
}
func (s *polygonUSGreeks) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *polygonUSGreeks) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	res, err := usmarket.BackfillMissingOptionGreeks(ctx, usmarket.OptionGreeksBackfillConfig{Conn: conn, DSN: s.cfg.DSN, StartDate: req.From, EndDate: req.To, Underlyings: s.cfg.Underlyings, PriorityOrder: usmarket.PriorityOrderUSDefault, Workers: s.cfg.Workers, BatchSize: s.cfg.BatchSize, LimitTasks: s.cfg.LimitTasks, RiskFreeRate: s.cfg.RiskFreeRate, DryRun: req.DryRun, RebuildAggregates: s.cfg.RebuildAggregates && !req.DryRun})
	return syncResult(req, res.BackfilledRows), err
}
func (s *polygonUSGreeks) AuditTargets(string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "us_options_bar_1m", DateColumn: "market_date", KeyColumns: []string{"market_date", "underlying", "expiration", "strike", "option_type", "timestamp"}, SourceFilter: "(isNaN(delta) OR isNaN(gamma) OR isNaN(theta) OR isNaN(vega) OR isNaN(rho))"}}
}
func (s *polygonUSGreeks) MaxConcurrency() int { return 1 }

type FeatureStoreBackfillConfig struct {
	DSN               string
	Markets           []string
	Underlyings       []string
	PriorityOrder     string
	LookbackDays      int
	MinDaysToExpiry   int
	MaxDaysToExpiry   int
	Workers           int
	Replace           bool
	ColdStartFloorUTC time.Time
}

type featureStoreBackfill struct{ cfg FeatureStoreBackfillConfig }

type featureCursorScope struct {
	table           string
	market          string
	includeLookback bool
	includeDTE      bool
}

func NewFeatureStoreBackfill(cfg FeatureStoreBackfillConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("feature_store_backfill: DSN is required")
	}
	if len(cfg.Markets) == 0 {
		cfg.Markets = []string{"us-options"}
	}
	if cfg.LookbackDays <= 0 {
		cfg.LookbackDays = 252
	}
	if cfg.MaxDaysToExpiry <= 0 {
		cfg.MaxDaysToExpiry = 365
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	priorityOrder, err := usmarket.NormalizeUSPriorityOrder(cfg.PriorityOrder)
	if err != nil {
		return nil, fmt.Errorf("feature_store_backfill: %w", err)
	}
	cfg.PriorityOrder = priorityOrder
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &featureStoreBackfill{cfg: cfg}, nil
}

func (s *featureStoreBackfill) Name() string { return "feature_store_backfill" }
func (s *featureStoreBackfill) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *featureStoreBackfill) ResolveCursor(ctx context.Context, conn driver.Conn, _ string) (time.Time, bool, error) {
	var earliest time.Time
	hasCursor := false
	for _, scope := range s.cursorScopes() {
		latest, ok, err := s.queryCursorScopeLatestDate(ctx, conn, scope)
		if err != nil {
			return time.Time{}, false, err
		}
		if !ok {
			return time.Time{}, false, nil
		}
		if !hasCursor || latest.Before(earliest) {
			earliest = latest.UTC()
			hasCursor = true
		}
	}
	if !hasCursor {
		return time.Time{}, false, nil
	}
	return earliest.UTC(), true, nil
}
func (s *featureStoreBackfill) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *featureStoreBackfill) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	featureSvc := service.NewFeatureService(chrepo.NewRepo(conn))
	stats, err := featureSvc.BackfillFeatureSnapshots(ctx, service.FeatureBackfillOptions{
		Markets:         s.cfg.Markets,
		Underlyings:     s.cfg.Underlyings,
		PriorityOrder:   s.cfg.PriorityOrder,
		ClickHouseDSN:   s.cfg.DSN,
		From:            req.From,
		To:              req.To.AddDate(0, 0, 1),
		LookbackDays:    s.cfg.LookbackDays,
		MinDaysToExpiry: s.cfg.MinDaysToExpiry,
		MaxDaysToExpiry: s.cfg.MaxDaysToExpiry,
		Workers:         s.cfg.Workers,
		Replace:         s.cfg.Replace && !req.DryRun,
		ContinueOnError: true,
	})
	result := syncResult(req, int64(stats.RowsWritten))
	return result, err
}
func (s *featureStoreBackfill) AuditTargets(string) []syncpipeline.AuditTarget { return nil }
func (s *featureStoreBackfill) MaxConcurrency() int                            { return 1 }

func (s *featureStoreBackfill) cursorScopes() []featureCursorScope {
	markets := normalizeFeatureCursorMarkets(s.cfg.Markets)
	scopes := make([]featureCursorScope, 0, len(markets)*5)
	for _, market := range markets {
		scopes = append(scopes,
			featureCursorScope{table: "feature_volatility_snapshot_daily", market: market, includeLookback: true},
			featureCursorScope{table: "feature_liquidity_snapshot_daily", market: market},
			featureCursorScope{table: "feature_daily_panel_daily", market: market, includeLookback: true, includeDTE: true},
		)
		if market == "us-options" {
			scopes = append(scopes,
				featureCursorScope{table: "feature_term_structure_snapshot_daily", market: market},
				featureCursorScope{table: "feature_skew_snapshot_daily", market: market},
			)
		}
	}
	return scopes
}

func (s *featureStoreBackfill) queryCursorScopeLatestDate(ctx context.Context, conn driver.Conn, scope featureCursorScope) (time.Time, bool, error) {
	where := "market = {market:String}"
	args := []any{clickhouse.Named("market", scope.market)}
	if len(s.cfg.Underlyings) > 0 {
		where += " AND underlying IN ({underlyings:Array(String)})"
		args = append(args, clickhouse.Named("underlyings", normalizeSymbols(s.cfg.Underlyings)))
	}
	if scope.includeLookback {
		where += " AND lookback_days = {lookback_days:UInt16}"
		args = append(args, clickhouse.Named("lookback_days", uint16(s.cfg.LookbackDays)))
	}
	if scope.includeDTE {
		where += " AND min_days_to_expiry = {min_dte:Int32} AND max_days_to_expiry = {max_dte:Int32}"
		args = append(args,
			clickhouse.Named("min_dte", int32(s.cfg.MinDaysToExpiry)),
			clickhouse.Named("max_dte", int32(s.cfg.MaxDaysToExpiry)),
		)
	}
	return queryLatestDate(ctx, conn, scope.table, "as_of_date", where, args...)
}

func normalizeFeatureCursorMarkets(markets []string) []string {
	if len(markets) == 0 {
		return []string{"us-options"}
	}
	seen := make(map[string]struct{}, len(markets))
	out := make([]string, 0, len(markets))
	for _, market := range markets {
		normalized := strings.ToLower(strings.TrimSpace(market))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return []string{"us-options"}
	}
	return out
}

type MacroSyncFunc func(context.Context, driver.Conn, time.Time, time.Time, bool) (int64, error)

type GuruMacroConfig struct {
	Dataset           string
	Source            string
	ColdStartFloorUTC time.Time
	SyncFunc          MacroSyncFunc
}

type guruMacro struct{ cfg GuruMacroConfig }

func NewGuruMacro(cfg GuruMacroConfig) (syncpipeline.Syncer, error) {
	if cfg.SyncFunc == nil {
		return nil, fmt.Errorf("guru_macro: SyncFunc is required")
	}
	if strings.TrimSpace(cfg.Dataset) == "" {
		cfg.Dataset = "gurufocus-shiller"
	}
	if strings.TrimSpace(cfg.Source) == "" {
		cfg.Source = "gurufocus"
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(1880, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &guruMacro{cfg: cfg}, nil
}

func (s *guruMacro) Name() string { return "guru_macro" }
func (s *guruMacro) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *guruMacro) ResolveCursor(ctx context.Context, conn driver.Conn, _ string) (time.Time, bool, error) {
	return queryLatestDate(ctx, conn, "macro_observation", "event_ts", "dataset = {dataset:String} AND source = {source:String}", clickhouse.Named("dataset", s.cfg.Dataset), clickhouse.Named("source", s.cfg.Source))
}
func (s *guruMacro) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *guruMacro) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	rows, err := s.cfg.SyncFunc(ctx, conn, req.From, req.To, req.DryRun)
	return syncResult(req, rows), err
}
func (s *guruMacro) AuditTargets(string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "macro_observation", DateColumn: "toDate(event_ts)", KeyColumns: []string{"dataset", "factor_code", "event_ts", "known_at", "source", "revision"}, SourceFilter: fmt.Sprintf("dataset = '%s' AND source = '%s'", escapeStringLiteral(s.cfg.Dataset), escapeStringLiteral(s.cfg.Source))}}
}
func (s *guruMacro) MaxConcurrency() int { return 1 }

type DeribitDVOLMacroConfig struct {
	Symbols           []string
	BatchSize         int
	ColdStartFloorUTC time.Time
}

type deribitDVOLMacro struct{ cfg DeribitDVOLMacroConfig }

func NewDeribitDVOLMacro(cfg DeribitDVOLMacroConfig) (syncpipeline.Syncer, error) {
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &deribitDVOLMacro{cfg: cfg}, nil
}

func (s *deribitDVOLMacro) Name() string { return "deribit_dvol_macro" }
func (s *deribitDVOLMacro) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *deribitDVOLMacro) ResolveCursor(ctx context.Context, conn driver.Conn, _ string) (time.Time, bool, error) {
	return queryLatestDate(ctx, conn, "macro_observation", "event_ts", "source = {source:String} AND dataset IN {datasets:Array(String)}",
		clickhouse.Named("source", macro.DefaultDeribitDVOLSource),
		clickhouse.Named("datasets", []string{macro.DefaultDeribitDVOLBTCDataset, macro.DefaultDeribitDVOLETHDataset}),
	)
}
func (s *deribitDVOLMacro) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *deribitDVOLMacro) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	res, err := macro.SyncDeribitDVOLFromDeribit(ctx, conn, macro.DeribitDVOLConfig{Symbols: s.cfg.Symbols, BatchSize: s.cfg.BatchSize}, req.From, req.To, req.DryRun)
	return syncResult(req, int64(res.ObservationRows)), err
}
func (s *deribitDVOLMacro) AuditTargets(string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{Table: "macro_observation", DateColumn: "toDate(event_ts)", KeyColumns: []string{"dataset", "factor_code", "event_ts", "known_at", "source", "revision"}, SourceFilter: fmt.Sprintf("source = '%s' AND dataset IN ('%s','%s')", macro.DefaultDeribitDVOLSource, macro.DefaultDeribitDVOLBTCDataset, macro.DefaultDeribitDVOLETHDataset)}}
}
func (s *deribitDVOLMacro) MaxConcurrency() int { return 1 }

type FMPEconomicCalendarConfig struct {
	APIKey            string
	FMPCacheDir       string
	MySQLDSN          string
	Cache             cache.Store
	ColdStartFloorUTC time.Time
}

type fmpEconomicCalendar struct{ cfg FMPEconomicCalendarConfig }

func NewFMPEconomicCalendar(cfg FMPEconomicCalendarConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_economic_calendar: APIKey is required")
	}
	if strings.TrimSpace(cfg.MySQLDSN) == "" {
		return nil, fmt.Errorf("fmp_economic_calendar: MySQLDSN is required")
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Now().UTC().Add(-24 * time.Hour)
	}
	return &fmpEconomicCalendar{cfg: cfg}, nil
}

func (s *fmpEconomicCalendar) Name() string { return "fmp_economic_calendar" }
func (s *fmpEconomicCalendar) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *fmpEconomicCalendar) ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (s *fmpEconomicCalendar) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpEconomicCalendar) Sync(ctx context.Context, _ driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	calendarSvc, closeFn, err := openFinanceCalendarService(s.cfg)
	if err != nil {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To}, err
	}
	defer closeFn()
	rows, err := calendarSvc.SyncEconomicCalendar(ctx)
	return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To, RowsInserted: int64(rows), Notes: []string{"calendar sync window is determined by finance calendar service markers/cache; runner from/to is informational"}}, err
}
func (s *fmpEconomicCalendar) AuditTargets(string) []syncpipeline.AuditTarget { return nil }
func (s *fmpEconomicCalendar) MaxConcurrency() int                            { return 1 }

type FMPObservedStockCalendarConfig struct {
	APIKey            string
	FMPCacheDir       string
	MySQLDSN          string
	Cache             cache.Store
	ColdStartFloorUTC time.Time
}

type fmpObservedStockCalendar struct {
	cfg FMPObservedStockCalendarConfig
}

func NewFMPObservedStockCalendar(cfg FMPObservedStockCalendarConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_observed_stock_calendar: APIKey is required")
	}
	if strings.TrimSpace(cfg.MySQLDSN) == "" {
		return nil, fmt.Errorf("fmp_observed_stock_calendar: MySQLDSN is required")
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Now().UTC().Add(-24 * time.Hour)
	}
	return &fmpObservedStockCalendar{cfg: cfg}, nil
}

func (s *fmpObservedStockCalendar) Name() string { return "fmp_observed_stock_calendar" }
func (s *fmpObservedStockCalendar) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{syncpipeline.SingletonSourceKey}, nil
}
func (s *fmpObservedStockCalendar) ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (s *fmpObservedStockCalendar) ColdStartFloor(string) time.Time { return s.cfg.ColdStartFloorUTC }
func (s *fmpObservedStockCalendar) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	calendarSvc, closeFn, err := openFinanceCalendarService(FMPEconomicCalendarConfig{
		APIKey:      s.cfg.APIKey,
		FMPCacheDir: s.cfg.FMPCacheDir,
		MySQLDSN:    s.cfg.MySQLDSN,
		Cache:       s.cfg.Cache,
	})
	if err != nil {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To}, err
	}
	defer closeFn()
	companyProfileProvider := service.NewClickHouseUSStockCompanyProfileProvider(chrepo.NewRepo(conn))
	screener := service.NewScreenerService(chrepo.NewRepo(conn), s.cfg.Cache).WithCompanyProfileProvider(companyProfileProvider)
	symbols, err := service.ResolveObservedUSStockPool(ctx, screener)
	if err != nil {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To}, err
	}
	rows, err := calendarSvc.SyncStockCalendar(ctx, symbols)
	return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To, RowsInserted: int64(rows), Notes: []string{fmt.Sprintf("observed_symbols=%d", len(symbols)), "calendar sync window is determined by finance calendar service markers/cache; runner from/to is informational"}}, err
}
func (s *fmpObservedStockCalendar) AuditTargets(string) []syncpipeline.AuditTarget { return nil }
func (s *fmpObservedStockCalendar) MaxConcurrency() int                            { return 1 }

func openFinanceCalendarService(cfg FMPEconomicCalendarConfig) (*service.FinanceCalendarService, func(), error) {
	options := []fmp.Option{}
	if strings.TrimSpace(cfg.FMPCacheDir) != "" {
		options = append(options, fmp.WithCacheDir(strings.TrimSpace(cfg.FMPCacheDir)))
	}
	gormDB, err := gorm.Open(mysql.Open(cfg.MySQLDSN), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("connect mysql: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("mysql sql db: %w", err)
	}
	repo := calendarrepo.New(gormDB)
	if err := repo.AutoMigrate(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("migrate finance calendar tables: %w", err)
	}
	closeFn := func() {
		_ = sqlDB.Close()
	}
	client := fmp.New(cfg.APIKey, options...)
	return service.NewFinanceCalendarService(repo, client, cfg.Cache), closeFn, nil
}

func queryLatestDate(ctx context.Context, conn driver.Conn, table, dateExpr, where string, args ...any) (time.Time, bool, error) {
	query := fmt.Sprintf("SELECT toString(ifNull(maxOrNull(toDate(%s)), toDate('1970-01-01'))) FROM %s", dateExpr, table)
	if strings.TrimSpace(where) != "" {
		query += " WHERE " + where
	}
	var latest string
	if err := conn.QueryRow(ctx, query, args...).Scan(&latest); err != nil {
		return time.Time{}, false, err
	}
	if latest == "" || latest == "1970-01-01" {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse("2006-01-02", latest)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed.UTC(), true, nil
}

func queryDistinctSymbols(ctx context.Context, conn driver.Conn, table, column string, limit int, suffix string) ([]string, error) {
	query := fmt.Sprintf("SELECT %s FROM %s GROUP BY %s ORDER BY %s", column, table, column, column)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, err
		}
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if suffix != "" && !strings.HasSuffix(symbol, suffix) {
			symbol += suffix
		}
		out = append(out, symbol)
	}
	return out, rows.Err()
}

func normalizeSymbols(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	seen := map[string]struct{}{}
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

func resolveUSStockSourceKeys(ctx context.Context, conn driver.Conn, symbols []string, limit int, includeOptionGapMappings bool) ([]string, error) {
	targets, err := usmarket.ResolveUSStockSyncTargets(ctx, conn, symbols, limit, includeOptionGapMappings)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(targets))
	seen := map[string]struct{}{}
	for _, target := range targets {
		symbol := strings.ToUpper(strings.TrimSpace(target.StoreSymbol))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out, nil
}

func syncResult(req syncpipeline.SyncRequest, rows int64) syncpipeline.SyncResult {
	return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To, RowsInserted: rows}
}

func escapeStringLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

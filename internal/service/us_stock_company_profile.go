package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	usStockCompanyProfileTTLMin           = 7 * 24 * time.Hour
	usStockCompanyProfileTTLMax           = 14 * 24 * time.Hour
	usStockCompanyProfileBatchSize        = 25
	usStockCompanyProfileBatchConcurrency = 4
)

type usStockCompanyProfileProvider interface {
	CompanyProfile(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, error)
	CompanyProfiles(ctx context.Context, symbols []string) (map[string]*dto.USStockCompanyProfile, error)
	IsETFLike(ctx context.Context, symbol string) (bool, error)
	IsETFLikeBySymbol(ctx context.Context, symbols []string) (map[string]bool, error)
}

type fmpCompanyProfiler interface {
	Profile(ctx context.Context, symbol string) (*fmp.Profile, error)
	Profiles(ctx context.Context, symbols []string) ([]fmp.Profile, error)
}

type cachedFMPUSStockCompanyProfileProvider struct {
	client   fmpCompanyProfiler
	cache    cache.Store
	ttlValue func() time.Duration
}

type cachedUSStockCompanyProfileRecord struct {
	Profile *dto.USStockCompanyProfile `json:"profile,omitempty"`
}

type clickHouseUSStockCompanyProfileProvider struct {
	repo *chrepo.Repo
}

func NewClickHouseUSStockCompanyProfileProvider(repo *chrepo.Repo) usStockCompanyProfileProvider {
	if repo == nil || repo.Conn == nil {
		return nil
	}
	return &clickHouseUSStockCompanyProfileProvider{repo: repo}
}

func (p *clickHouseUSStockCompanyProfileProvider) CompanyProfile(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
	profiles, err := p.CompanyProfiles(ctx, []string{symbol})
	if err != nil {
		return nil, err
	}
	return profiles[normalizeUSStockCompanyProfileSymbol(symbol)], nil
}

func (p *clickHouseUSStockCompanyProfileProvider) CompanyProfiles(ctx context.Context, symbols []string) (map[string]*dto.USStockCompanyProfile, error) {
	normalized := normalizeUSStockCompanyProfileSymbols(symbols)
	profiles := make(map[string]*dto.USStockCompanyProfile, len(normalized))
	if len(normalized) == 0 {
		return profiles, nil
	}
	query := `
SELECT
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
    is_etf,
    is_fund
FROM us_stock_company_profile FINAL
WHERE symbol IN {symbols:Array(String)}
ORDER BY symbol`
	rows, err := p.repo.Conn.Query(ctx, query, clickhouse.Named("symbols", normalized))
	if err != nil {
		return nil, fmt.Errorf("query US stock company profiles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var profile dto.USStockCompanyProfile
		var marketCapitalization *float64
		var shareOutstanding *float64
		var isETF uint8
		var isFund uint8
		if err := rows.Scan(
			&profile.Symbol,
			&profile.Ticker,
			&profile.Name,
			&profile.Country,
			&profile.Currency,
			&profile.Exchange,
			&profile.ExchangeFullName,
			&profile.Sector,
			&profile.Industry,
			&profile.IPO,
			&marketCapitalization,
			&shareOutstanding,
			&profile.WebURL,
			&profile.Logo,
			&profile.Source,
			&isETF,
			&isFund,
		); err != nil {
			return nil, err
		}
		profile.Symbol = normalizeUSStockCompanyProfileSymbol(profile.Symbol)
		if profile.Ticker == "" {
			profile.Ticker = profile.Symbol
		}
		profile.MarketCapitalization = marketCapitalization
		profile.ShareOutstanding = shareOutstanding
		profile.IsETF = isETF != 0
		profile.IsFund = isFund != 0
		profiles[profile.Symbol] = &profile
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}

func (p *clickHouseUSStockCompanyProfileProvider) IsETFLike(ctx context.Context, symbol string) (bool, error) {
	profile, err := p.CompanyProfile(ctx, symbol)
	if err != nil {
		return false, err
	}
	return isETFLikeUSStockProfile(profile), nil
}

func (p *clickHouseUSStockCompanyProfileProvider) IsETFLikeBySymbol(ctx context.Context, symbols []string) (map[string]bool, error) {
	profiles, err := p.CompanyProfiles(ctx, symbols)
	if err != nil {
		return nil, err
	}
	normalized := normalizeUSStockCompanyProfileSymbols(symbols)
	result := make(map[string]bool, len(normalized))
	for _, symbol := range normalized {
		result[symbol] = isETFLikeUSStockProfile(profiles[symbol])
	}
	return result, nil
}

func NewCachedFMPUSStockCompanyProfileProvider(apiKey string, cacheStore cache.Store) usStockCompanyProfileProvider {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	return &cachedFMPUSStockCompanyProfileProvider{
		client:   fmp.New(apiKey),
		cache:    cacheStore,
		ttlValue: randomUSStockCompanyProfileTTL,
	}
}

func (p *cachedFMPUSStockCompanyProfileProvider) CompanyProfile(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, error) {
	normalized := normalizeUSStockCompanyProfileSymbol(symbol)
	if normalized == "" {
		return nil, nil
	}
	if cached, ok, err := p.loadFromCache(ctx, normalized); err == nil && ok {
		return cached, nil
	}
	profile, err := p.client.Profile(ctx, normalized)
	if err != nil {
		return nil, err
	}
	classification := classifyUSStockCompanyProfile(normalized, *profile)
	_ = p.storeInCache(ctx, normalized, classification)
	return classification, nil
}

func (p *cachedFMPUSStockCompanyProfileProvider) IsETFLike(ctx context.Context, symbol string) (bool, error) {
	profile, err := p.CompanyProfile(ctx, symbol)
	if err != nil {
		return false, err
	}
	return isETFLikeUSStockProfile(profile), nil
}

func (p *cachedFMPUSStockCompanyProfileProvider) CompanyProfiles(ctx context.Context, symbols []string) (map[string]*dto.USStockCompanyProfile, error) {
	return p.companyProfilesBySymbol(ctx, symbols)
}

func (p *cachedFMPUSStockCompanyProfileProvider) IsETFLikeBySymbol(ctx context.Context, symbols []string) (map[string]bool, error) {
	profiles, err := p.companyProfilesBySymbol(ctx, symbols)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(profiles))
	for symbol, profile := range profiles {
		result[symbol] = isETFLikeUSStockProfile(profile)
	}
	return result, nil
}

// isETFLikeUSStockProfile centralizes the ETF/fund classification rule so
// other US-stock features can reuse a single definition.
func isETFLikeUSStockProfile(profile *dto.USStockCompanyProfile) bool {
	return profile != nil && (profile.IsETF || profile.IsFund)
}

func (p *cachedFMPUSStockCompanyProfileProvider) companyProfilesBySymbol(ctx context.Context, symbols []string) (map[string]*dto.USStockCompanyProfile, error) {
	normalizedSymbols := normalizeUSStockCompanyProfileSymbols(symbols)
	profiles := make(map[string]*dto.USStockCompanyProfile, len(normalizedSymbols))
	missing := make([]string, 0, len(normalizedSymbols))
	for _, symbol := range normalizedSymbols {
		if cached, ok, err := p.loadFromCache(ctx, symbol); err == nil && ok {
			profiles[symbol] = cached
			continue
		} else if err != nil {
			return nil, err
		}
		missing = append(missing, symbol)
	}
	if len(missing) == 0 {
		return profiles, nil
	}

	upstreamProfiles, err := p.fetchProfilesInBatches(ctx, missing)
	if err != nil {
		return nil, err
	}
	upstreamBySymbol := make(map[string]fmp.Profile, len(upstreamProfiles))
	for _, profile := range upstreamProfiles {
		normalizedSymbol := normalizeUSStockCompanyProfileSymbol(profile.Symbol)
		if normalizedSymbol == "" {
			continue
		}
		upstreamBySymbol[normalizedSymbol] = profile
	}
	for _, symbol := range missing {
		classification := classifyUSStockCompanyProfile(symbol, upstreamBySymbol[symbol])
		profiles[symbol] = classification
		_ = p.storeInCache(ctx, symbol, classification)
	}
	return profiles, nil
}

func (p *cachedFMPUSStockCompanyProfileProvider) fetchProfilesInBatches(ctx context.Context, symbols []string) ([]fmp.Profile, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	if len(symbols) <= usStockCompanyProfileBatchSize {
		return p.client.Profiles(ctx, symbols)
	}

	type batchResult struct {
		profiles []fmp.Profile
		err      error
	}
	batches := chunkUSStockCompanyProfileSymbols(symbols, usStockCompanyProfileBatchSize)
	results := make(chan batchResult, len(batches))
	sem := make(chan struct{}, usStockCompanyProfileBatchConcurrency)
	var wg sync.WaitGroup
	for _, batch := range batches {
		batch := batch
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- batchResult{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			profiles, err := p.client.Profiles(ctx, batch)
			results <- batchResult{profiles: profiles, err: err}
		}()
	}
	wg.Wait()
	close(results)
	merged := make([]fmp.Profile, 0, len(symbols))
	for result := range results {
		if result.err != nil {
			return nil, result.err
		}
		merged = append(merged, result.profiles...)
	}
	return merged, nil
}

func classifyUSStockCompanyProfile(symbol string, profile fmp.Profile) *dto.USStockCompanyProfile {
	normalized := normalizeUSStockCompanyProfileSymbol(symbol)
	if normalized == "" {
		return nil
	}
	classification := &dto.USStockCompanyProfile{
		Symbol:           normalized,
		Ticker:           normalized,
		Name:             strings.TrimSpace(profile.CompanyName),
		Country:          strings.TrimSpace(profile.Country),
		Currency:         strings.TrimSpace(profile.Currency),
		Exchange:         strings.TrimSpace(profile.Exchange),
		ExchangeFullName: strings.TrimSpace(profile.ExchangeFullName),
		Sector:           strings.TrimSpace(profile.Sector),
		Industry:         strings.TrimSpace(profile.Industry),
		IPO:              strings.TrimSpace(profile.IPODate),
		WebURL:           strings.TrimSpace(profile.Website),
		Logo:             strings.TrimSpace(profile.Image),
		Source:           "fmp",
		IsETF:            profile.IsETF,
		IsFund:           profile.IsFund,
	}
	if profile.MarketCap > 0 {
		value := float64(profile.MarketCap)
		classification.MarketCapitalization = &value
	}
	if classification.Name == "" && classification.Country == "" && classification.Currency == "" && classification.Exchange == "" && classification.Sector == "" && classification.Industry == "" && !classification.IsETF && !classification.IsFund {
		return nil
	}
	return classification
}

func ClassifyFMPUSStockCompanyProfile(symbol string, profile fmp.Profile) *dto.USStockCompanyProfile {
	return classifyUSStockCompanyProfile(symbol, profile)
}

func normalizeUSStockCompanyProfileSymbols(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	normalized := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalizedSymbol := normalizeUSStockCompanyProfileSymbol(symbol)
		if normalizedSymbol == "" {
			continue
		}
		if _, ok := seen[normalizedSymbol]; ok {
			continue
		}
		seen[normalizedSymbol] = struct{}{}
		normalized = append(normalized, normalizedSymbol)
	}
	return normalized
}

func chunkUSStockCompanyProfileSymbols(symbols []string, chunkSize int) [][]string {
	if chunkSize <= 0 || len(symbols) == 0 {
		return nil
	}
	batches := make([][]string, 0, (len(symbols)+chunkSize-1)/chunkSize)
	for start := 0; start < len(symbols); start += chunkSize {
		end := start + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := append([]string(nil), symbols[start:end]...)
		batches = append(batches, batch)
	}
	return batches
}

func (p *cachedFMPUSStockCompanyProfileProvider) loadFromCache(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, bool, error) {
	if p == nil || p.cache == nil {
		return nil, false, nil
	}
	payload, ok, err := p.cache.Get(ctx, usStockCompanyProfileCacheKey(symbol))
	if err != nil || !ok {
		return nil, ok, err
	}
	var record cachedUSStockCompanyProfileRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, false, err
	}
	return record.Profile, true, nil
}

func (p *cachedFMPUSStockCompanyProfileProvider) storeInCache(ctx context.Context, symbol string, profile *dto.USStockCompanyProfile) error {
	if p == nil || p.cache == nil {
		return nil
	}
	payload, err := json.Marshal(cachedUSStockCompanyProfileRecord{Profile: profile})
	if err != nil {
		return err
	}
	ttl := randomUSStockCompanyProfileTTL()
	if p.ttlValue != nil {
		ttl = p.ttlValue()
	}
	return p.cache.Set(ctx, usStockCompanyProfileCacheKey(symbol), payload, ttl)
}

func usStockCompanyProfileCacheKey(symbol string) string {
	return "us-stocks:company-profile:v3:" + normalizeUSStockCompanyProfileSymbol(symbol)
}

func normalizeUSStockCompanyProfileSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func randomUSStockCompanyProfileTTL() time.Duration {
	window := usStockCompanyProfileTTLMax - usStockCompanyProfileTTLMin
	if window <= 0 {
		return usStockCompanyProfileTTLMin
	}
	return usStockCompanyProfileTTLMin + time.Duration(rand.Int64N(int64(window)+1))
}

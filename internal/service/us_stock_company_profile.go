package service

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	usStockCompanyProfileTTLMin = 7 * 24 * time.Hour
	usStockCompanyProfileTTLMax = 14 * 24 * time.Hour
)

type usStockCompanyProfileProvider interface {
	CompanyProfile(ctx context.Context, symbol string) (*dto.USStockCompanyProfile, error)
	IsETFLike(ctx context.Context, symbol string) (bool, error)
}

type fmpCompanyProfiler interface {
	Profile(ctx context.Context, symbol string) (*fmp.Profile, error)
}

type cachedFMPUSStockCompanyProfileProvider struct {
	client   fmpCompanyProfiler
	cache    cache.Store
	ttlValue func() time.Duration
}

type cachedUSStockCompanyProfileRecord struct {
	Profile *dto.USStockCompanyProfile `json:"profile,omitempty"`
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
	classification := &dto.USStockCompanyProfile{
		Symbol:   normalized,
		Sector:   strings.TrimSpace(profile.Sector),
		Industry: strings.TrimSpace(profile.Industry),
		IsETF:    profile.IsETF,
		IsFund:   profile.IsFund,
	}
	if classification.Sector == "" && classification.Industry == "" && !classification.IsETF && !classification.IsFund {
		classification = nil
	}
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

// isETFLikeUSStockProfile centralizes the ETF/fund classification rule so
// other US-stock features can reuse a single definition.
func isETFLikeUSStockProfile(profile *dto.USStockCompanyProfile) bool {
	return profile != nil && (profile.IsETF || profile.IsFund)
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
	return "us-stocks:company-profile:v2:" + normalizeUSStockCompanyProfileSymbol(symbol)
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

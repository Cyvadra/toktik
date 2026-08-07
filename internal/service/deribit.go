package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dto"
	deribitpkg "github.com/Cyvadra/toktik/pkg/deribit"
)

const (
	deribitOptionChainTTL      = 5 * time.Second
	deribitOptionChainMaxLimit = 1000
)

type deribitClient interface {
	OptionChain(ctx context.Context, currency string) ([]deribitpkg.BookSummary, error)
}

type DeribitService struct {
	client deribitClient
	cache  cache.Store
}

func NewDeribitService(client deribitClient, store cache.Store) *DeribitService {
	return &DeribitService{client: client, cache: store}
}

func NewDeribitServiceFromConfig(cfg config.Runtime, store cache.Store) (*DeribitService, error) {
	client, err := deribitpkg.NewClient(deribitpkg.Config{
		BaseURL:  cfg.Deribit.BaseURL,
		ProxyURL: cfg.Deribit.ProxyURL,
	})
	if err != nil {
		return nil, fmt.Errorf("init deribit client: %w", err)
	}
	return NewDeribitService(client, store), nil
}

func (s *DeribitService) OptionChain(ctx context.Context, underlying string) ([]deribitpkg.BookSummary, error) {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	key := deribitCacheKey("option-chain", underlying)
	return cacheFetch(ctx, s.cache, key, deribitOptionChainTTL, func() ([]deribitpkg.BookSummary, error) {
		return s.client.OptionChain(ctx, underlying)
	})
}

func (s *DeribitService) QueryOptionChain(ctx context.Context, req dto.DeribitOptionChainRequest) (*dto.DeribitOptionChainResponse, error) {
	filter, err := newDeribitOptionChainFilter(req)
	if err != nil {
		return nil, err
	}
	raw, err := s.OptionChain(ctx, filter.underlying)
	if err != nil {
		return nil, err
	}

	data := make([]dto.DeribitOptionChainContract, 0, len(raw))
	for _, item := range raw {
		mapped, meta, err := mapDeribitOptionChainContract(item)
		if err != nil {
			return nil, &deribitpkg.ResponseError{Message: fmt.Sprintf("invalid instrument %q", item.InstrumentName)}
		}
		if !strings.EqualFold(meta.BaseAsset, filter.underlying) {
			continue
		}
		if filter.matches(meta) {
			data = append(data, mapped)
		}
	}

	sort.SliceStable(data, func(i, j int) bool {
		comparison := compareDeribitContracts(data[i], data[j], filter.sortField)
		if filter.descending {
			return comparison > 0
		}
		return comparison < 0
	})
	if filter.limit > 0 && len(data) > filter.limit {
		data = data[:filter.limit]
	}
	return &dto.DeribitOptionChainResponse{Data: data}, nil
}

type deribitOptionChainFilter struct {
	underlying    string
	expiration    *time.Time
	expirationGte *time.Time
	expirationGt  *time.Time
	expirationLte *time.Time
	expirationLt  *time.Time
	contractType  string
	strike        *float64
	strikeGte     *float64
	strikeGt      *float64
	strikeLte     *float64
	strikeLt      *float64
	sortField     string
	descending    bool
	limit         int
}

func newDeribitOptionChainFilter(req dto.DeribitOptionChainRequest) (deribitOptionChainFilter, error) {
	filter := deribitOptionChainFilter{
		underlying:   strings.ToUpper(strings.TrimSpace(req.Underlying)),
		contractType: strings.ToLower(strings.TrimSpace(req.ContractType)),
		strike:       req.StrikePrice,
		strikeGte:    req.StrikePriceGte,
		strikeGt:     req.StrikePriceGt,
		strikeLte:    req.StrikePriceLte,
		strikeLt:     req.StrikePriceLt,
		sortField:    strings.ToLower(strings.TrimSpace(req.Sort)),
		limit:        req.Limit,
	}
	if filter.underlying == "" {
		return filter, dto.NewValidationError("underlying is required")
	}
	if filter.contractType != "" && filter.contractType != "call" && filter.contractType != "put" {
		return filter, dto.NewValidationError("contract_type must be call or put")
	}
	order := strings.ToLower(strings.TrimSpace(req.Order))
	if order != "" && order != "asc" && order != "desc" {
		return filter, dto.NewValidationError("order must be asc or desc")
	}
	filter.descending = order == "desc"
	if filter.sortField == "" {
		filter.sortField = "expiration_date"
	}
	supportedSorts := map[string]bool{
		"expiration_date":    true,
		"strike_price":       true,
		"contract_type":      true,
		"ticker":             true,
		"mark_price":         true,
		"open_interest":      true,
		"implied_volatility": true,
	}
	if !supportedSorts[filter.sortField] {
		return filter, dto.NewValidationError("unsupported sort field %q", req.Sort)
	}
	if filter.limit < 0 {
		return filter, dto.NewValidationError("limit must be non-negative")
	}
	if filter.limit > deribitOptionChainMaxLimit {
		filter.limit = deribitOptionChainMaxLimit
	}

	dateFields := []struct {
		raw    string
		target **time.Time
		name   string
	}{
		{req.ExpirationDate, &filter.expiration, "expiration_date"},
		{req.ExpirationDateGte, &filter.expirationGte, "expiration_date_gte"},
		{req.ExpirationDateGt, &filter.expirationGt, "expiration_date_gt"},
		{req.ExpirationDateLte, &filter.expirationLte, "expiration_date_lte"},
		{req.ExpirationDateLt, &filter.expirationLt, "expiration_date_lt"},
	}
	for _, field := range dateFields {
		if strings.TrimSpace(field.raw) == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(field.raw))
		if err != nil {
			return filter, dto.NewValidationError("%s must use YYYY-MM-DD", field.name)
		}
		parsed = parsed.UTC()
		*field.target = &parsed
	}
	if err := validateDeribitRanges(filter); err != nil {
		return filter, err
	}
	return filter, nil
}

func validateDeribitRanges(filter deribitOptionChainFilter) error {
	if filter.expirationGte != nil && filter.expirationLte != nil && filter.expirationGte.After(*filter.expirationLte) {
		return dto.NewValidationError("expiration_date_gte must be before or equal to expiration_date_lte")
	}
	if filter.expirationGt != nil && filter.expirationLt != nil && !filter.expirationGt.Before(*filter.expirationLt) {
		return dto.NewValidationError("expiration_date_gt must be before expiration_date_lt")
	}
	if filter.strikeGte != nil && filter.strikeLte != nil && *filter.strikeGte > *filter.strikeLte {
		return dto.NewValidationError("strike_price_gte must be less than or equal to strike_price_lte")
	}
	if filter.strikeGt != nil && filter.strikeLt != nil && *filter.strikeGt >= *filter.strikeLt {
		return dto.NewValidationError("strike_price_gt must be less than strike_price_lt")
	}
	return nil
}

func (f deribitOptionChainFilter) matches(meta cryptooptions.SymbolMeta) bool {
	expiration := meta.Expiration.UTC()
	strike := float64(meta.StrikePrice)
	if f.expiration != nil && !expiration.Equal(*f.expiration) {
		return false
	}
	if f.expirationGte != nil && expiration.Before(*f.expirationGte) {
		return false
	}
	if f.expirationGt != nil && !expiration.After(*f.expirationGt) {
		return false
	}
	if f.expirationLte != nil && expiration.After(*f.expirationLte) {
		return false
	}
	if f.expirationLt != nil && !expiration.Before(*f.expirationLt) {
		return false
	}
	if f.contractType != "" && meta.OptionType != f.contractType {
		return false
	}
	if f.strike != nil && strike != *f.strike {
		return false
	}
	if f.strikeGte != nil && strike < *f.strikeGte {
		return false
	}
	if f.strikeGt != nil && strike <= *f.strikeGt {
		return false
	}
	if f.strikeLte != nil && strike > *f.strikeLte {
		return false
	}
	if f.strikeLt != nil && strike >= *f.strikeLt {
		return false
	}
	return true
}

func mapDeribitOptionChainContract(item deribitpkg.BookSummary) (dto.DeribitOptionChainContract, cryptooptions.SymbolMeta, error) {
	meta, err := cryptooptions.ParseSymbol(item.InstrumentName)
	if err != nil {
		return dto.DeribitOptionChainContract{}, cryptooptions.SymbolMeta{}, err
	}
	underlyingTicker := strings.TrimSpace(item.UnderlyingIndex)
	if underlyingTicker == "" {
		underlyingTicker = meta.BaseAsset
	}
	impliedVolatility := divideFloat(item.MarkIV, 100)
	return dto.DeribitOptionChainContract{
		Contract: dto.DeribitOptionContract{
			Ticker:           meta.Symbol,
			UnderlyingTicker: underlyingTicker,
			ContractType:     meta.OptionType,
			ExerciseStyle:    "european",
			ExpirationDate:   meta.Expiration.Format("2006-01-02"),
			StrikePrice:      float64(meta.StrikePrice),
			BaseCurrency:     strings.ToUpper(strings.TrimSpace(item.BaseCurrency)),
			QuoteCurrency:    strings.ToUpper(strings.TrimSpace(item.QuoteCurrency)),
		},
		Day: dto.DeribitOptionDay{
			High:          item.High,
			Low:           item.Low,
			ChangePercent: item.PriceChange,
			Volume:        item.Volume,
			VolumeUSD:     item.VolumeUSD,
		},
		MarkPrice:         item.MarkPrice,
		LastPrice:         item.LastPrice,
		BidPrice:          item.BidPrice,
		AskPrice:          item.AskPrice,
		MidPrice:          item.MidPrice,
		ImpliedVolatility: impliedVolatility,
		OpenInterest:      item.OpenInterest,
		UnderlyingAsset: dto.DeribitUnderlyingAsset{
			Ticker: underlyingTicker,
			Price:  item.UnderlyingPrice,
		},
		PremiumCurrency: strings.ToUpper(strings.TrimSpace(item.BaseCurrency)),
		Timestamp:       item.CreationTimestamp,
	}, meta, nil
}

func compareDeribitContracts(left, right dto.DeribitOptionChainContract, field string) int {
	var comparison int
	switch field {
	case "strike_price":
		comparison = compareFloat(left.Contract.StrikePrice, right.Contract.StrikePrice)
	case "contract_type":
		comparison = strings.Compare(left.Contract.ContractType, right.Contract.ContractType)
	case "ticker":
		comparison = strings.Compare(left.Contract.Ticker, right.Contract.Ticker)
	case "mark_price":
		comparison = compareOptionalFloat(left.MarkPrice, right.MarkPrice)
	case "open_interest":
		comparison = compareOptionalFloat(left.OpenInterest, right.OpenInterest)
	case "implied_volatility":
		comparison = compareOptionalFloat(left.ImpliedVolatility, right.ImpliedVolatility)
	default:
		comparison = strings.Compare(left.Contract.ExpirationDate, right.Contract.ExpirationDate)
		if comparison == 0 {
			comparison = compareFloat(left.Contract.StrikePrice, right.Contract.StrikePrice)
		}
		if comparison == 0 {
			comparison = strings.Compare(left.Contract.ContractType, right.Contract.ContractType)
		}
	}
	if comparison == 0 {
		comparison = strings.Compare(left.Contract.Ticker, right.Contract.Ticker)
	}
	return comparison
}

func compareFloat(left, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareOptionalFloat(left, right *float64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	return compareFloat(*left, *right)
}

func divideFloat(value *float64, divisor float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value / divisor
	return &result
}

func deribitCacheKey(parts ...string) string {
	payload := strings.Join(parts, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return "deribit:" + hex.EncodeToString(sum[:])
}

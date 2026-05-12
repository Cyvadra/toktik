package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/config"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

const (
	polygonRealtimeTTL   = 5 * time.Second
	polygonRecentTTL     = 15 * time.Second
	polygonHistoricalTTL = 60 * time.Second
	polygonContractTTL   = 6 * time.Hour
	polygonTimeCutoff    = 15 * time.Minute
	polygonDayCutoff     = 24 * time.Hour
)

type polygonClient interface {
	DownloadStockMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error)
	DownloadOptionMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error)
	StockSnapshot(ctx context.Context, symbol string) (*polygonpkg.StockSnapshot, error)
	StockAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error)
	StockQuotes(ctx context.Context, symbol string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error)
	StockTrades(ctx context.Context, symbol string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error)
	OptionContract(ctx context.Context, ticker string) (*polygonpkg.OptionContract, error)
	OptionChain(ctx context.Context, req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error)
	OptionAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error)
	OptionQuotes(ctx context.Context, ticker string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error)
	OptionTrades(ctx context.Context, ticker string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error)
}

type PolygonService struct {
	client polygonClient
	cache  cache.Store
	now    func() time.Time
}

func NewPolygonService(client polygonClient, store cache.Store) *PolygonService {
	return &PolygonService{client: client, cache: store, now: time.Now}
}

func NewPolygonServiceFromConfig(cfg config.Runtime, store cache.Store) (*PolygonService, error) {
	client, err := polygonpkg.NewFromRuntime(cfg)
	if err != nil {
		return nil, fmt.Errorf("init polygon client: %w", err)
	}
	return NewPolygonService(client, store), nil
}

func (s *PolygonService) DownloadStockMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error) {
	return s.client.DownloadStockMinuteAggregates(ctx, date, force)
}

func (s *PolygonService) DownloadOptionMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error) {
	return s.client.DownloadOptionMinuteAggregates(ctx, date, force)
}

func (s *PolygonService) StockSnapshot(ctx context.Context, symbol string) (*polygonpkg.StockSnapshot, error) {
	key := s.cacheKey("stock-snapshot", strings.ToUpper(strings.TrimSpace(symbol)))
	return cacheFetch(ctx, s.cache, key, polygonRealtimeTTL, func() (*polygonpkg.StockSnapshot, error) {
		return s.client.StockSnapshot(ctx, symbol)
	})
}

func (s *PolygonService) StockAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error) {
	ttl := s.aggregateTTL(req)
	key := s.cacheKey("stock-aggregates", req)
	return cacheFetch(ctx, s.cache, key, ttl, func() ([]polygonpkg.AggregateBar, error) {
		return s.client.StockAggregates(ctx, req)
	})
}

func (s *PolygonService) StockQuotes(ctx context.Context, symbol string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error) {
	ttl := s.quoteTradeTTL(req.Timestamp, req.TimestampGte, req.TimestampGt, req.TimestampLte, req.TimestampLt)
	key := s.cacheKey("stock-quotes", strings.ToUpper(strings.TrimSpace(symbol)), req)
	return cacheFetch(ctx, s.cache, key, ttl, func() ([]polygonpkg.Quote, error) {
		return s.client.StockQuotes(ctx, symbol, req)
	})
}

func (s *PolygonService) StockTrades(ctx context.Context, symbol string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error) {
	ttl := s.quoteTradeTTL(req.Timestamp, req.TimestampGte, req.TimestampGt, req.TimestampLte, req.TimestampLt)
	key := s.cacheKey("stock-trades", strings.ToUpper(strings.TrimSpace(symbol)), req)
	return cacheFetch(ctx, s.cache, key, ttl, func() ([]polygonpkg.Trade, error) {
		return s.client.StockTrades(ctx, symbol, req)
	})
}

func (s *PolygonService) OptionContract(ctx context.Context, ticker string) (*polygonpkg.OptionContract, error) {
	key := s.cacheKey("option-contract", strings.ToUpper(strings.TrimSpace(ticker)))
	return cacheFetch(ctx, s.cache, key, polygonContractTTL, func() (*polygonpkg.OptionContract, error) {
		return s.client.OptionContract(ctx, ticker)
	})
}

func (s *PolygonService) OptionChain(ctx context.Context, req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error) {
	key := s.cacheKey("option-chain", req)
	return cacheFetch(ctx, s.cache, key, polygonRealtimeTTL, func() ([]polygonpkg.OptionChainContract, error) {
		return s.client.OptionChain(ctx, req)
	})
}

func (s *PolygonService) OptionAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error) {
	ttl := s.aggregateTTL(req)
	key := s.cacheKey("option-aggregates", req)
	return cacheFetch(ctx, s.cache, key, ttl, func() ([]polygonpkg.AggregateBar, error) {
		return s.client.OptionAggregates(ctx, req)
	})
}

func (s *PolygonService) OptionQuotes(ctx context.Context, ticker string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error) {
	ttl := s.quoteTradeTTL(req.Timestamp, req.TimestampGte, req.TimestampGt, req.TimestampLte, req.TimestampLt)
	key := s.cacheKey("option-quotes", strings.ToUpper(strings.TrimSpace(ticker)), req)
	return cacheFetch(ctx, s.cache, key, ttl, func() ([]polygonpkg.Quote, error) {
		return s.client.OptionQuotes(ctx, ticker, req)
	})
}

func (s *PolygonService) OptionTrades(ctx context.Context, ticker string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error) {
	ttl := s.quoteTradeTTL(req.Timestamp, req.TimestampGte, req.TimestampGt, req.TimestampLte, req.TimestampLt)
	key := s.cacheKey("option-trades", strings.ToUpper(strings.TrimSpace(ticker)), req)
	return cacheFetch(ctx, s.cache, key, ttl, func() ([]polygonpkg.Trade, error) {
		return s.client.OptionTrades(ctx, ticker, req)
	})
}

func (s *PolygonService) aggregateTTL(req polygonpkg.AggregateRequest) time.Duration {
	from, okFrom := parseCacheTime(req.From)
	to, okTo := parseCacheTime(req.To)
	if okFrom && okTo {
		return s.windowTTL(from, to)
	}
	return polygonRealtimeTTL
}

func (s *PolygonService) quoteTradeTTL(values ...string) time.Duration {
	var start time.Time
	var end time.Time
	for _, raw := range values {
		parsed, ok := parseCacheTime(raw)
		if !ok {
			continue
		}
		if start.IsZero() || parsed.Before(start) {
			start = parsed
		}
		if end.IsZero() || parsed.After(end) {
			end = parsed
		}
	}
	if start.IsZero() || end.IsZero() {
		return polygonRealtimeTTL
	}
	return s.windowTTL(start, end)
}

func (s *PolygonService) windowTTL(from, to time.Time) time.Duration {
	now := s.now().UTC()
	if to.After(now.Add(-polygonTimeCutoff)) {
		return polygonRealtimeTTL
	}
	if to.After(now.Add(-polygonDayCutoff)) {
		return polygonRecentTTL
	}
	return polygonHistoricalTTL
}

func (s *PolygonService) cacheKey(parts ...any) string {
	payload, _ := json.Marshal(parts)
	sum := sha256.Sum256(payload)
	return "polygon:" + hex.EncodeToString(sum[:])
}

func cacheFetch[T any](ctx context.Context, store cache.Store, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	var zero T
	if store != nil && ttl > 0 {
		if raw, ok, err := store.Get(ctx, key); err == nil && ok {
			var cached T
			if err := json.Unmarshal(raw, &cached); err == nil {
				return cached, nil
			}
		}
	}
	value, err := load()
	if err != nil {
		return zero, err
	}
	if store != nil && ttl > 0 {
		if raw, err := json.Marshal(value); err == nil {
			_ = store.Set(ctx, key, raw, ttl)
		}
	}
	return value, nil
}

func parseCacheTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

package service

import (
	"context"
	"fmt"

	"github.com/Cyvadra/toktik/internal/dto"
)

const observedUSStockPoolTopLimit = 60

var observedUSStockPoolLookbackDays = []int{20, 60, 120}

type usTurnoverIntersectionScreener interface {
	ScreenUSTurnoverIntersection(ctx context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error)
}

func ResolveObservedUSStockPool(ctx context.Context, screener usTurnoverIntersectionScreener) ([]string, error) {
	if screener == nil {
		return nil, fmt.Errorf("observed us stock pool screener not configured")
	}
	seen := make(map[string]struct{}, observedUSStockPoolTopLimit*len(observedUSStockPoolLookbackDays))
	pool := make([]string, 0, observedUSStockPoolTopLimit*len(observedUSStockPoolLookbackDays))
	for _, lookbackDays := range observedUSStockPoolLookbackDays {
		resp, err := screener.ScreenUSTurnoverIntersection(ctx, dto.ScreenUSTurnoverIntersectionRequest{
			Limit:        observedUSStockPoolTopLimit,
			LookbackDays: lookbackDays,
			NonETFOnly:   true,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve observed us stock pool for %d-day turnover: %w", lookbackDays, err)
		}
		for _, row := range resp.Data {
			symbol := normalizeSymbol(row.Underlying)
			if symbol == "" {
				continue
			}
			if _, ok := seen[symbol]; ok {
				continue
			}
			seen[symbol] = struct{}{}
			pool = append(pool, symbol)
		}
	}
	return pool, nil
}

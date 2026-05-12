package fmp

import (
	"context"
	"sort"
)

// DailyBar combines an end-of-day EODPrice with the most recently applicable
// annual ratio snapshot (PE, PB, PS, EPS, BVPS).
type DailyBar struct {
	EODPrice

	// RatioDate is the fiscal year-end date that sourced the ratio values.
	RatioDate  string `json:"ratioDate"`
	FiscalYear string `json:"fiscalYear"`

	PriceToEarningsRatio float64 `json:"pe"`
	PriceToBookRatio     float64 `json:"pb"`
	PriceToSalesRatio    float64 `json:"ps"`
	EarningsPerShare     float64 `json:"eps"`
	BookValuePerShare    float64 `json:"bvps"`
}

// DailyWithFundamentals fetches EOD prices for the date range and stitches the
// most recent available annual ratios onto each bar. It makes two API calls.
func (c *Client) DailyWithFundamentals(ctx context.Context, symbol, from, to string) ([]DailyBar, error) {
	prices, err := c.HistoricalPrices(ctx, symbol, from, to)
	if err != nil {
		return nil, err
	}

	ratios, err := c.Ratios(ctx, symbol, "annual", 10)
	if err != nil {
		return nil, err
	}

	sort.Slice(ratios, func(i, j int) bool { return ratios[i].Date < ratios[j].Date })

	findRatio := func(tradeDate string) *Ratios {
		lo, hi, best := 0, len(ratios)-1, -1
		for lo <= hi {
			mid := (lo + hi) / 2
			if ratios[mid].Date <= tradeDate {
				best = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		if best < 0 {
			return nil
		}
		return &ratios[best]
	}

	bars := make([]DailyBar, 0, len(prices))
	for _, p := range prices {
		bar := DailyBar{EODPrice: p}
		if r := findRatio(p.Date); r != nil {
			bar.RatioDate = r.Date
			bar.FiscalYear = r.FiscalYear
			bar.PriceToEarningsRatio = r.PriceToEarningsRatio
			bar.PriceToBookRatio = r.PriceToBookRatio
			bar.PriceToSalesRatio = r.PriceToSalesRatio
			bar.EarningsPerShare = r.NetIncomePerShare
			bar.BookValuePerShare = r.BookValuePerShare
		}
		bars = append(bars, bar)
	}
	return bars, nil
}

package datafeed

import (
	"context"
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/feeds"
)

// FeedFactorBridge adapts a pkg/feeds.Store query into a backtest.FactorFeed.
// This allows any registered external data feed (e.g., dvol) to be used as a
// factor in backtesting strategies via ctx.AddFactor("dvol", "1h").
type FeedFactorBridge struct {
	feedName string
	store    *feeds.Store
	fields   []string
}

// NewFeedFactorBridge creates a bridge that serves feed data from ClickHouse
// to the backtest engine.
func NewFeedFactorBridge(feedName string, store *feeds.Store) *FeedFactorBridge {
	f := feeds.Get(feedName)
	var fields []string
	if f != nil {
		fields = f.Fields()
	} else {
		fields = []string{"open", "high", "low", "close"}
	}
	return &FeedFactorBridge{
		feedName: feedName,
		store:    store,
		fields:   fields,
	}
}

// Load implements backtest.FactorFeed. It reads bars from the appropriate
// feed_<name>_<interval> table and converts them to a columnar DataSet.
func (b *FeedFactorBridge) Load(ctx context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	w, err := feeds.ParseWindow(req.Interval)
	if err != nil {
		return nil, fmt.Errorf("parse interval %q: %w", req.Interval, err)
	}

	// FactorRequest.Name is "<feedName>" or "<feedName>:<symbol>"
	symbol := resolveSymbol(b.feedName, req.Name)

	bars, err := b.store.QueryBars(ctx, b.feedName, w, symbol, req.From, req.To)
	if err != nil {
		return nil, fmt.Errorf("query feed %s/%s/%s: %w", b.feedName, symbol, w.Label, err)
	}

	ds := backtest.NewDataSet(len(bars))
	opens := make([]float64, len(bars))
	highs := make([]float64, len(bars))
	lows := make([]float64, len(bars))
	closes := make([]float64, len(bars))

	for i, bar := range bars {
		ds.Timestamps = append(ds.Timestamps, bar.Timestamp)
		opens[i] = bar.Open
		highs[i] = bar.High
		lows[i] = bar.Low
		closes[i] = bar.Close
	}

	ds.Len = len(bars)
	for name, col := range map[string][]float64{
		"open":  opens,
		"high":  highs,
		"low":   lows,
		"close": closes,
	} {
		if err := ds.AddColumn(name, col); err != nil {
			return nil, fmt.Errorf("build factor dataset: %w", err)
		}
	}

	return ds, nil
}

// Fields implements backtest.FactorFeed.
func (b *FeedFactorBridge) Fields() []string {
	return b.fields
}

// resolveSymbol extracts a symbol from the factor name.
// Format: "dvol" (use first symbol) or "dvol:BTC" (explicit symbol).
func resolveSymbol(feedName, factorName string) string {
	for i := range factorName {
		if factorName[i] == ':' {
			return factorName[i+1:]
		}
	}

	f := feeds.Get(feedName)
	if f != nil {
		syms := f.Symbols()
		if len(syms) > 0 {
			return syms[0]
		}
	}
	return ""
}

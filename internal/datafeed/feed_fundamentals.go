package datafeed

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
)

// FundamentalsQuerier is the subset of the fundamentals service needed by the
// factor-feed bridge. Defined here to keep datafeed independent of the
// service package and to make unit testing trivial.
type FundamentalsQuerier interface {
	QuerySeries(ctx context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error)
}

// FundamentalsFactorFeed adapts the symbol-bound fundamentals store into a
// backtest.FactorFeed. A single registered feed services many symbols by
// inspecting FactorRequest.Market and FactorRequest.Symbol. The feed name
// passed to Engine.RegisterFactorFeed must match the factor code used in
// SetupContext.AddSymbolFactor (e.g., "pe", "pb", "market_cap").
//
// The DataSet returned by Load contains a single value column named "value"
// at the requested interval grid. Sparse fundamentals are forward-filled per
// the service's catalog policy and emitted as NaN before the first known
// observation.
type FundamentalsFactorFeed struct {
	svc        FundamentalsQuerier
	factorCode string
	mode       string // service-side mode passed through (default "as_of")
}

// NewFundamentalsFactorFeed constructs a feed bound to a single factor code.
//
// Strategies register one feed per factor (e.g., one for "pe", one for "pb")
// and then call ctx.AddSymbolFactor(factorCode, market, symbol, interval) per
// symbol they need. All symbols share the same registered feed instance.
func NewFundamentalsFactorFeed(svc FundamentalsQuerier, factorCode string) *FundamentalsFactorFeed {
	return &FundamentalsFactorFeed{
		svc:        svc,
		factorCode: factorCode,
		mode:       "as_of",
	}
}

// WithMode overrides the service-side series mode (event | as_of | filled).
func (f *FundamentalsFactorFeed) WithMode(mode string) *FundamentalsFactorFeed {
	mode = strings.TrimSpace(mode)
	if mode != "" {
		f.mode = mode
	}
	return f
}

// Load implements backtest.FactorFeed. It expects req.Market and req.Symbol
// to identify the symbol-bound series to fetch. The returned DataSet exposes
// one column named "value" plus the original event_ts as Timestamps.
//
// Forward-fill onto a uniform interval grid is intentionally NOT performed
// here; the backtest preparer's alignment pipeline aligns the sparse series
// against the primary security's bar timestamps. Indicators computed on the
// resulting series then see forward-filled values whenever the most recent
// known value carries forward to the bar timestamp via standard alignment.
func (f *FundamentalsFactorFeed) Load(ctx context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	if req.Market == "" || req.Symbol == "" {
		return nil, fmt.Errorf("fundamentals factor %q requires Market and Symbol on FactorRequest", f.factorCode)
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = f.mode
	}
	resp, err := f.svc.QuerySeries(ctx, dto.FundamentalSeriesRequest{
		Market: req.Market,
		Symbol: req.Symbol,
		Factor: f.factorCode,
		From:   req.From.UTC().Format(time.RFC3339Nano),
		To:     req.To.UTC().Format(time.RFC3339Nano),
		Mode:   mode,
		AsOf:   req.To.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("load fundamental %s/%s/%s: %w", req.Market, req.Symbol, f.factorCode, err)
	}

	n := len(resp.Data)
	ds := backtest.NewDataSet(n)
	values := make([]float64, n)
	for i, p := range resp.Data {
		ds.Timestamps = append(ds.Timestamps, p.EventTS)
		if math.IsNaN(p.Value) {
			values[i] = math.NaN()
		} else {
			values[i] = p.Value
		}
	}
	ds.Len = n
	ds.AddColumn("value", values)
	return ds, nil
}

// Fields implements backtest.FactorFeed. Fundamentals expose a single
// "value" column. Indicators registered against this factor address the
// column by that name.
func (f *FundamentalsFactorFeed) Fields() []string {
	return []string{"value"}
}

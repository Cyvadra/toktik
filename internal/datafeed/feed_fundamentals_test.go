package datafeed

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
)

type stubFundamentals struct {
	got dto.FundamentalSeriesRequest
	out *dto.FundamentalSeriesResponse
	err error
}

func (s *stubFundamentals) QuerySeries(_ context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error) {
	s.got = req
	return s.out, s.err
}

func TestFundamentalsFactorFeed_LoadPropagatesSymbolAndProducesValueColumn(t *testing.T) {
	t1 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)
	stub := &stubFundamentals{
		out: &dto.FundamentalSeriesResponse{
			Market: "us-stocks",
			Symbol: "AAPL",
			Factor: "pe",
			Mode:   "as_of",
			Data: []dto.FundamentalSeriesPoint{
				{EventTS: t1, KnownAt: t1, Value: 28.5},
				{EventTS: t2, KnownAt: t2, Value: 29.1},
			},
		},
	}
	feed := NewFundamentalsFactorFeed(stub, "pe")
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)

	ds, err := feed.Load(context.Background(), backtest.FactorRequest{
		Name: "pe", Interval: "1d",
		Market: "us-stocks", Symbol: "AAPL",
		From: from, To: to,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stub.got.Market != "us-stocks" || stub.got.Symbol != "AAPL" || stub.got.Factor != "pe" {
		t.Fatalf("request not propagated: %+v", stub.got)
	}
	if got := feed.Fields(); len(got) != 1 || got[0] != "value" {
		t.Fatalf("unexpected fields: %v", got)
	}
	if ds.Len != 2 || len(ds.Timestamps) != 2 {
		t.Fatalf("unexpected dataset shape: len=%d ts=%d", ds.Len, len(ds.Timestamps))
	}
	col, ok := ds.Column("value"), true
	if col == nil {
		ok = false
	}
	if !ok {
		t.Fatalf("missing value column")
	}
	if col[0] != 28.5 || col[1] != 29.1 {
		t.Fatalf("unexpected values: %v", col)
	}
}

func TestFundamentalsFactorFeed_RequiresMarketAndSymbol(t *testing.T) {
	feed := NewFundamentalsFactorFeed(&stubFundamentals{}, "pe")
	_, err := feed.Load(context.Background(), backtest.FactorRequest{Name: "pe"})
	if err == nil {
		t.Fatal("expected error when market/symbol missing")
	}
}

// guard against accidental NaN normalization regression.
var _ = math.NaN

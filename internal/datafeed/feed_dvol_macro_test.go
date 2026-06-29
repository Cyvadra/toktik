package datafeed

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	usmacro "github.com/Cyvadra/toktik/internal/usmarket/macro"
)

type stubMacroSeries struct {
	got dto.MacroSeriesRequest
	out *dto.MacroSeriesResponse
	err error
}

func (s *stubMacroSeries) QuerySeries(_ context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error) {
	s.got = req
	return s.out, s.err
}

func TestDVOLMacroFactorFeedLoadMapsSymbolAndBuildsOHLCColumns(t *testing.T) {
	t1 := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	stub := &stubMacroSeries{out: &dto.MacroSeriesResponse{Dataset: usmacro.DefaultDeribitDVOLETHDataset, Interval: "1h", Data: []dto.MacroSeriesPoint{
		{Factor: "close", Timestamp: t2, EventTS: t2, Value: 44},
		{Factor: "open", Timestamp: t1, EventTS: t1, Value: 40},
		{Factor: "high", Timestamp: t1, EventTS: t1, Value: 45},
		{Factor: "low", Timestamp: t1, EventTS: t1, Value: 39},
		{Factor: "close", Timestamp: t1, EventTS: t1, Value: 43},
		{Factor: "open", Timestamp: t2, EventTS: t2, Value: 43},
		{Factor: "high", Timestamp: t2, EventTS: t2, Value: 46},
		{Factor: "low", Timestamp: t2, EventTS: t2, Value: 42},
	}}}
	feed := NewDVOLMacroFactorFeed(stub)

	ds, err := feed.Load(context.Background(), backtest.FactorRequest{Name: "dvol:ETH", Interval: "1h", From: t1, To: t2.Add(time.Hour)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stub.got.Dataset != usmacro.DefaultDeribitDVOLETHDataset || stub.got.Interval != "1h" {
		t.Fatalf("unexpected macro request: %+v", stub.got)
	}
	if ds.Len != 2 || !ds.Timestamps[0].Equal(t1) || !ds.Timestamps[1].Equal(t2) {
		t.Fatalf("unexpected timestamps: len=%d %v", ds.Len, ds.Timestamps)
	}
	if got := ds.Column("open"); got[0] != 40 || got[1] != 43 {
		t.Fatalf("open=%v", got)
	}
	if got := ds.Column("high"); got[0] != 45 || got[1] != 46 {
		t.Fatalf("high=%v", got)
	}
	if got := ds.Column("low"); got[0] != 39 || got[1] != 42 {
		t.Fatalf("low=%v", got)
	}
	if got := ds.Column("close"); got[0] != 43 || got[1] != 44 {
		t.Fatalf("close=%v", got)
	}
}

func TestDVOLMacroFactorFeedDefaultsToBTC(t *testing.T) {
	stub := &stubMacroSeries{out: &dto.MacroSeriesResponse{}}
	feed := NewDVOLMacroFactorFeed(stub)
	_, err := feed.Load(context.Background(), backtest.FactorRequest{Name: "dvol", Interval: "1h", From: time.Now().UTC(), To: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if stub.got.Dataset != usmacro.DefaultDeribitDVOLBTCDataset {
		t.Fatalf("Dataset=%s want %s", stub.got.Dataset, usmacro.DefaultDeribitDVOLBTCDataset)
	}
}

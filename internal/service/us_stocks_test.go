package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

type stubUSStockFundamentals struct {
	snapshotReqs []dto.FundamentalSnapshotRequest
	seriesReqs   []dto.FundamentalSeriesRequest
	snapshotResp *dto.FundamentalSnapshotResponse
	seriesResp   map[string]*dto.FundamentalSeriesResponse
}

func (s *stubUSStockFundamentals) QuerySnapshot(_ context.Context, req dto.FundamentalSnapshotRequest) (*dto.FundamentalSnapshotResponse, error) {
	s.snapshotReqs = append(s.snapshotReqs, req)
	if s.snapshotResp == nil {
		return &dto.FundamentalSnapshotResponse{}, nil
	}
	return s.snapshotResp, nil
}

func (s *stubUSStockFundamentals) QuerySeries(_ context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error) {
	s.seriesReqs = append(s.seriesReqs, req)
	if resp, ok := s.seriesResp[req.Factor]; ok {
		return resp, nil
	}
	return &dto.FundamentalSeriesResponse{}, nil
}

func TestUSStocksAttachFundamentalsAlignsPointInTimeValuesToBars(t *testing.T) {
	start := time.Date(2026, 4, 30, 13, 30, 0, 0, time.UTC)
	bars := []dto.USStockBarRow{
		{Timestamp: start, Symbol: "AAPL"},
		{Timestamp: start.Add(24 * time.Hour), Symbol: "AAPL"},
		{Timestamp: start.Add(48 * time.Hour), Symbol: "AAPL"},
	}

	stub := &stubUSStockFundamentals{
		snapshotResp: &dto.FundamentalSnapshotResponse{
			Data: []dto.FundamentalSnapshotEntry{
				{Factor: "pe", EventTS: time.Date(2025, 12, 27, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 1, 30, 6, 1, 32, 0, time.UTC), Value: 34.56, Source: "fmp_quarter_statements"},
			},
		},
		seriesResp: map[string]*dto.FundamentalSeriesResponse{
			"pe": {
				Data: []dto.FundamentalSeriesPoint{
					{EventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Value: 22.03, Source: "fmp_quarter_statements", Revision: 0},
				},
			},
			"pb": {
				Data: []dto.FundamentalSeriesPoint{
					{EventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Value: 6.65, Source: "fmp_quarter_statements", Revision: 0},
				},
			},
		},
	}

	svc := NewUSStocksService(nil, stub)
	if err := svc.attachFundamentals(context.Background(), "AAPL", []string{"pe", "pb"}, bars); err != nil {
		t.Fatalf("attachFundamentals returned error: %v", err)
	}

	if got := bars[0].Fundamentals["pe"].Value; got != 34.56 {
		t.Fatalf("expected first bar to use snapshot PE, got %v", got)
	}
	if _, ok := bars[0].Fundamentals["pb"]; ok {
		t.Fatalf("expected first bar to omit PB before first known value, got %#v", bars[0].Fundamentals["pb"])
	}
	if got := bars[1].Fundamentals["pe"].Value; got != 22.03 {
		t.Fatalf("expected second bar to pick up newly known PE, got %v", got)
	}
	if got := bars[1].Fundamentals["pb"].Value; got != 6.65 {
		t.Fatalf("expected second bar to pick up newly known PB, got %v", got)
	}
	if !bars[2].Fundamentals["pe"].Filled || !bars[2].Fundamentals["pb"].Filled {
		t.Fatalf("expected later bars to expose carried-forward fundamentals, got %#v", bars[2].Fundamentals)
	}
	if len(stub.snapshotReqs) != 1 {
		t.Fatalf("expected one snapshot request, got %d", len(stub.snapshotReqs))
	}
	if len(stub.seriesReqs) != 2 {
		t.Fatalf("expected one series request per factor, got %d", len(stub.seriesReqs))
	}
}

func TestNormalizeRequestedFactorsSplitsCommaSeparatedValues(t *testing.T) {
	got := normalizeRequestedFactors([]string{"pe,pb", " pb ", "pe", ""})
	if len(got) != 2 || got[0] != "pe" || got[1] != "pb" {
		t.Fatalf("normalizeRequestedFactors returned %#v", got)
	}
}

func TestPriceDerivedFundamentalValueUsesCurrentBarCloseForPEAndPB(t *testing.T) {
	observation := dto.USStockBarFundamentalValue{
		EventTS: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		Value:   20,
	}
	denominators := map[string]float64{
		fundamentalObservationKey("pe", observation.EventTS): 10,
	}
	got := priceDerivedFundamentalValue("pe", 250, observation, denominators)
	if got != 25 {
		t.Fatalf("expected price-derived PE to be recomputed from bar close, got %v", got)
	}
	if got := priceDerivedFundamentalValue("market_cap", 250, observation, denominators); got != 20 {
		t.Fatalf("expected non price-derived factor to keep stored value, got %v", got)
	}
}

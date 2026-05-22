package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

type stubMacroSeriesProvider struct {
	resp *dto.MacroSeriesResponse
	err  error
	reqs []dto.MacroSeriesRequest
}

func (s *stubMacroSeriesProvider) QuerySeries(_ context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error) {
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.resp == nil {
		return &dto.MacroSeriesResponse{}, nil
	}
	return s.resp, nil
}

func TestResolveVirtualFundamentalMacroTarget(t *testing.T) {
	tests := []struct {
		name          string
		market        string
		symbol        string
		factor        string
		wantOK        bool
		wantDataset   string
		wantRefSymbol string
		wantMacro     string
	}{
		{name: "spy direct", market: "us-stocks", symbol: "SPY", factor: "pe10_live", wantOK: true, wantDataset: macroDatasetFMPSP500Shiller, wantRefSymbol: "SPY", wantMacro: "pe10_live"},
		{name: "spx alias", market: "us-stocks", symbol: "SPX", factor: "pe10_live", wantOK: true, wantDataset: macroDatasetFMPSP500Shiller, wantRefSymbol: "SPY", wantMacro: "pe10_live"},
		{name: "qqq direct", market: "us-stocks", symbol: "QQQ", factor: "pe10_live", wantOK: false},
		{name: "ndx alias", market: "us-stocks", symbol: "NDX", factor: "pe10_live", wantOK: false},
		{name: "other symbol", market: "us-stocks", symbol: "IWM", factor: "pe10_live", wantOK: false},
		{name: "spy trailing pe", market: "us-stocks", symbol: "SPY", factor: "pe", wantOK: true, wantDataset: macroDatasetFMPSP500Shiller, wantRefSymbol: "SPY", wantMacro: "pe"},
		{name: "qqq trailing pe", market: "us-stocks", symbol: "QQQ", factor: "pe", wantOK: true, wantDataset: macroDatasetFMPNDXShiller, wantRefSymbol: "QQQ", wantMacro: "pe"},
		{name: "ndx trailing pe", market: "us-stocks", symbol: "NDX", factor: "pe", wantOK: true, wantDataset: macroDatasetFMPNDXShiller, wantRefSymbol: "QQQ", wantMacro: "pe"},
		{name: "other factor", market: "us-stocks", symbol: "SPY", factor: "pb", wantOK: false},
		{name: "other market", market: "crypto-spot", symbol: "SPY", factor: "pe10_live", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveVirtualFundamentalMacroTarget(tc.market, tc.symbol, tc.factor)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Dataset != tc.wantDataset {
				t.Fatalf("dataset=%q want %q", got.Dataset, tc.wantDataset)
			}
			if got.ReferenceSymbol != tc.wantRefSymbol {
				t.Fatalf("reference symbol=%q want %q", got.ReferenceSymbol, tc.wantRefSymbol)
			}
			if got.MacroFactor != tc.wantMacro {
				t.Fatalf("macro factor=%q want %q", got.MacroFactor, tc.wantMacro)
			}
		})
	}
}

func TestAppendVirtualFundamentalCatalogEntries(t *testing.T) {
	provider := newVirtualFundamentalsProvider(nil)
	entries := provider.appendCatalogEntries(nil, "us-stocks")
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].FactorCode != virtualFundamentalFactorPE10Live {
		t.Fatalf("factor=%q want %q", entries[0].FactorCode, virtualFundamentalFactorPE10Live)
	}

	entries = provider.appendCatalogEntries(entries, "us-stocks")
	if len(entries) != 1 {
		t.Fatalf("expected deduplicated virtual factor, got %d entries", len(entries))
	}

	otherMarket := provider.appendCatalogEntries(nil, "crypto-spot")
	if len(otherMarket) != 0 {
		t.Fatalf("expected no virtual entries for crypto-spot, got %d", len(otherMarket))
	}
}

func TestSplitFundamentalFactorSelection(t *testing.T) {
	selection := splitFundamentalFactorSelection([]string{"pb", virtualFundamentalFactorPE10Live, "pe"})
	if !selection.includePE {
		t.Fatalf("expected virtual pe to be selected")
	}
	if !selection.includePE10Live {
		t.Fatalf("expected virtual pe10_live to be selected")
	}
	if len(selection.base) != 2 {
		t.Fatalf("len(base)=%d want 2", len(selection.base))
	}
	if selection.base[0] != "pb" || selection.base[1] != "pe" {
		t.Fatalf("base=%v want [pb pe]", selection.base)
	}

	selection = splitFundamentalFactorSelection([]string{virtualFundamentalFactorPE, virtualFundamentalFactorPE10Live})
	if !selection.includePE {
		t.Fatalf("expected virtual-only selection to include pe")
	}
	if !selection.includePE10Live {
		t.Fatalf("expected virtual-only selection to include pe10_live")
	}
	if len(selection.base) != 1 || selection.base[0] != "pe" {
		t.Fatalf("expected base factors [pe] for virtual-only selection, got %v", selection.base)
	}
}

func TestVirtualFundamentalsQuerySeriesUsesDailyTimestampAsEventTS(t *testing.T) {
	macro := &stubMacroSeriesProvider{
		resp: &dto.MacroSeriesResponse{
			Data: []dto.MacroSeriesPoint{{
				Timestamp: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
				EventTS:   time.Date(2025, 11, 28, 17, 59, 0, 0, time.UTC),
				KnownAt:   time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC),
				Value:     39.18,
				Source:    "fmp",
			}},
		},
	}
	provider := newVirtualFundamentalsProvider(macro)

	points, _, handled, err := provider.querySeries(context.Background(), dto.FundamentalSeriesRequest{From: "2026-01-02T00:00:00Z", To: "2026-01-07T00:00:00.000000001Z"}, "us-stocks", "SPY", virtualFundamentalFactorPE10Live, fundamentalSeriesModeFilled)
	if err != nil {
		t.Fatalf("querySeries returned error: %v", err)
	}
	if !handled {
		t.Fatalf("expected virtual series to be handled")
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	if !points[0].EventTS.Equal(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected daily timestamp as event_ts, got %s", points[0].EventTS)
	}
	if got := macro.reqs[0].To; got != "2026-01-08T00:00:00Z" {
		t.Fatalf("expected expanded daily range end, got %s", got)
	}
}

func TestVirtualFundamentalsQuerySeriesHandlesTrailingPE(t *testing.T) {
	macro := &stubMacroSeriesProvider{
		resp: &dto.MacroSeriesResponse{
			Data: []dto.MacroSeriesPoint{{
				Timestamp: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
				EventTS:   time.Date(2025, 11, 28, 17, 59, 0, 0, time.UTC),
				KnownAt:   time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC),
				Value:     31.82,
				Source:    "fmp",
			}},
		},
	}
	provider := newVirtualFundamentalsProvider(macro)

	points, fillPolicy, handled, err := provider.querySeries(context.Background(), dto.FundamentalSeriesRequest{From: "2026-01-02T00:00:00Z", To: "2026-01-07T00:00:00Z"}, "us-stocks", "QQQ", virtualFundamentalFactorPE, fundamentalSeriesModeFilled)
	if err != nil {
		t.Fatalf("querySeries returned error: %v", err)
	}
	if !handled {
		t.Fatalf("expected virtual series to be handled")
	}
	if fillPolicy != fundamentalFillForwardFill {
		t.Fatalf("fillPolicy=%q want %q", fillPolicy, fundamentalFillForwardFill)
	}
	if len(points) != 1 || points[0].Value != 31.82 {
		t.Fatalf("unexpected points: %#v", points)
	}
	if got := macro.reqs[0].Factors; len(got) != 1 || got[0] != virtualFundamentalFactorPE {
		t.Fatalf("unexpected macro factors: %v", got)
	}
}

func TestVirtualFundamentalsQuerySeriesDoesNotHandleQQQPE10Live(t *testing.T) {
	macro := &stubMacroSeriesProvider{
		resp: &dto.MacroSeriesResponse{
			Data: []dto.MacroSeriesPoint{{
				Timestamp: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
				EventTS:   time.Date(2025, 11, 28, 17, 59, 0, 0, time.UTC),
				KnownAt:   time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC),
				Value:     31.82,
				Source:    "fmp",
			}},
		},
	}
	provider := newVirtualFundamentalsProvider(macro)

	points, _, handled, err := provider.querySeries(context.Background(), dto.FundamentalSeriesRequest{From: "2026-01-02T00:00:00Z", To: "2026-01-07T00:00:00Z"}, "us-stocks", "QQQ", virtualFundamentalFactorPE10Live, fundamentalSeriesModeFilled)
	if err != nil {
		t.Fatalf("querySeries returned error: %v", err)
	}
	if handled {
		t.Fatalf("expected qqq pe10_live to bypass virtual fundamentals")
	}
	if len(points) != 0 {
		t.Fatalf("expected no virtual points, got %#v", points)
	}
	if len(macro.reqs) != 0 {
		t.Fatalf("expected no macro requests, got %#v", macro.reqs)
	}
}

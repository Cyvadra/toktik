package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/Cyvadra/toktik/internal/logorepo"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

type stubFMPLogoProfiler struct {
	profileCalls int
}

func (s *stubFMPLogoProfiler) Profile(context.Context, string) (*fmp.Profile, error) {
	s.profileCalls++
	return nil, fmt.Errorf("fmp should not be called")
}

func (s *stubFMPLogoProfiler) Profiles(context.Context, []string) ([]fmp.Profile, error) {
	return nil, nil
}

func TestUSStockLogoSymbolCandidatesUseAliasGroups(t *testing.T) {
	tests := []struct {
		symbol string
		want   []string
	}{
		{symbol: "GOOG", want: []string{"GOOG", "GOOGL"}},
		{symbol: "GOOGL", want: []string{"GOOGL", "GOOG"}},
		{symbol: "SPX", want: []string{"SPX", "SPY"}},
		{symbol: "brk-b", want: []string{"BRK-B", "BRK.B", "BRK.A", "BRK-A"}},
		{symbol: "AAPL", want: []string{"AAPL"}},
	}
	for _, tc := range tests {
		if got := usStockLogoSymbolCandidates(tc.symbol); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("usStockLogoSymbolCandidates(%q) = %#v, want %#v", tc.symbol, got, tc.want)
		}
	}
}

func TestUSStockLogoGetLogoReturnsDefaultWithoutLiveFMPFetch(t *testing.T) {
	fmpClient := &stubFMPLogoProfiler{}
	svc := &USStockLogoService{repo: nilLogoRepo{}, fmpClient: fmpClient}

	logo, err := svc.GetLogo(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("GetLogo returned error: %v", err)
	}
	if logo == nil || !logo.Default || logo.ContentType != "image/png" {
		t.Fatalf("expected default png logo, got %#v", logo)
	}
	if fmpClient.profileCalls != 0 {
		t.Fatalf("GetLogo called FMP %d times; public path must be cache-only", fmpClient.profileCalls)
	}
}

type nilLogoRepo struct{}

func (nilLogoRepo) Find(context.Context, string) (*logorepo.StockLogo, bool, error) {
	return nil, false, nil
}

func (nilLogoRepo) Upsert(context.Context, logorepo.StockLogo) error {
	return nil
}

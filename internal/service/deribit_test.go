package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
	deribitpkg "github.com/Cyvadra/toktik/pkg/deribit"
)

type stubDeribitClient struct {
	rows  []deribitpkg.BookSummary
	err   error
	calls int
}

func (s *stubDeribitClient) OptionChain(context.Context, string) ([]deribitpkg.BookSummary, error) {
	s.calls++
	return s.rows, s.err
}

func TestDeribitServiceQueryOptionChainMapsFiltersAndCaches(t *testing.T) {
	markIV := 65.75
	markPrice := 0.15
	underlyingPrice := 64892.68
	client := &stubDeribitClient{rows: []deribitpkg.BookSummary{
		{
			InstrumentName:    "BTC-28AUG26-110000-P",
			BaseCurrency:      "BTC",
			QuoteCurrency:     "USD",
			UnderlyingIndex:   "BTC-USD",
			CreationTimestamp: 1786100000000,
			MarkPrice:         &markPrice,
			MarkIV:            &markIV,
			UnderlyingPrice:   &underlyingPrice,
		},
		{
			InstrumentName:    "BTC-28AUG26-90000-C",
			BaseCurrency:      "BTC",
			QuoteCurrency:     "USD",
			UnderlyingIndex:   "BTC-USD",
			CreationTimestamp: 1786100000001,
		},
		{
			InstrumentName:    "BTC-25SEP26-100000-C",
			BaseCurrency:      "BTC",
			QuoteCurrency:     "USD",
			UnderlyingIndex:   "BTC-USD",
			CreationTimestamp: 1786100000002,
		},
	}}
	service := NewDeribitService(client, cache.NewMemoryStore())

	putResponse, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{
		Underlying:   "btc",
		ContractType: "put",
	})
	if err != nil {
		t.Fatalf("QueryOptionChain put: %v", err)
	}
	if len(putResponse.Data) != 1 {
		t.Fatalf("len(putResponse.Data)=%d want 1", len(putResponse.Data))
	}
	contract := putResponse.Data[0]
	if contract.Contract.Ticker != "BTC-28AUG26-110000-P" || contract.Contract.ContractType != "put" {
		t.Fatalf("unexpected contract: %#v", contract.Contract)
	}
	if contract.Contract.ExpirationDate != "2026-08-28" || contract.Contract.ExerciseStyle != "european" {
		t.Fatalf("unexpected expiry/style: %#v", contract.Contract)
	}
	if contract.ImpliedVolatility == nil || *contract.ImpliedVolatility != 0.6575 {
		t.Fatalf("ImpliedVolatility=%v want 0.6575", contract.ImpliedVolatility)
	}
	if contract.PremiumCurrency != "BTC" || contract.UnderlyingAsset.Ticker != "BTC-USD" {
		t.Fatalf("unexpected currency/underlying: %#v", contract)
	}

	strike := 90000.0
	callResponse, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{
		Underlying:  "BTC",
		StrikePrice: &strike,
	})
	if err != nil {
		t.Fatalf("QueryOptionChain strike: %v", err)
	}
	if len(callResponse.Data) != 1 || callResponse.Data[0].Contract.Ticker != "BTC-28AUG26-90000-C" {
		t.Fatalf("unexpected strike response: %#v", callResponse.Data)
	}
	if client.calls != 1 {
		t.Fatalf("client calls=%d want 1 shared cached fetch", client.calls)
	}
}

func TestDeribitServiceQueryOptionChainSortsAndLimits(t *testing.T) {
	client := &stubDeribitClient{rows: []deribitpkg.BookSummary{
		{InstrumentName: "BTC-25SEP26-100000-C", BaseCurrency: "BTC"},
		{InstrumentName: "BTC-28AUG26-110000-P", BaseCurrency: "BTC"},
		{InstrumentName: "BTC-28AUG26-90000-C", BaseCurrency: "BTC"},
	}}
	service := NewDeribitService(client, nil)

	response, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{
		Underlying: "BTC",
		Sort:       "strike_price",
		Order:      "desc",
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("QueryOptionChain: %v", err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("len(response.Data)=%d want 2", len(response.Data))
	}
	if response.Data[0].Contract.StrikePrice != 110000 || response.Data[1].Contract.StrikePrice != 100000 {
		t.Fatalf("unexpected order: %#v", response.Data)
	}
}

func TestDeribitServiceQueryOptionChainValidation(t *testing.T) {
	service := NewDeribitService(&stubDeribitClient{}, nil)
	tests := []dto.DeribitOptionChainRequest{
		{Underlying: "BTC", ContractType: "future"},
		{Underlying: "BTC", Order: "sideways"},
		{Underlying: "BTC", Sort: "unknown"},
		{Underlying: "BTC", ExpirationDate: "08/28/2026"},
		{Underlying: "BTC", Limit: -1},
	}
	for _, request := range tests {
		_, err := service.QueryOptionChain(context.Background(), request)
		var validationError *dto.ValidationError
		if !errors.As(err, &validationError) {
			t.Fatalf("request=%#v error=%T %v want ValidationError", request, err, err)
		}
	}
}

func TestDeribitServiceRejectsMalformedInstrument(t *testing.T) {
	service := NewDeribitService(&stubDeribitClient{rows: []deribitpkg.BookSummary{
		{InstrumentName: "not-an-option"},
	}}, nil)

	_, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{Underlying: "BTC"})
	if err == nil {
		t.Fatal("expected malformed instrument error")
	}
}

func TestDeribitServiceReturnsEmptySlice(t *testing.T) {
	service := NewDeribitService(&stubDeribitClient{}, nil)
	response, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{Underlying: "BTC"})
	if err != nil {
		t.Fatalf("QueryOptionChain: %v", err)
	}
	if response.Data == nil || len(response.Data) != 0 {
		t.Fatalf("Data=%#v want non-nil empty slice", response.Data)
	}
}

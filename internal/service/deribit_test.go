package service

import (
	"context"
	"errors"
	"testing"
	"time"

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
		{Underlying: "BTC", Date: "08/13/2026"},
		{Underlying: "BTC", From: "2026-08-12", To: "2026-08-13"},
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

func TestDeribitServiceQueryHistoricalOptionChainMapsFiltersAndCaches(t *testing.T) {
	date := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	markIV := 62.5
	markPrice := 0.12
	bidPrice := 0.11
	askPrice := 0.13
	underlyingPrice := 118000.0
	openInterest := 42.0
	loadCalls := 0
	client := &stubDeribitClient{}
	service := NewDeribitService(client, cache.NewMemoryStore())
	service.historicalLoad = func(_ context.Context, underlying string, day time.Time) ([]dto.DeribitOptionChainContract, error) {
		loadCalls++
		if underlying != "BTC" || !day.Equal(date) {
			t.Fatalf("historical loader received %q at %s", underlying, day)
		}
		return []dto.DeribitOptionChainContract{
			{
				Contract:  dto.DeribitOptionContract{Ticker: "BTC-28AUG26-110000-P", UnderlyingTicker: "BTC", ContractType: "put", ExerciseStyle: "european", ExpirationDate: "2026-08-28", StrikePrice: 110000, BaseCurrency: "BTC", QuoteCurrency: "USD"},
				MarkPrice: &markPrice, BidPrice: &bidPrice, AskPrice: &askPrice, ImpliedVolatility: &markIV, OpenInterest: &openInterest,
				UnderlyingAsset: dto.DeribitUnderlyingAsset{Ticker: "BTC", Price: &underlyingPrice}, PremiumCurrency: "BTC", Timestamp: date.UnixMilli(),
			},
			{Contract: dto.DeribitOptionContract{Ticker: "BTC-28AUG26-90000-C", UnderlyingTicker: "BTC", ContractType: "call", ExerciseStyle: "european", ExpirationDate: "2026-08-28", StrikePrice: 90000, BaseCurrency: "BTC", QuoteCurrency: "USD"}},
		}, nil
	}

	putResponse, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{Underlying: "btc", Date: "2026-08-13", ContractType: "put"})
	if err != nil {
		t.Fatalf("QueryOptionChain historical put: %v", err)
	}
	if len(putResponse.Data) != 1 || putResponse.Data[0].Contract.Ticker != "BTC-28AUG26-110000-P" {
		t.Fatalf("unexpected historical response: %#v", putResponse.Data)
	}

	strike := 90000.0
	callResponse, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{Underlying: "BTC", Date: "2026-08-13", StrikePrice: &strike})
	if err != nil {
		t.Fatalf("QueryOptionChain historical strike: %v", err)
	}
	if len(callResponse.Data) != 1 || callResponse.Data[0].Contract.Ticker != "BTC-28AUG26-90000-C" {
		t.Fatalf("unexpected historical strike response: %#v", callResponse.Data)
	}
	if loadCalls != 1 {
		t.Fatalf("historical loads=%d want 1", loadCalls)
	}
	if client.calls != 0 {
		t.Fatalf("upstream calls=%d want 0", client.calls)
	}
}

func TestDeribitServiceQueryHistoricalOptionChainReturnsEmptySlice(t *testing.T) {
	service := NewDeribitService(&stubDeribitClient{}, nil)
	service.historicalLoad = func(context.Context, string, time.Time) ([]dto.DeribitOptionChainContract, error) {
		return nil, nil
	}

	response, err := service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{Underlying: "BTC", Date: "2026-08-13"})
	if err != nil {
		t.Fatalf("QueryOptionChain historical: %v", err)
	}
	if response.Data == nil || len(response.Data) != 0 {
		t.Fatalf("Data=%#v want non-nil empty slice", response.Data)
	}
}

func TestDeribitServiceQueryOptionChainHistoryIncludesEveryDayAndReusesCache(t *testing.T) {
	dayOne := time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC)
	dayTwo := dayOne.AddDate(0, 0, 1)
	loadCalls := make(map[string]int)
	service := NewDeribitService(&stubDeribitClient{}, cache.NewMemoryStore())
	service.historicalLoad = func(_ context.Context, underlying string, day time.Time) ([]dto.DeribitOptionChainContract, error) {
		loadCalls[day.Format("2006-01-02")]++
		if underlying != "BTC" {
			t.Fatalf("underlying=%q want BTC", underlying)
		}
		if day.Equal(dayTwo) {
			return nil, nil
		}
		return []dto.DeribitOptionChainContract{{
			Contract: dto.DeribitOptionContract{Ticker: "BTC-28AUG26-110000-P", ContractType: "put", ExpirationDate: "2026-08-28", StrikePrice: 110000},
		}}, nil
	}

	response, err := service.QueryOptionChainHistory(context.Background(), dto.DeribitOptionChainRequest{Underlying: "BTC", From: "2026-08-12", To: "2026-08-13", ContractType: "put"})
	if err != nil {
		t.Fatalf("QueryOptionChainHistory: %v", err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("snapshots=%d want 2", len(response.Data))
	}
	if response.Data[0].Date != "2026-08-12" || len(response.Data[0].Data) != 1 {
		t.Fatalf("unexpected first snapshot: %#v", response.Data[0])
	}
	if response.Data[1].Date != "2026-08-13" || response.Data[1].Data == nil || len(response.Data[1].Data) != 0 {
		t.Fatalf("unexpected empty second snapshot: %#v", response.Data[1])
	}

	_, err = service.QueryOptionChain(context.Background(), dto.DeribitOptionChainRequest{Underlying: "BTC", Date: "2026-08-12"})
	if err != nil {
		t.Fatalf("QueryOptionChain single day: %v", err)
	}
	if loadCalls["2026-08-12"] != 1 || loadCalls["2026-08-13"] != 1 {
		t.Fatalf("historical loads=%#v want one per day", loadCalls)
	}
}

func TestDeribitServiceQueryOptionChainHistoryValidation(t *testing.T) {
	service := NewDeribitService(&stubDeribitClient{}, nil)
	tests := []dto.DeribitOptionChainRequest{
		{Underlying: "BTC", To: "2026-08-13"},
		{Underlying: "BTC", From: "2026-08-12"},
		{Underlying: "BTC", From: "2026/08/12", To: "2026-08-13"},
		{Underlying: "BTC", From: "2026-08-14", To: "2026-08-13"},
		{Underlying: "BTC", Date: "2026-08-12", From: "2026-08-12", To: "2026-08-13"},
		{Underlying: "BTC", From: "2025-01-01", To: "2026-01-02"},
	}
	for _, request := range tests {
		_, err := service.QueryOptionChainHistory(context.Background(), request)
		var validationError *dto.ValidationError
		if !errors.As(err, &validationError) {
			t.Fatalf("request=%#v error=%T %v want ValidationError", request, err, err)
		}
	}
}

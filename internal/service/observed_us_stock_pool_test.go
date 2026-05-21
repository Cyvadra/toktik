package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/Cyvadra/toktik/internal/dto"
)

type stubObservedUSStockPoolScreener struct {
	responses map[int]*dto.ScreenUSTurnoverIntersectionResponse
	requests  []dto.ScreenUSTurnoverIntersectionRequest
	errAt     map[int]error
}

func (s *stubObservedUSStockPoolScreener) ScreenUSTurnoverIntersection(_ context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error) {
	s.requests = append(s.requests, req)
	if err := s.errAt[req.LookbackDays]; err != nil {
		return nil, err
	}
	if resp, ok := s.responses[req.LookbackDays]; ok {
		return resp, nil
	}
	return &dto.ScreenUSTurnoverIntersectionResponse{}, nil
}

func TestResolveObservedUSStockPool(t *testing.T) {
	screener := &stubObservedUSStockPoolScreener{
		responses: map[int]*dto.ScreenUSTurnoverIntersectionResponse{
			20:  {Data: []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "AAPL"}, {Underlying: "MSFT"}}},
			60:  {Data: []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "MSFT"}, {Underlying: "NVDA"}}},
			120: {Data: []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "BRK.B"}, {Underlying: "AAPL"}}},
		},
		errAt: map[int]error{},
	}

	got, err := ResolveObservedUSStockPool(context.Background(), screener)
	if err != nil {
		t.Fatalf("ResolveObservedUSStockPool() error = %v", err)
	}
	want := []string{"AAPL", "MSFT", "NVDA", "BRK.B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveObservedUSStockPool() = %#v, want %#v", got, want)
	}
	if len(screener.requests) != 3 {
		t.Fatalf("expected 3 screener requests, got %d", len(screener.requests))
	}
	for index, req := range screener.requests {
		if req.Limit != observedUSStockPoolTopLimit || !req.NonETFOnly || req.LookbackDays != observedUSStockPoolLookbackDays[index] {
			t.Fatalf("unexpected screener request %d: %+v", index, req)
		}
	}
}

func TestResolveObservedUSStockPoolPropagatesError(t *testing.T) {
	screener := &stubObservedUSStockPoolScreener{
		responses: map[int]*dto.ScreenUSTurnoverIntersectionResponse{},
		errAt:     map[int]error{60: fmt.Errorf("boom")},
	}

	if _, err := ResolveObservedUSStockPool(context.Background(), screener); err == nil {
		t.Fatalf("ResolveObservedUSStockPool() error = nil, want non-nil")
	}
}

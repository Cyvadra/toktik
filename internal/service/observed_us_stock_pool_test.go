package service

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
)

type stubObservedUSStockPoolScreener struct {
	mu        sync.Mutex
	responses map[int]*dto.ScreenUSTurnoverIntersectionResponse
	requests  []dto.ScreenUSTurnoverIntersectionRequest
	errAt     map[int]error
	requestCh chan dto.ScreenUSTurnoverIntersectionRequest
}

func (s *stubObservedUSStockPoolScreener) ScreenUSTurnoverIntersection(_ context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	requestCh := s.requestCh
	errAt := s.errAt[req.LookbackDays]
	resp, ok := s.responses[req.LookbackDays]
	s.mu.Unlock()
	if requestCh != nil {
		requestCh <- req
	}
	if errAt != nil {
		return nil, errAt
	}
	if ok {
		return resp, nil
	}
	return &dto.ScreenUSTurnoverIntersectionResponse{}, nil
}

func (s *stubObservedUSStockPoolScreener) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *stubObservedUSStockPoolScreener) requestAt(index int) dto.ScreenUSTurnoverIntersectionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[index]
}

func TestResolveObservedUSStockPool(t *testing.T) {
	screener := &stubObservedUSStockPoolScreener{
		responses: map[int]*dto.ScreenUSTurnoverIntersectionResponse{
			7:   {Data: []dto.ScreenedUSTurnoverIntersectionRow{{Underlying: "TSLA"}, {Underlying: "AAPL"}}},
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
	want := []string{"TSLA", "AAPL", "MSFT", "NVDA", "BRK.B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveObservedUSStockPool() = %#v, want %#v", got, want)
	}
	if len(screener.requests) != 4 {
		t.Fatalf("expected 4 screener requests, got %d", len(screener.requests))
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

func TestUSTurnoverIntersectionCacheRefresherRunsImmediatelyAndStopsOnCancel(t *testing.T) {
	screener := &stubObservedUSStockPoolScreener{
		responses: map[int]*dto.ScreenUSTurnoverIntersectionResponse{},
		errAt:     map[int]error{},
		requestCh: make(chan dto.ScreenUSTurnoverIntersectionRequest, 16),
	}
	store := cache.NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	refresher := StartUSTurnoverIntersectionCacheRefresher(ctx, nil, screener, store, true, 10*time.Millisecond, 5*time.Millisecond, time.Second)

	for i := 0; i < len(observedUSStockPoolLookbackDays)*2; i++ {
		select {
		case <-screener.requestCh:
		case <-time.After(500 * time.Millisecond):
			cancel()
			refresher.Wait()
			t.Fatalf("timed out waiting for refresh call %d", i+1)
		}
	}

	cancel()
	refresher.Wait()
	countAfterStop := screener.requestCount()
	time.Sleep(30 * time.Millisecond)
	if got := screener.requestCount(); got != countAfterStop {
		t.Fatalf("expected refresher to stop after cancel, got %d requests, want %d", got, countAfterStop)
	}
	if countAfterStop < len(observedUSStockPoolLookbackDays)*2 {
		t.Fatalf("expected at least two refresh cycles, got %d requests", countAfterStop)
	}
	for index := 0; index < countAfterStop; index++ {
		req := screener.requestAt(index)
		if req.Limit != observedUSStockPoolTopLimit || !req.NonETFOnly {
			t.Fatalf("unexpected refresher request %d: %+v", index, req)
		}
	}
}

func TestUSTurnoverIntersectionCacheRefresherSkipsStartupWarmupWithinCooldown(t *testing.T) {
	store := cache.NewMemoryStore()
	if err := markUSTurnoverIntersectionWarmupSuccess(context.Background(), store, true, time.Hour); err != nil {
		t.Fatalf("markUSTurnoverIntersectionWarmupSuccess() error = %v", err)
	}
	screener := &stubObservedUSStockPoolScreener{
		responses: map[int]*dto.ScreenUSTurnoverIntersectionResponse{},
		errAt:     map[int]error{},
		requestCh: make(chan dto.ScreenUSTurnoverIntersectionRequest, 4),
	}
	ctx, cancel := context.WithCancel(context.Background())
	refresher := StartUSTurnoverIntersectionCacheRefresher(ctx, nil, screener, store, true, time.Hour, time.Hour, time.Second)

	select {
	case req := <-screener.requestCh:
		cancel()
		refresher.Wait()
		t.Fatalf("expected startup warmup to be skipped, got request %+v", req)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	refresher.Wait()
	if got := screener.requestCount(); got != 0 {
		t.Fatalf("expected no warmup requests during cooldown, got %d", got)
	}
}

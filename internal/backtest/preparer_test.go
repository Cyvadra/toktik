package backtest

import (
	"context"
	"sync"
	"testing"
	"time"
)

type concurrencyTrackingFeed struct {
	mu        sync.Mutex
	active    int
	maxActive int
	release   chan struct{}
}

func (*concurrencyTrackingFeed) Fields() []string { return []string{"close"} }

func (f *concurrencyTrackingFeed) Load(ctx context.Context, req DataRequest) (*DataSet, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()

	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return &DataSet{
		Timestamps: []time.Time{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		Columns:    map[string][]float64{"close": {100}},
		Len:        1,
	}, nil
}

type manySecuritiesStrategy struct {
	count int
}

func (s manySecuritiesStrategy) Name() string { return "many-securities" }

func (s manySecuritiesStrategy) Init(ctx *SetupContext) error {
	for index := 0; index < s.count; index++ {
		ctx.AddSecurity("secondary", string(rune('A'+index)), "1d")
	}
	return nil
}

func (manySecuritiesStrategy) OnBar(*BarContext) {}

type fixedDataFeed struct{}

func (fixedDataFeed) Fields() []string { return []string{"close"} }

func (fixedDataFeed) Load(context.Context, DataRequest) (*DataSet, error) {
	return &DataSet{
		Timestamps: []time.Time{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		Columns:    map[string][]float64{"close": {100}},
		Len:        1,
	}, nil
}

func TestDataPreparerBoundsSecondaryLoadConcurrency(t *testing.T) {
	feed := &concurrencyTrackingFeed{release: make(chan struct{}, maxPrepareLoadConcurrency+1)}
	preparer := DataPreparer{
		feeds: map[string]DataFeed{
			"primary":   fixedDataFeed{},
			"secondary": feed,
		},
		dsCache: make(map[DataRequest]*DataSet),
	}

	done := make(chan error, 1)
	go func() {
		_, err := preparer.Prepare(
			context.Background(),
			"primary",
			"SPY",
			"1d",
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
			manySecuritiesStrategy{count: maxPrepareLoadConcurrency + 1},
			nil,
		)
		done <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		feed.mu.Lock()
		active := feed.active
		feed.mu.Unlock()
		if active == maxPrepareLoadConcurrency {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d concurrent loads; got %d", maxPrepareLoadConcurrency, active)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	for index := 0; index < maxPrepareLoadConcurrency+1; index++ {
		feed.release <- struct{}{}
	}
	if err := <-done; err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if feed.maxActive != maxPrepareLoadConcurrency {
		t.Fatalf("max concurrent loads = %d, want %d", feed.maxActive, maxPrepareLoadConcurrency)
	}
}

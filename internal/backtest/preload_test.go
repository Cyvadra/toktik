package backtest

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

type preloadStrategy struct{}

func (s *preloadStrategy) Name() string { return "preload" }

func (s *preloadStrategy) Init(ctx *SetupContext) error {
	ctx.Register("ma3", SMA("close", 3))
	return nil
}

func (s *preloadStrategy) Preload(ctx *PreloadContext) error {
	primary := ctx.Primary()
	closeCol, err := primary.RequireColumn("close")
	if err != nil {
		return err
	}
	ma3, err := primary.RequireColumn("ma3")
	if err != nil {
		return err
	}

	out := make([]float64, len(closeCol))
	for i := range closeCol {
		if i < 2 {
			out[i] = closeCol[i]
			continue
		}
		out[i] = closeCol[i] - ma3[i]
	}
	return primary.SetColumn("pre_close_minus_ma3", out)
}

func (s *preloadStrategy) OnBar(_ *BarContext) {}

type preloadErrStrategy struct{}

func (s *preloadErrStrategy) Name() string               { return "preload-err" }
func (s *preloadErrStrategy) Init(_ *SetupContext) error { return nil }
func (s *preloadErrStrategy) OnBar(_ *BarContext)        {}
func (s *preloadErrStrategy) Preload(_ *PreloadContext) error {
	return errors.New("boom")
}

type preloadMultiQuantileStrategy struct{}

func (s *preloadMultiQuantileStrategy) Name() string { return "preload-mq" }
func (s *preloadMultiQuantileStrategy) Init(_ *SetupContext) error {
	return nil
}
func (s *preloadMultiQuantileStrategy) OnBar(_ *BarContext) {}
func (s *preloadMultiQuantileStrategy) Preload(ctx *PreloadContext) error {
	return ctx.Primary().MultiQuantile("high", 100, map[string]float64{
		"high_q80_100": 0.8,
		"high_q50_100": 0.5,
	})
}

type warmupStrategy struct{}

func (s *warmupStrategy) Name() string { return "warmup" }
func (s *warmupStrategy) Init(ctx *SetupContext) error {
	ctx.SetWarmup(3 * time.Hour)
	ctx.Register("ma3", SMA("close", 3))
	return nil
}
func (s *warmupStrategy) OnBar(_ *BarContext) {}

func TestPrepareRunsStrategyPreload(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	prepared, err := engine.Prepare(context.Background(), "test", "TEST", "1h", from, to, &preloadStrategy{}, nil)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	col := prepared.PrimaryDS.Column("pre_close_minus_ma3")
	if col == nil {
		t.Fatalf("expected preloaded column to exist")
	}
	if len(col) != prepared.PrimaryDS.Len {
		t.Fatalf("unexpected preloaded column length: %d", len(col))
	}

	closeCol := prepared.PrimaryDS.Column("close")
	ma3 := prepared.PrimaryDS.Column("ma3")
	if col[10] != closeCol[10]-ma3[10] {
		t.Fatalf("unexpected preloaded value at index 10: got %v want %v", col[10], closeCol[10]-ma3[10])
	}
}

func TestPreparePropagatesPreloadError(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	_, err := engine.Prepare(context.Background(), "test", "TEST", "1h", from, to, &preloadErrStrategy{}, nil)
	if err == nil {
		t.Fatalf("expected preload error")
	}
	if !strings.Contains(err.Error(), "strategy preload") {
		t.Fatalf("expected wrapped preload error, got %v", err)
	}
}

func TestPreparePreloadMultiQuantile(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	prepared, err := engine.Prepare(context.Background(), "test", "TEST", "1h", from, to, &preloadMultiQuantileStrategy{}, nil)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	q80 := prepared.PrimaryDS.Column("high_q80_100")
	q50 := prepared.PrimaryDS.Column("high_q50_100")
	if q80 == nil || q50 == nil {
		t.Fatalf("expected multi-quantile preload columns")
	}

	for i := 0; i < 99; i++ {
		if !math.IsNaN(q80[i]) || !math.IsNaN(q50[i]) {
			t.Fatalf("expected warmup NaN at index %d", i)
		}
	}

	if q80[99] <= q50[99] {
		t.Fatalf("expected q80 > q50 at first valid bar: q80=%v q50=%v", q80[99], q50[99])
	}
}

func TestPrepareWarmupLoadsHistoryButTrimsVisibleWindow(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	engine.RegisterDataFeed("test", &stubDataFeed{
		fields: []string{"open", "high", "low", "close", "volume"},
	})

	from := time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	prepared, err := engine.Prepare(context.Background(), "test", "TEST", "1h", from, to, &warmupStrategy{}, nil)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	if got := prepared.PrimaryDS.Timestamps[0]; !got.Equal(from) {
		t.Fatalf("unexpected trimmed start time: got %v want %v", got, from)
	}

	ma3 := prepared.PrimaryDS.Column("ma3")
	if ma3 == nil {
		t.Fatalf("expected warmup-prepared indicator column")
	}
	if math.IsNaN(ma3[0]) {
		t.Fatalf("expected first visible bar to use warmup history, got NaN")
	}

	if prepared.PrimaryDS.Len != 97 {
		t.Fatalf("unexpected trimmed primary length: got %d want %d", prepared.PrimaryDS.Len, 97)
	}
	if len(prepared.AlignMaps) != 1 || prepared.AlignMaps[0] != nil {
		t.Fatalf("unexpected primary align map after trim: %#v", prepared.AlignMaps)
	}
}

type preloadAlignmentStrategy struct {
	secRef SecurityRef
}

func (s *preloadAlignmentStrategy) Name() string { return "preload-alignment" }
func (s *preloadAlignmentStrategy) Init(ctx *SetupContext) error {
	s.secRef = ctx.AddSecurity("test", "TEST2", "1h")
	return nil
}
func (s *preloadAlignmentStrategy) OnBar(_ *BarContext) {}
func (s *preloadAlignmentStrategy) Preload(ctx *PreloadContext) error {
	aligned, err := ctx.ColumnAlignedToPrimary(s.secRef, "close")
	if err != nil {
		return err
	}
	return ctx.Primary().SetColumn("sec_close_aligned", aligned)
}

func TestPreloadAlignedColumnToPrimary(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 10000})
	feed := &stubDataFeed{fields: []string{"open", "high", "low", "close", "volume"}}
	engine.RegisterDataFeed("test", feed)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	strategy := &preloadAlignmentStrategy{}
	prepared, err := engine.Prepare(context.Background(), "test", "TEST", "1h", from, to, strategy, nil)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	aligned := prepared.PrimaryDS.Column("sec_close_aligned")
	if aligned == nil {
		t.Fatalf("expected aligned preload column")
	}
	if len(aligned) != prepared.PrimaryDS.Len {
		t.Fatalf("unexpected aligned column length: got %d want %d", len(aligned), prepared.PrimaryDS.Len)
	}
	if math.IsNaN(aligned[20]) {
		t.Fatalf("expected aligned value at index 20")
	}
}

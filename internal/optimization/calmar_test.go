package optimization

import (
	"math"
	"testing"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestCalmarRatioZeroMaxDrawdown(t *testing.T) {
	// Strategy with positive annualized return and no drawdown → should rank highest
	r := &backtest.Result{AnnualizedReturn: 0.15, MaxDrawdown: 0}
	v := extractMetric(r, MetricCalmar)
	if !math.IsInf(v, 1) {
		t.Fatalf("expected +Inf Calmar when MaxDrawdown=0 and positive return, got %v", v)
	}

	// Strategy with zero return and no drawdown → neither winner nor loser
	r2 := &backtest.Result{AnnualizedReturn: 0, MaxDrawdown: 0}
	v2 := extractMetric(r2, MetricCalmar)
	if v2 != 0 {
		t.Fatalf("expected 0 Calmar when MaxDrawdown=0 and zero return, got %v", v2)
	}

	// Normal case: Calmar = annualized_return / max_drawdown
	r3 := &backtest.Result{AnnualizedReturn: 0.30, MaxDrawdown: 0.10}
	v3 := extractMetric(r3, MetricCalmar)
	if math.Abs(v3-3.0) > 1e-10 {
		t.Fatalf("expected Calmar 3.0, got %v", v3)
	}
}

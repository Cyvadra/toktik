package dualspreadsvol

import (
	"math"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
)

func TestVolStdRatioRequiresForty12hBars(t *testing.T) {
	closeSeries := make([]float64, 50)
	for i := range closeSeries {
		closeSeries[i] = float64(i + 1)
	}

	stdSeries := optutil.RollingStdDev(closeSeries, volStdPeriod)
	smaSeries := backtest.SMA("std", volStdSMAPeriod).Compute(map[string][]float64{"std": stdSeries})
	ratioSeries := volStdRatioIndicator("std", "sma").Compute(map[string][]float64{
		"std": stdSeries,
		"sma": smaSeries,
	})

	if !math.IsNaN(stdSeries[volStdPeriod-2]) {
		t.Fatalf("stdSeries[%d] = %v, want NaN before 20 bars complete", volStdPeriod-2, stdSeries[volStdPeriod-2])
	}
	if math.IsNaN(stdSeries[volStdPeriod-1]) {
		t.Fatalf("stdSeries[%d] is NaN, want first valid std(20)", volStdPeriod-1)
	}
	if !math.IsNaN(ratioSeries[volStdPeriod+volStdSMAPeriod-3]) {
		t.Fatalf("ratioSeries[%d] = %v, want NaN before sma(std(20),20) is fully available", volStdPeriod+volStdSMAPeriod-3, ratioSeries[volStdPeriod+volStdSMAPeriod-3])
	}
	if math.IsNaN(ratioSeries[volStdPeriod+volStdSMAPeriod-2]) {
		t.Fatalf("ratioSeries[%d] is NaN, want first valid vol_std after 40 12h bars", volStdPeriod+volStdSMAPeriod-2)
	}

	want := stdSeries[volStdPeriod+volStdSMAPeriod-2] / smaSeries[volStdPeriod+volStdSMAPeriod-2]
	if diff := math.Abs(ratioSeries[volStdPeriod+volStdSMAPeriod-2] - want); diff > 1e-12 {
		t.Fatalf("first valid vol_std = %.12f, want %.12f", ratioSeries[volStdPeriod+volStdSMAPeriod-2], want)
	}
}

func TestSelectEligibleCallsPrefersPrimaryWindow(t *testing.T) {
	now := time.Date(2023, 6, 21, 0, 0, 0, 0, time.UTC)
	chain := backtest.NewOptionsChain([]backtest.OptionContract{
		{Type: backtest.Call, Expiration: now.Add(28 * 24 * time.Hour)},
		{Type: backtest.Call, Expiration: now.Add(42 * 24 * time.Hour)},
		{Type: backtest.Call, Expiration: now.Add(60 * 24 * time.Hour)},
	}, now)

	selected, targetDTE, usedFallback := selectEligibleCalls(chain, selectDTEWindow(60))
	if usedFallback {
		t.Fatal("expected primary window selection, got fallback")
	}
	if targetDTE != highIVTargetDTE {
		t.Fatalf("targetDTE = %d, want %d", targetDTE, highIVTargetDTE)
	}
	if selected.Len() != 1 {
		t.Fatalf("selected len = %d, want 1", selected.Len())
	}
	if got := selected.Contracts()[0].DaysToExpiry(now); got != 28 {
		t.Fatalf("selected DTE = %.0f, want 28", got)
	}
}

func TestSelectEligibleCallsUsesHighIVFallbackWhenPrimaryEmpty(t *testing.T) {
	now := time.Date(2023, 6, 21, 0, 0, 0, 0, time.UTC)
	chain := backtest.NewOptionsChain([]backtest.OptionContract{
		{Type: backtest.Call, Expiration: now.Add(10 * 24 * time.Hour)},
		{Type: backtest.Call, Expiration: now.Add(42 * 24 * time.Hour)},
		{Type: backtest.Call, Expiration: now.Add(60 * 24 * time.Hour)},
	}, now)

	selected, targetDTE, usedFallback := selectEligibleCalls(chain, selectDTEWindow(60))
	if !usedFallback {
		t.Fatal("expected high-IV fallback selection")
	}
	if targetDTE != highIVTargetDTE {
		t.Fatalf("targetDTE = %d, want %d", targetDTE, highIVTargetDTE)
	}
	expiries := candidateExpiries(selected.Contracts(), now, targetDTE)
	if len(expiries) == 0 {
		t.Fatal("expected candidate expiries for fallback selection")
	}
	if got := expiries[0].Sub(now).Hours() / 24; got != 42 {
		t.Fatalf("first fallback DTE = %.0f, want 42", got)
	}
}

func TestSelectEligibleCallsUsesLowIVFallbackTarget40(t *testing.T) {
	now := time.Date(2023, 6, 21, 0, 0, 0, 0, time.UTC)
	chain := backtest.NewOptionsChain([]backtest.OptionContract{
		{Type: backtest.Call, Expiration: now.Add(20 * 24 * time.Hour)},
		{Type: backtest.Call, Expiration: now.Add(52 * 24 * time.Hour)},
		{Type: backtest.Call, Expiration: now.Add(70 * 24 * time.Hour)},
	}, now)

	selected, targetDTE, usedFallback := selectEligibleCalls(chain, selectDTEWindow(55))
	if !usedFallback {
		t.Fatal("expected low-IV fallback selection")
	}
	if targetDTE != 40 {
		t.Fatalf("targetDTE = %d, want 40", targetDTE)
	}
	expiries := candidateExpiries(selected.Contracts(), now, targetDTE)
	if len(expiries) == 0 {
		t.Fatal("expected candidate expiries for fallback selection")
	}
	if got := expiries[0].Sub(now).Hours() / 24; got != 52 {
		t.Fatalf("first fallback DTE = %.0f, want 52", got)
	}
}

func TestBuildHTFProgressUsesPrimaryCloseTime(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	primaryTimes := make([]time.Time, 14)
	for i := range primaryTimes {
		primaryTimes[i] = base.Add(time.Duration(i) * time.Hour)
	}
	higherTimes := []time.Time{base, base.Add(12 * time.Hour), base.Add(24 * time.Hour)}

	indices, weights, err := buildHTFProgress(primaryTimes, "1h", higherTimes, "12h")
	if err != nil {
		t.Fatalf("buildHTFProgress failed: %v", err)
	}
	if indices[0] != 0 || math.Abs(weights[0]-1.0/12.0) > 1e-12 {
		t.Fatalf("bar0 = (%d, %.6f), want (0, %.6f)", indices[0], weights[0], 1.0/12.0)
	}
	if indices[10] != 0 || math.Abs(weights[10]-11.0/12.0) > 1e-12 {
		t.Fatalf("bar10 = (%d, %.6f), want (0, %.6f)", indices[10], weights[10], 11.0/12.0)
	}
	if indices[11] != 0 || math.Abs(weights[11]-1) > 1e-12 {
		t.Fatalf("bar11 = (%d, %.6f), want (0, 1)", indices[11], weights[11])
	}
	if indices[12] != 1 || math.Abs(weights[12]-1.0/12.0) > 1e-12 {
		t.Fatalf("bar12 = (%d, %.6f), want (1, %.6f)", indices[12], weights[12], 1.0/12.0)
	}
}

func TestEstimateVolStdRatioUsesEstimatedCurrentClose(t *testing.T) {
	confirmedClose := make([]float64, 40)
	for i := range confirmedClose {
		confirmedClose[i] = float64(i + 1)
	}
	confirmedStd := optutil.RollingStdDev(confirmedClose, volStdPeriod)
	estimated := estimateVolStdRatio(40, 50, confirmedClose, confirmedStd)
	if math.IsNaN(estimated) {
		t.Fatal("estimated ratio is NaN")
	}
	seriesWithEstimate := append(append([]float64(nil), confirmedClose...), 50)
	stdWithEstimate := optutil.RollingStdDev(seriesWithEstimate, volStdPeriod)
	smaWithEstimate := backtest.SMA("std", volStdSMAPeriod).Compute(map[string][]float64{"std": stdWithEstimate})
	want := stdWithEstimate[len(stdWithEstimate)-1] / smaWithEstimate[len(smaWithEstimate)-1]
	if diff := math.Abs(estimated - want); diff > 1e-12 {
		t.Fatalf("estimated ratio = %.12f, want %.12f", estimated, want)
	}
}

func TestBlendEstimatedValue(t *testing.T) {
	got := blendEstimatedValue(20, 80, 0.25)
	if math.Abs(got-35) > 1e-12 {
		t.Fatalf("blendEstimatedValue = %.12f, want 35", got)
	}
	if got := blendEstimatedValue(math.NaN(), 80, 0.25); got != 80 {
		t.Fatalf("blend with NaN confirmed = %.12f, want 80", got)
	}
}

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

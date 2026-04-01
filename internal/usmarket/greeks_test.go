package usmarket

import (
	"math"
	"testing"
	"time"
)

func TestCalculateOptionGreeksCall(t *testing.T) {
	start := time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC)
	expiration := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	timeToExpiry := timeToExpiryYears(start, expiration)
	price := blackScholesPrice(100, 100, "C", timeToExpiry, 0.05, 0, 0.2)

	greeks := calculateOptionGreeks(100, 100, price, "C", start, expiration, GreeksConfig{RiskFreeRate: 0.05})

	assertNear(t, greeks.ImpliedVolatility, 0.2, 2e-3, "implied volatility")
	assertBetween(t, greeks.Delta, 0, 1, "delta")
	assertPositive(t, greeks.Gamma, "gamma")
	assertPositive(t, greeks.Vega, "vega")
	assertNegative(t, greeks.Theta, "theta")
	assertPositive(t, greeks.Rho, "rho")
}

func TestCalculateOptionGreeksPut(t *testing.T) {
	start := time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC)
	expiration := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	timeToExpiry := timeToExpiryYears(start, expiration)
	callPrice := blackScholesPrice(100, 100, "C", timeToExpiry, 0.05, 0, 0.2)
	putPrice := blackScholesPrice(100, 100, "P", timeToExpiry, 0.05, 0, 0.2)
	callGreeks := calculateOptionGreeks(100, 100, callPrice, "C", start, expiration, GreeksConfig{RiskFreeRate: 0.05})
	greeks := calculateOptionGreeks(100, 100, putPrice, "P", start, expiration, GreeksConfig{RiskFreeRate: 0.05})

	assertNear(t, greeks.ImpliedVolatility, 0.2, 2e-3, "implied volatility")
	assertBetween(t, greeks.Delta, -1, 0, "delta")
	assertPositive(t, greeks.Gamma, "gamma")
	assertPositive(t, greeks.Vega, "vega")
	assertNegative(t, greeks.Theta, "theta")
	assertNegative(t, greeks.Rho, "rho")
	assertNear(t, callGreeks.Gamma, greeks.Gamma, 1e-6, "call/put gamma parity")
	assertNear(t, callGreeks.Vega, greeks.Vega, 1e-6, "call/put vega parity")
	assertNear(t, callGreeks.Delta-greeks.Delta, 1.0, 2e-3, "delta parity")
}

func TestEnrichOptionBarsWithGreeks(t *testing.T) {
	bars := make(chan OptionBar1m, 1)
	bars <- OptionBar1m{
		Timestamp:  time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC),
		Symbol:     "O:AAPL240201C00100000",
		Underlying: "AAPL",
		OptionType: "C",
		Expiration: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		Strike:     100,
		Close:      2.4933768,
	}
	close(bars)

	stockCloses := stockCloseSeries{
		"AAPL": {
			{timestamp: time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC).Unix(), close: 100},
		},
	}

	enriched, errPtr := EnrichOptionBarsWithGreeks(bars, stockCloses, GreeksConfig{RiskFreeRate: 0.05})
	var bar OptionBar1m
	for enrichedBar := range enriched {
		bar = enrichedBar
	}
	if *errPtr != nil {
		t.Fatalf("unexpected enrich error: %v", *errPtr)
	}
	if bar.UnderlyingClose != 100 {
		t.Fatalf("underlying close: got %v, want 100", bar.UnderlyingClose)
	}
	if math.IsNaN(float64(bar.Delta)) || math.IsNaN(float64(bar.Gamma)) {
		t.Fatalf("expected finite greeks, got delta=%v gamma=%v", bar.Delta, bar.Gamma)
	}
}

func TestEnrichOptionBarsWithGreeksMissingStockClose(t *testing.T) {
	bars := make(chan OptionBar1m, 1)
	bars <- OptionBar1m{
		Timestamp:  time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC),
		Symbol:     "O:SPX240201C05000000",
		Underlying: "SPX",
		OptionType: "C",
		Expiration: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		Strike:     5000,
		Close:      12.5,
	}
	close(bars)

	enriched, errPtr := EnrichOptionBarsWithGreeks(bars, nil, GreeksConfig{RiskFreeRate: 0.05})
	var bar OptionBar1m
	for enrichedBar := range enriched {
		bar = enrichedBar
	}

	if *errPtr == nil {
		t.Fatal("expected missing stock close warning")
	}
	if !math.IsNaN(float64(bar.UnderlyingClose)) {
		t.Fatalf("expected NaN underlying close, got %v", bar.UnderlyingClose)
	}
	if !math.IsNaN(float64(bar.ImpliedVolatility)) || !math.IsNaN(float64(bar.Delta)) || !math.IsNaN(float64(bar.Gamma)) {
		t.Fatalf("expected NaN greeks, got iv=%v delta=%v gamma=%v", bar.ImpliedVolatility, bar.Delta, bar.Gamma)
	}
}

func TestStockCloseSeriesLookupUsesLatestPriorClose(t *testing.T) {
	series := stockCloseSeries{
		"AAPL": {
			{timestamp: time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC).Unix(), close: 100},
			{timestamp: time.Date(2024, 1, 2, 15, 33, 0, 0, time.UTC).Unix(), close: 101},
		},
	}

	closeAt1532, ok := series.Lookup("AAPL", time.Date(2024, 1, 2, 15, 32, 0, 0, time.UTC))
	if !ok {
		t.Fatal("expected prior close lookup to succeed")
	}
	if closeAt1532 != 100 {
		t.Fatalf("got %v, want 100", closeAt1532)
	}

	if _, ok := series.Lookup("AAPL", time.Date(2024, 1, 2, 15, 29, 0, 0, time.UTC)); ok {
		t.Fatal("expected lookup before first close to fail")
	}
}

func TestCalculateOptionGreeksClampsSlightlyBelowIntrinsicValue(t *testing.T) {
	start := time.Date(2025, 9, 3, 19, 31, 0, 0, time.UTC)
	expiration := time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC)
	spot := 237.375
	strike := 110.0
	price := 127.35

	greeks := calculateOptionGreeks(spot, strike, price, "C", start, expiration, GreeksConfig{RiskFreeRate: 0.05})

	if math.IsNaN(greeks.ImpliedVolatility) {
		t.Fatal("expected finite implied volatility after boundary clamp")
	}
	if math.IsNaN(greeks.Delta) || greeks.Delta < 0 || greeks.Delta > 1 {
		t.Fatalf("delta: got %v, want within [0, 1]", greeks.Delta)
	}
	if math.IsNaN(greeks.Gamma) || greeks.Gamma < 0 {
		t.Fatalf("gamma: got %v, want >= 0", greeks.Gamma)
	}
	if math.IsNaN(greeks.Vega) || greeks.Vega < 0 {
		t.Fatalf("vega: got %v, want >= 0", greeks.Vega)
	}
}

func TestCalculateOptionGreeksRejectsMateriallyInvalidPrice(t *testing.T) {
	start := time.Date(2025, 9, 3, 19, 31, 0, 0, time.UTC)
	expiration := time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC)
	spot := 237.375
	strike := 110.0
	price := 120.0

	greeks := calculateOptionGreeks(spot, strike, price, "C", start, expiration, GreeksConfig{RiskFreeRate: 0.05})

	if !math.IsNaN(greeks.ImpliedVolatility) {
		t.Fatalf("expected NaN implied volatility for materially invalid price, got %v", greeks.ImpliedVolatility)
	}
	if !math.IsNaN(greeks.Delta) {
		t.Fatalf("expected NaN delta for materially invalid price, got %v", greeks.Delta)
	}
}

func assertNear(t *testing.T, got, want, tolerance float64, label string) {
	t.Helper()
	if math.IsNaN(got) {
		t.Fatalf("%s: got NaN", label)
	}
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.6f, want %.6f (tol %.6f)", label, got, want, tolerance)
	}
}

func assertBetween(t *testing.T, got, min, max float64, label string) {
	t.Helper()
	if math.IsNaN(got) || got <= min || got >= max {
		t.Fatalf("%s: got %.6f, want within (%.6f, %.6f)", label, got, min, max)
	}
}

func assertPositive(t *testing.T, got float64, label string) {
	t.Helper()
	if math.IsNaN(got) || got <= 0 {
		t.Fatalf("%s: got %.6f, want > 0", label, got)
	}
}

func assertNegative(t *testing.T, got float64, label string) {
	t.Helper()
	if math.IsNaN(got) || got >= 0 {
		t.Fatalf("%s: got %.6f, want < 0", label, got)
	}
}

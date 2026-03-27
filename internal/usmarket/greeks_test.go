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

	stockCloses := map[stockCloseKey]float64{
		newStockCloseKey("AAPL", time.Date(2024, 1, 2, 15, 30, 0, 0, time.UTC)): 100,
	}

	enriched, errPtr := EnrichOptionBarsWithGreeks(bars, stockCloses, GreeksConfig{RiskFreeRate: 0.05})
	bar := <-enriched
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

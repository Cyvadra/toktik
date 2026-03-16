package thetadata

import (
	"fmt"
	"math"
	"sort"
)

// Black-76 model for computing option prices and Greeks on forward prices.
//
// C = DF * [F*N(d1) - K*N(d2)]
// P = DF * [K*N(-d2) - F*N(-d1)]
// d1 = [ln(F/K) + σ²T/2] / (σ√T)
// d2 = d1 - σ√T
//
// Where:
//   F  = forward price
//   K  = strike price
//   T  = time to expiry in years
//   σ  = implied volatility
//   DF = discount factor = exp(-r*T)
//   r  = risk-free rate

const (
	ivMaxIter  = 100
	ivTol      = 1e-8
	ivMinSigma = 0.001
	ivMaxSigma = 10.0
)

// normalCDF returns the cumulative distribution function of standard normal.
func normalCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// normalPDF returns the probability density function of standard normal.
func normalPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}

// black76Price computes the Black-76 option price.
// isCall: true for call, false for put.
func black76Price(F, K, T, sigma, DF float64, isCall bool) float64 {
	if T <= 0 || sigma <= 0 {
		// At or past expiry: intrinsic value
		if isCall {
			return DF * math.Max(F-K, 0)
		}
		return DF * math.Max(K-F, 0)
	}

	sqrtT := math.Sqrt(T)
	d1 := (math.Log(F/K) + 0.5*sigma*sigma*T) / (sigma * sqrtT)
	d2 := d1 - sigma*sqrtT

	if isCall {
		return DF * (F*normalCDF(d1) - K*normalCDF(d2))
	}
	return DF * (K*normalCDF(-d2) - F*normalCDF(-d1))
}

// ImpliedVol solves for the implied volatility using bisection.
// marketPrice: observed option mid price
// F, K, T, DF: forward price, strike, time to expiry (years), discount factor
// isCall: true for call, false for put
// Returns the implied vol, or error if no solution found.
func ImpliedVol(marketPrice, F, K, T, DF float64, isCall bool) (float64, error) {
	if T <= 0 {
		return 0, fmt.Errorf("T <= 0")
	}
	if marketPrice <= 0 {
		return 0, fmt.Errorf("market price <= 0")
	}

	// Check bounds
	intrinsic := DF * math.Max(F-K, 0)
	if !isCall {
		intrinsic = DF * math.Max(K-F, 0)
	}
	if marketPrice < intrinsic-ivTol {
		return 0, fmt.Errorf("market price %.4f below intrinsic %.4f", marketPrice, intrinsic)
	}

	lo, hi := ivMinSigma, ivMaxSigma
	for i := 0; i < ivMaxIter; i++ {
		mid := (lo + hi) / 2
		price := black76Price(F, K, T, mid, DF, isCall)
		if math.Abs(price-marketPrice) < ivTol {
			return mid, nil
		}
		if price < marketPrice {
			lo = mid
		} else {
			hi = mid
		}
	}

	// Return best estimate even if not converged
	return (lo + hi) / 2, nil
}

// ComputeGreeks computes all first-order Greeks using the Black-76 model.
// F: forward price, K: strike, T: time to expiry (years),
// sigma: implied vol, DF: discount factor, r: risk-free rate, isCall: true for call.
func ComputeGreeks(F, K, T, sigma, DF, r float64, isCall bool) GreeksResult {
	if T <= 0 || sigma <= 0 {
		delta := 0.0
		if isCall && F > K {
			delta = DF
		} else if !isCall && F < K {
			delta = -DF
		}
		return GreeksResult{
			IV:    sigma,
			Delta: delta,
		}
	}

	sqrtT := math.Sqrt(T)
	d1 := (math.Log(F/K) + 0.5*sigma*sigma*T) / (sigma * sqrtT)
	d2 := d1 - sigma*sqrtT

	nd1 := normalCDF(d1)
	pd1 := normalPDF(d1)
	nd2 := normalCDF(d2)

	var delta, theta, rho float64

	if isCall {
		delta = DF * nd1
		theta = -DF*F*pd1*sigma/(2*sqrtT) - r*DF*F*nd1 + r*DF*K*nd2
		rho = K * T * DF * nd2
	} else {
		delta = DF * (nd1 - 1)
		theta = -DF*F*pd1*sigma/(2*sqrtT) + r*DF*F*(1-nd1) - r*DF*K*(1-nd2)
		rho = -K * T * DF * (1 - nd2)
	}

	gamma := DF * pd1 / (F * sigma * sqrtT)
	vega := DF * F * pd1 * sqrtT

	return GreeksResult{
		IV:    sigma,
		Delta: delta,
		Gamma: gamma,
		Vega:  vega / 100,  // Per 1% vol change
		Theta: theta / 365, // Per calendar day
		Rho:   rho / 100,   // Per 1% rate change
	}
}

// pairQuote holds matched call/put mid prices at a given strike.
type pairQuote struct {
	Strike  float64
	CallMid float64
	PutMid  float64
}

// ForwardFromParity estimates the forward price and discount factor
// using put-call parity across multiple strikes:
//
//	C(K) - P(K) = DF * (F - K)
//
// This is a linear regression: diff(K) = DF*F - DF*K
// i.e., y = a + b*K where a = DF*F, b = -DF
//
// callMids and putMids are maps from strike to mid price.
// T is time to expiry in years (used for rate computation).
// Returns ForwardInfo or error if insufficient data.
func ForwardFromParity(callMids, putMids map[float64]float64, T float64) (ForwardInfo, error) {
	// Find strikes present in both calls and puts
	var pairs []pairQuote
	for k, cmid := range callMids {
		if pmid, ok := putMids[k]; ok {
			if cmid > 0 && pmid > 0 {
				pairs = append(pairs, pairQuote{Strike: k, CallMid: cmid, PutMid: pmid})
			}
		}
	}

	if len(pairs) < 2 {
		return ForwardInfo{}, fmt.Errorf("need at least 2 C/P pairs, got %d", len(pairs))
	}

	// Sort by strike for consistency
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Strike < pairs[j].Strike })

	// Linear regression: y_i = a + b * x_i
	// where y_i = C(K_i) - P(K_i), x_i = K_i
	n := float64(len(pairs))
	var sumX, sumY, sumXX, sumXY float64
	for _, p := range pairs {
		y := p.CallMid - p.PutMid
		x := p.Strike
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}

	denom := n*sumXX - sumX*sumX
	if math.Abs(denom) < 1e-12 {
		return ForwardInfo{}, fmt.Errorf("degenerate regression (all strikes identical)")
	}

	b := (n*sumXY - sumX*sumY) / denom
	a := (sumY - b*sumX) / n

	// DF = -b, F = a / DF
	DF := -b
	if DF <= 0 || DF > 1.1 {
		// Fallback: use simple average
		DF = 1.0
	}
	F := a / DF

	r := 0.0
	if T > 0 && DF > 0 && DF < 1.1 {
		r = -math.Log(DF) / T
	}

	return ForwardInfo{
		Forward:        F,
		DiscountFactor: DF,
		Rate:           r,
	}, nil
}

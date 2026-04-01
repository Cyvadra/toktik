package usmarket

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	minImpliedVolatility = 1e-4
	maxImpliedVolatility = 5.0
	epsilonPrice         = 1e-8
	priceClampTolerance  = 0.10
	daysPerYear          = 365.0
)

var newYorkLocation = loadNewYorkLocation()

type stockCloseKey struct {
	symbol    string
	timestamp int64
}

type stockClosePoint struct {
	timestamp int64
	close     float64
}

type stockCloseSeries map[string][]stockClosePoint

func (s stockCloseSeries) Lookup(symbol string, timestamp time.Time) (float64, bool) {
	points := s[symbol]
	if len(points) == 0 {
		return 0, false
	}

	target := timestamp.UTC().Unix()
	idx := sort.Search(len(points), func(i int) bool {
		return points[i].timestamp > target
	}) - 1
	if idx < 0 {
		return 0, false
	}

	return points[idx].close, true
}

// GreeksConfig controls the assumptions used by the Black-Scholes calculation.
type GreeksConfig struct {
	RiskFreeRate  float64
	DividendYield float64
}

type optionGreeks struct {
	ImpliedVolatility float64
	Delta             float64
	Gamma             float64
	Vega              float64
	Theta             float64
	Rho               float64
}

func loadNewYorkLocation() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*60*60)
	}
	return loc
}

func newStockCloseKey(symbol string, timestamp time.Time) stockCloseKey {
	return stockCloseKey{symbol: symbol, timestamp: timestamp.UTC().Unix()}
}

// MissingStockSymbols returns the symbols missing from the loaded stock dataset.
func MissingStockSymbols(expected []string, seen map[string]struct{}) []string {
	missing := make([]string, 0)
	for _, symbol := range expected {
		if _, ok := seen[symbol]; !ok {
			missing = append(missing, symbol)
		}
	}
	sort.Strings(missing)
	return missing
}

// ValidateOptionStockCoverage loads available stock closes and returns any option
// underlyings that have no matching stock rows for the market date.
func ValidateOptionStockCoverage(ctx context.Context, conn driver.Conn, optionPath string, marketDate time.Time) (stockCloseSeries, []string, error) {
	underlyings, err := CollectOptionUnderlyings(optionPath)
	if err != nil {
		return nil, nil, fmt.Errorf("scan option underlyings: %w", err)
	}
	if len(underlyings) == 0 {
		return nil, nil, fmt.Errorf("no valid option tickers found in %s", optionPath)
	}

	stockCloses, seenSymbols, err := LoadStockCloseMap(ctx, conn, underlyings, marketDate)
	if err != nil {
		return nil, nil, err
	}

	return stockCloses, MissingStockSymbols(underlyings, seenSymbols), nil
}

// EnrichOptionBarsWithGreeks attaches stock closes and Black-Scholes greeks to each option bar.
// Missing stock closes are tolerated: affected bars are kept with NaN enrichment values
// and a summary warning is stored in *enrichErr after the channel is drained.
func EnrichOptionBarsWithGreeks(bars <-chan OptionBar1m, stockCloses stockCloseSeries, cfg GreeksConfig) (<-chan OptionBar1m, *error) {
	out := make(chan OptionBar1m, 8192)
	var enrichErr error

	go func() {
		defer close(out)

		missingBars := 0
		missingSymbols := make(map[string]struct{})

		for bar := range bars {
			underlyingClose, ok := stockCloses.Lookup(bar.Underlying, bar.Timestamp)
			if !ok {
				missingBars++
				missingSymbols[bar.Underlying] = struct{}{}
				applyMissingGreeks(&bar)
				out <- bar
				continue
			}

			greeks := calculateOptionGreeks(
				underlyingClose,
				bar.Strike,
				float64(bar.Close),
				bar.OptionType,
				bar.Timestamp,
				bar.Expiration,
				cfg,
			)

			bar.UnderlyingClose = float32(underlyingClose)
			bar.ImpliedVolatility = float32(greeks.ImpliedVolatility)
			bar.Delta = float32(greeks.Delta)
			bar.Gamma = float32(greeks.Gamma)
			bar.Vega = float32(greeks.Vega)
			bar.Theta = float32(greeks.Theta)
			bar.Rho = float32(greeks.Rho)
			out <- bar
		}

		if missingBars > 0 {
			symbols := make([]string, 0, len(missingSymbols))
			for symbol := range missingSymbols {
				symbols = append(symbols, symbol)
			}
			sort.Strings(symbols)
			enrichErr = fmt.Errorf("missing stock closes for %d option bars across %d underlyings: %v", missingBars, len(symbols), symbols)
		}
	}()

	return out, &enrichErr
}

func applyMissingGreeks(bar *OptionBar1m) {
	nan := float32(math.NaN())
	bar.UnderlyingClose = nan
	bar.ImpliedVolatility = nan
	bar.Delta = nan
	bar.Gamma = nan
	bar.Vega = nan
	bar.Theta = nan
	bar.Rho = nan
}

func calculateOptionGreeks(spot, strike, optionPrice float64, optionType string, timestamp, expiration time.Time, cfg GreeksConfig) optionGreeks {
	if spot <= 0 || strike <= 0 || optionPrice <= 0 {
		return nanGreeks()
	}

	timeToExpiry := timeToExpiryYears(timestamp, expiration)
	if timeToExpiry <= 0 {
		return nanGreeks()
	}

	dividendYield := cfg.DividendYield
	iv := solveImpliedVolatility(spot, strike, optionPrice, optionType, timeToExpiry, cfg.RiskFreeRate, dividendYield)
	if math.IsNaN(iv) {
		return nanGreeks()
	}

	d1, d2 := blackScholesD1D2(spot, strike, timeToExpiry, cfg.RiskFreeRate, dividendYield, iv)
	if math.IsNaN(d1) || math.IsNaN(d2) {
		return nanGreeks()
	}

	discountQ := math.Exp(-dividendYield * timeToExpiry)
	discountR := math.Exp(-cfg.RiskFreeRate * timeToExpiry)
	pdf := normalPDF(d1)
	sqrtT := math.Sqrt(timeToExpiry)

	var delta, theta, rho float64
	switch optionType {
	case "C":
		delta = discountQ * normalCDF(d1)
		theta = (-(spot*discountQ*pdf*iv)/(2*sqrtT) - cfg.RiskFreeRate*strike*discountR*normalCDF(d2) + dividendYield*spot*discountQ*normalCDF(d1)) / daysPerYear
		rho = strike * timeToExpiry * discountR * normalCDF(d2) / 100.0
	case "P":
		delta = discountQ * (normalCDF(d1) - 1.0)
		theta = (-(spot*discountQ*pdf*iv)/(2*sqrtT) + cfg.RiskFreeRate*strike*discountR*normalCDF(-d2) - dividendYield*spot*discountQ*normalCDF(-d1)) / daysPerYear
		rho = -strike * timeToExpiry * discountR * normalCDF(-d2) / 100.0
	default:
		return nanGreeks()
	}

	gamma := discountQ * pdf / (spot * iv * sqrtT)
	vega := spot * discountQ * pdf * sqrtT / 100.0

	return optionGreeks{
		ImpliedVolatility: iv,
		Delta:             delta,
		Gamma:             gamma,
		Vega:              vega,
		Theta:             theta,
		Rho:               rho,
	}
}

func nanGreeks() optionGreeks {
	nan := math.NaN()
	return optionGreeks{
		ImpliedVolatility: nan,
		Delta:             nan,
		Gamma:             nan,
		Vega:              nan,
		Theta:             nan,
		Rho:               nan,
	}
}

func timeToExpiryYears(timestamp, expiration time.Time) float64 {
	year, month, day := expiration.Date()
	expiryLocal := time.Date(year, month, day, 16, 0, 0, 0, newYorkLocation)
	duration := expiryLocal.UTC().Sub(timestamp.UTC())
	if duration <= 0 {
		return 0
	}
	return duration.Hours() / 24.0 / daysPerYear
}

func solveImpliedVolatility(spot, strike, targetPrice float64, optionType string, timeToExpiry, riskFreeRate, dividendYield float64) float64 {
	if targetPrice <= 0 || timeToExpiry <= 0 {
		return math.NaN()
	}

	minPrice, maxPrice := optionPriceBounds(spot, strike, optionType, timeToExpiry, riskFreeRate, dividendYield)
	targetPrice, ok := clampOptionPriceToBounds(targetPrice, minPrice, maxPrice)
	if !ok {
		return math.NaN()
	}

	sigma := math.Sqrt(2*math.Pi/timeToExpiry) * (targetPrice / spot)
	if math.IsNaN(sigma) || sigma < minImpliedVolatility || sigma > maxImpliedVolatility {
		sigma = 0.3
	}

	for i := 0; i < 16; i++ {
		price := blackScholesPrice(spot, strike, optionType, timeToExpiry, riskFreeRate, dividendYield, sigma)
		diff := price - targetPrice
		if math.Abs(diff) < 1e-10 {
			return sigma
		}

		d1, _ := blackScholesD1D2(spot, strike, timeToExpiry, riskFreeRate, dividendYield, sigma)
		vega := spot * math.Exp(-dividendYield*timeToExpiry) * normalPDF(d1) * math.Sqrt(timeToExpiry)
		if vega < 1e-8 {
			break
		}

		next := sigma - diff/vega
		if math.IsNaN(next) || next < minImpliedVolatility || next > maxImpliedVolatility {
			break
		}
		sigma = next
	}

	lo := minImpliedVolatility
	hi := maxImpliedVolatility
	for i := 0; i < 80; i++ {
		mid := (lo + hi) * 0.5
		price := blackScholesPrice(spot, strike, optionType, timeToExpiry, riskFreeRate, dividendYield, mid)
		diff := price - targetPrice
		if math.Abs(diff) < 1e-10 {
			return mid
		}
		if diff > 0 {
			hi = mid
		} else {
			lo = mid
		}
	}

	return (lo + hi) * 0.5
}

func clampOptionPriceToBounds(targetPrice, minPrice, maxPrice float64) (float64, bool) {
	if targetPrice < minPrice {
		if minPrice-targetPrice > priceClampTolerance {
			return math.NaN(), false
		}
		return minPrice, true
	}
	if targetPrice > maxPrice {
		if targetPrice-maxPrice > priceClampTolerance {
			return math.NaN(), false
		}
		return maxPrice, true
	}
	return targetPrice, true
}

func optionPriceBounds(spot, strike float64, optionType string, timeToExpiry, riskFreeRate, dividendYield float64) (float64, float64) {
	discountQ := math.Exp(-dividendYield * timeToExpiry)
	discountR := math.Exp(-riskFreeRate * timeToExpiry)
	maxPrice := spot * discountQ
	minPrice := math.Max(0, maxPrice-strike*discountR)

	if optionType == "P" {
		maxPrice = strike * discountR
		minPrice = math.Max(0, strike*discountR-spot*discountQ)
	}

	return minPrice, maxPrice
}

func blackScholesPrice(spot, strike float64, optionType string, timeToExpiry, riskFreeRate, dividendYield, sigma float64) float64 {
	d1, d2 := blackScholesD1D2(spot, strike, timeToExpiry, riskFreeRate, dividendYield, sigma)
	if math.IsNaN(d1) || math.IsNaN(d2) {
		return math.NaN()
	}

	discountQ := math.Exp(-dividendYield * timeToExpiry)
	discountR := math.Exp(-riskFreeRate * timeToExpiry)
	switch optionType {
	case "C":
		return spot*discountQ*normalCDF(d1) - strike*discountR*normalCDF(d2)
	case "P":
		return strike*discountR*normalCDF(-d2) - spot*discountQ*normalCDF(-d1)
	default:
		return math.NaN()
	}
}

func blackScholesD1D2(spot, strike, timeToExpiry, riskFreeRate, dividendYield, sigma float64) (float64, float64) {
	if spot <= 0 || strike <= 0 || timeToExpiry <= 0 || sigma <= 0 {
		return math.NaN(), math.NaN()
	}

	sqrtT := math.Sqrt(timeToExpiry)
	variance := sigma * sqrtT
	d1 := (math.Log(spot/strike) + (riskFreeRate-dividendYield+0.5*sigma*sigma)*timeToExpiry) / variance
	d2 := d1 - variance
	return d1, d2
}

func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

func normalPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}

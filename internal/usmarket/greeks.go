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
	daysPerYear          = 365.0
)

var newYorkLocation = loadNewYorkLocation()

type stockCloseKey struct {
	symbol    string
	timestamp int64
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

// ValidateOptionStockCoverage ensures the required stock bars exist before option import starts.
func ValidateOptionStockCoverage(ctx context.Context, conn driver.Conn, optionPath string, fileDate time.Time) (map[stockCloseKey]float64, error) {
	underlyings, err := CollectOptionUnderlyings(optionPath)
	if err != nil {
		return nil, fmt.Errorf("scan option underlyings: %w", err)
	}
	if len(underlyings) == 0 {
		return nil, fmt.Errorf("no valid option tickers found in %s", optionPath)
	}

	nextDay := fileDate.AddDate(0, 0, 1)
	stockCloses, seenSymbols, err := LoadStockCloseMap(ctx, conn, underlyings, fileDate, nextDay)
	if err != nil {
		return nil, err
	}

	if missing := MissingStockSymbols(underlyings, seenSymbols); len(missing) > 0 {
		return nil, fmt.Errorf("missing stock data for %s: %v", fileDate.Format("2006-01-02"), missing)
	}

	return stockCloses, nil
}

// EnrichOptionBarsWithGreeks attaches stock closes and Black-Scholes greeks to each option bar.
func EnrichOptionBarsWithGreeks(bars <-chan OptionBar1m, stockCloses map[stockCloseKey]float64, cfg GreeksConfig) (<-chan OptionBar1m, *error) {
	out := make(chan OptionBar1m, 8192)
	var enrichErr error

	go func() {
		defer close(out)

		for bar := range bars {
			underlyingClose, ok := stockCloses[newStockCloseKey(bar.Underlying, bar.Timestamp)]
			if !ok {
				enrichErr = fmt.Errorf("missing stock close for %s at %s", bar.Underlying, bar.Timestamp.Format(time.RFC3339))
				return
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
	}()

	return out, &enrichErr
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
	if targetPrice < minPrice-epsilonPrice || targetPrice > maxPrice+epsilonPrice {
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

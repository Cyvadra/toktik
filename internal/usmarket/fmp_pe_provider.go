package usmarket

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

// fmpQuarterStatementsSource tags legacy observations computed from FMP's quarterly
// income-statement, balance-sheet-statement, and historical-price-eod endpoints.
// Stored in the `source` column of fundamental_observation so different upstream
// providers can coexist for the same (symbol, factor_code) pair without
// overwriting each other.
const fmpQuarterStatementsSource = "fmp_quarter_statements"

// FMPPEBackfillProvider implements PEBackfillProvider against the Financial
// Modeling Prep API. Quarterly cadence: one PE/PB observation per filing
// quarter, keyed at the quarter-end date. Values are computed from FMP's
// quarterly statements and EOD close price so the provider does not depend on
// premium quarter-ratio endpoint access.
type FMPPEBackfillProvider struct {
	apiKey string
	limit  int
}

type fmpPEBackfillWorker struct {
	client *fmp.Client
	limit  int
}

// NewFMPPEBackfillProvider returns a quarterly PE/PB provider backed by FMP.
// quarterLimit caps the number of historical quarters fetched per symbol; pass
// 0 to use a sensible default (40 quarters ≈ 10 years).
func NewFMPPEBackfillProvider(apiKey string, quarterLimit int) *FMPPEBackfillProvider {
	if quarterLimit <= 0 {
		quarterLimit = 40
	}
	return &FMPPEBackfillProvider{apiKey: strings.TrimSpace(apiKey), limit: quarterLimit}
}

func (p *FMPPEBackfillProvider) Name() string { return "fmp" }

func (p *FMPPEBackfillProvider) Validate() error {
	if p.apiKey == "" {
		return fmt.Errorf("FMP API key is required (set fmp.api_key in runtime config or FMP_API_KEY env)")
	}
	return nil
}

func (p *FMPPEBackfillProvider) NewWorker(_ context.Context) (PEBackfillWorker, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &fmpPEBackfillWorker{client: fmp.New(p.apiKey), limit: p.limit}, nil
}

func (w *fmpPEBackfillWorker) FetchSymbolPE(ctx context.Context, conn driver.Conn, symbol string, startDate, endDate time.Time, _ int, batchSize int, dryRun bool, limiter backfillRateLimiter) (PEFetchResult, error) {
	if err := limiter.Wait(ctx); err != nil {
		return PEFetchResult{}, err
	}

	incomeStatements, err := w.client.IncomeStatements(ctx, symbol, "quarter", w.limit)
	if err != nil {
		return PEFetchResult{}, fmt.Errorf("fmp income statements: %w", err)
	}
	if err := limiter.Wait(ctx); err != nil {
		return PEFetchResult{}, err
	}
	balanceSheets, err := w.client.BalanceSheets(ctx, symbol, "quarter", w.limit)
	if err != nil {
		return PEFetchResult{}, fmt.Errorf("fmp balance sheets: %w", err)
	}
	if err := limiter.Wait(ctx); err != nil {
		return PEFetchResult{}, err
	}
	cashFlowStatements, err := w.client.CashFlowStatements(ctx, symbol, "quarter", w.limit)
	if err != nil {
		return PEFetchResult{}, fmt.Errorf("fmp cash flow statements: %w", err)
	}

	var persisted fmpStatementPersistResult
	if !dryRun {
		var err error
		persisted, err = persistFMPQuarterlyStatements(ctx, conn, symbol, incomeStatements, balanceSheets, cashFlowStatements, batchSize)
		if err != nil {
			return PEFetchResult{}, fmt.Errorf("persist FMP quarterly statements: %w", err)
		}
	}

	quarterInputs := buildFMPDerivedQuarterInputs(incomeStatements, balanceSheets)
	if len(quarterInputs) == 0 {
		return PEFetchResult{
			ProviderName:     "fmp",
			ProviderSource:   fmpStatementDerivedSource,
			StatementRows:    persisted.InsertedRows,
			StatementSkipped: persisted.SkippedRows,
			Diagnostics: PEFetchDiagnostics{
				NoQuarterInputs: 1,
			},
		}, nil
	}

	observations, diagnostics, err := deriveFMPStatementRatioObservations(ctx, conn, symbol, quarterInputs, startDate, endDate)
	if err != nil {
		return PEFetchResult{}, fmt.Errorf("derive FMP PE/PB observations: %w", err)
	}

	return PEFetchResult{
		ScannedBars:      len(quarterInputs),
		Observations:     observations,
		ProviderName:     "fmp",
		ProviderSource:   fmpStatementDerivedSource,
		StatementRows:    persisted.InsertedRows,
		StatementSkipped: persisted.SkippedRows,
		Diagnostics:      diagnostics,
	}, nil
}

type fmpQuarterFundamentalInput struct {
	Date               time.Time
	KnownAt            time.Time
	EPS                float64
	StockholdersEquity float64
	WeightedShares     float64
	WeightedSharesDil  float64
}

func (input fmpQuarterFundamentalInput) BookValuePerShare() (float64, bool) {
	shares := input.WeightedSharesDil
	if shares <= 0 {
		shares = input.WeightedShares
	}
	if shares <= 0 || input.StockholdersEquity == 0 {
		return 0, false
	}
	return input.StockholdersEquity / shares, true
}

func buildFMPQuarterFundamentalInputs(incomeStatements []fmp.IncomeStatement, balanceSheets []fmp.BalanceSheet) []fmpQuarterFundamentalInput {
	balanceByDate := make(map[string]fmp.BalanceSheet, len(balanceSheets))
	for _, balanceSheet := range balanceSheets {
		date := strings.TrimSpace(balanceSheet.Date)
		if date == "" {
			continue
		}
		balanceByDate[date] = balanceSheet
	}

	inputs := make([]fmpQuarterFundamentalInput, 0, len(incomeStatements))
	for _, incomeStatement := range incomeStatements {
		date, ok := parseFMPDate(incomeStatement.Date)
		if !ok {
			continue
		}
		balanceSheet, ok := balanceByDate[incomeStatement.Date]
		if !ok {
			continue
		}
		eps := incomeStatement.EPSDiluted
		if eps == 0 {
			eps = incomeStatement.EPS
		}
		knownAt := parseFMPKnownAt(incomeStatement.AcceptedDate, incomeStatement.FilingDate, incomeStatement.Date)
		inputs = append(inputs, fmpQuarterFundamentalInput{
			Date:               date,
			KnownAt:            knownAt,
			EPS:                eps,
			StockholdersEquity: incomeStatementFallbackEquity(balanceSheet),
			WeightedShares:     incomeStatement.WeightedAverageShsOut,
			WeightedSharesDil:  incomeStatement.WeightedAverageShsOutDil,
		})
	}

	sort.Slice(inputs, func(left, right int) bool { return inputs[left].Date.Before(inputs[right].Date) })
	return inputs
}

func incomeStatementFallbackEquity(balanceSheet fmp.BalanceSheet) float64 {
	if balanceSheet.TotalStockholdersEquity != 0 {
		return balanceSheet.TotalStockholdersEquity
	}
	if balanceSheet.TotalEquity != 0 {
		return balanceSheet.TotalEquity
	}
	return balanceSheet.TotalAssets - balanceSheet.TotalLiabilities
}

func trailingTwelveMonthEPS(inputs []fmpQuarterFundamentalInput, quarterIndex int) (float64, bool) {
	if quarterIndex < 3 || quarterIndex >= len(inputs) {
		return 0, false
	}
	total := 0.0
	for offset := 0; offset < 4; offset++ {
		eps := inputs[quarterIndex-offset].EPS
		if eps == 0 || math.IsNaN(eps) || math.IsInf(eps, 0) {
			return 0, false
		}
		total += eps
	}
	if total == 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, false
	}
	return total, true
}

type fmpHistoricalPricePoint struct {
	Date  time.Time
	Close float64
}

type fmpHistoricalPriceSeries []fmpHistoricalPricePoint

func buildFMPPriceSeries(prices []fmp.EODPrice) fmpHistoricalPriceSeries {
	series := make(fmpHistoricalPriceSeries, 0, len(prices))
	for _, price := range prices {
		date, ok := parseFMPDate(price.Date)
		if !ok || price.Close <= 0 {
			continue
		}
		series = append(series, fmpHistoricalPricePoint{Date: date, Close: price.Close})
	}
	sort.Slice(series, func(left, right int) bool { return series[left].Date.Before(series[right].Date) })
	return series
}

func (series fmpHistoricalPriceSeries) CloseOnOrBefore(date time.Time) (float64, bool) {
	if len(series) == 0 {
		return 0, false
	}
	position := sort.Search(len(series), func(index int) bool { return !series[index].Date.Before(date.AddDate(0, 0, 1)) }) - 1
	if position < 0 {
		return 0, false
	}
	return series[position].Close, true
}

func appendRatioObservation(observations []fundamentalObservationInsert, symbol, factorCode string, eventTS, knownAt time.Time, value float64) []fundamentalObservationInsert {
	return appendRatioObservationWithSource(observations, symbol, factorCode, eventTS, knownAt, fmpQuarterStatementsSource, value)
}

func appendRatioObservationWithSource(observations []fundamentalObservationInsert, symbol, factorCode string, eventTS, knownAt time.Time, source string, value float64) []fundamentalObservationInsert {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return observations
	}
	return append(observations, fundamentalObservationInsert{
		Market:     usStocksFundamentalsMarket,
		Symbol:     strings.ToUpper(strings.TrimSpace(symbol)),
		FactorCode: factorCode,
		EventTS:    eventTS,
		KnownAt:    knownAt,
		Source:     source,
		Value:      value,
	})
}

func parseFMPKnownAt(values ...string) time.Time {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
			parsed, err := time.ParseInLocation(layout, value, time.UTC)
			if err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

// parseFMPDate parses FMP's "YYYY-MM-DD" date strings into a UTC midnight
// time.Time. Returns ok=false on empty/invalid input.
func parseFMPDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

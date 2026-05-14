package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	macroDataset            = "gurufocus-shiller"
	macroSourceName         = "fmp"
	defaultPriceSymbol      = "^GSPC"
	defaultReferenceSymbol  = "SPX"
	defaultReferenceMarket  = "us-stocks"
	realtimeForwardFill     = "forward_fill"
	realtimePriceScaled     = "price_scaled"
	defaultWorkerCount      = 6
	defaultRollingQuarters  = 8
	defaultMinQuarters      = 4
	defaultDebugCSVFileName = "fmp_shiller_last_1y.csv"
)

type factorDefinition struct {
	Code         string
	DisplayName  string
	Description  string
	ValueType    string
	Unit         string
	RealtimeMode string
}

type macroCatalogRow struct {
	Dataset            string
	FactorCode         string
	DisplayName        string
	Description        string
	ValueType          string
	Unit               string
	PreferredFrequency string
	FillPolicy         string
	FillMaxDays        uint16
	PointInTime        uint8
	Source             string
	ReferenceMarket    string
	ReferenceSymbol    string
	RealtimeMode       string
	Active             uint8
	SLAHours           uint32
	Metadata           string
}

type macroObservationRow struct {
	Dataset         string
	FactorCode      string
	EventTS         time.Time
	KnownAt         time.Time
	PeriodStart     time.Time
	PeriodEnd       time.Time
	Source          string
	Value           float64
	ReferenceMarket string
	ReferenceSymbol string
	AnchorValue     float64
	Revision        uint32
}

type monthAnchor struct {
	StartMonth time.Time
	LastTS     time.Time
	LastClose  float64
	FirstTS    time.Time
}

type constituentChange struct {
	Date          time.Time
	AddedSymbol   string
	RemovedSymbol string
}

type quarterlyEarningsRecord struct {
	KnownAt   time.Time
	NetIncome float64
}

type symbolData struct {
	Symbol            string
	QuarterlyEarnings []quarterlyEarningsRecord
	MonthMarketCap    map[string]float64
}

type monthlyPoint struct {
	Month               time.Time
	PeriodEnd           time.Time
	KnownAt             time.Time
	AnchorValue         float64
	Price               float64
	CPI                 float64
	RateGS10            float64
	NominalEarnings     float64
	RealSP              float64
	RealEarnings        float64
	PE10                float64
	ExcessCAPEYield     float64
	ConstituentCount    int
	CoveredConstituents int
	TotalMarketCap      float64
	TotalNetIncome      float64
}

type dailyLivePoint struct {
	Date             time.Time
	Price            float64
	PELive           float64
	AnchorMonth      time.Time
	CoveredCount     int
	ConstituentCount int
}

type symbolFetchResult struct {
	Data symbolData
	Err  error
}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	fmpAPIKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		log.Fatalf("load FMP api key: %v", err)
	}

	now := time.Now().UTC()
	defaultFrom := time.Date(now.Year()-1, now.Month(), 1, 0, 0, 0, 0, time.UTC)
	defaultTo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)

	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	fromValue := flag.String("from", defaultFrom.Format("2006-01-02"), "Output start date (YYYY-MM-DD)")
	toValue := flag.String("to", defaultTo.Format("2006-01-02"), "Output end date, exclusive (YYYY-MM-DD)")
	priceSymbol := flag.String("price-symbol", defaultPriceSymbol, "FMP symbol used for index price history")
	referenceSymbol := flag.String("reference-symbol", defaultReferenceSymbol, "Reference symbol used for timestamp alignment and realtime scaling")
	workers := flag.Int("workers", defaultWorkerCount, "Concurrent FMP symbol fetch workers")
	batchSize := flag.Int("batch-size", 1000, "Rows per ClickHouse batch")
	initSchema := flag.Bool("init-schema", true, "Initialize fundamentals schema before sync")
	schemaFile := flag.String("schema", "", "Path to fundamentals.sql DDL (auto-detected if empty)")
	dryRun := flag.Bool("dry-run", false, "Compute but do not write to ClickHouse")
	debugCSV := flag.String("debug-csv", filepath.Join("reports", defaultDebugCSVFileName), "CSV path for last-year Shiller PE debug output")
	debugLiveCSV := flag.String("debug-live-csv", filepath.Join("reports", "fmp_shiller_live_last_1y.csv"), "Daily live-series CSV path for debugging")
	rollingQuarters := flag.Int("rolling-quarters", defaultRollingQuarters, "Quarter count used in the smoothed CAPE-like denominator")
	minQuarters := flag.Int("min-quarters", defaultMinQuarters, "Minimum available quarters required before emitting PE")
	symbolLimit := flag.Int("symbol-limit", 0, "Limit number of constituent symbols fetched for debugging (0 = all)")
	flag.Parse()

	from := appCli.ParseDate(*fromValue, "--from")
	to := appCli.ParseDate(*toValue, "--to")
	if !from.Before(to) {
		log.Fatalf("--from must be earlier than --to")
	}

	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	if *initSchema {
		ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.FundamentalsSchemaFile)
		if err != nil {
			log.Fatalf("resolve fundamentals.sql schema: %v", err)
		}
		if err := usmarket.InitFundamentalsSchema(ctx, conn, ddlFile); err != nil {
			log.Fatalf("initialize fundamentals schema: %v", err)
		}
	}

	if *rollingQuarters <= 0 {
		log.Fatalf("--rolling-quarters must be positive")
	}
	if *minQuarters <= 0 || *minQuarters > *rollingQuarters {
		log.Fatalf("--min-quarters must be in [1, rolling-quarters]")
	}

	client := fmp.New(fmpAPIKey, fmp.WithHTTPClient(&http.Client{Timeout: 90 * time.Second}))
	rollingMonths := *rollingQuarters * 3
	calcFrom := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -rollingMonths, 0)
	calcTo := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)

	anchors, err := loadMonthAnchors(ctx, conn, strings.ToUpper(strings.TrimSpace(*referenceSymbol)), calcFrom, calcTo)
	if err != nil {
		log.Fatalf("load month anchors: %v", err)
	}
	prices, err := fetchMonthEndPrices(ctx, client, *priceSymbol, calcFrom, calcTo)
	if err != nil {
		log.Fatalf("fetch price history: %v", err)
	}
	cpiSeries, err := fetchMonthlyCPI(ctx, client, calcFrom, calcTo)
	if err != nil {
		log.Printf("warning: fetch CPI from FMP failed, falling back to legacy stored macro series: %v", err)
		cpiSeries = map[string]float64{}
	}
	if len(cpiSeries) < rollingMonths {
		legacyCPI, err := loadLegacyMonthlySeries(ctx, conn, macroDataset, "CPI", calcFrom, calcTo)
		if err != nil {
			log.Fatalf("load legacy CPI bootstrap: %v", err)
		}
		for month, value := range legacyCPI {
			if _, ok := cpiSeries[month]; !ok {
				cpiSeries[month] = value
			}
		}
	}
	if len(cpiSeries) == 0 {
		log.Fatalf("CPI series unavailable from both FMP and legacy storage")
	}
	rateSeries, err := fetchMonthlyGS10(ctx, client, calcFrom, calcTo)
	if err != nil {
		log.Printf("warning: fetch treasury rates from FMP failed, falling back to legacy stored macro series: %v", err)
		rateSeries = map[string]float64{}
	}
	if len(rateSeries) < rollingMonths {
		legacyRates, err := loadLegacyMonthlySeries(ctx, conn, macroDataset, "rate_GS10", calcFrom, calcTo)
		if err != nil {
			log.Fatalf("load legacy GS10 bootstrap: %v", err)
		}
		for month, value := range legacyRates {
			if _, ok := rateSeries[month]; !ok {
				rateSeries[month] = value
			}
		}
	}
	if len(rateSeries) == 0 {
		log.Fatalf("GS10 series unavailable from both FMP and legacy storage")
	}
	currentConstituents, err := fetchCurrentConstituents(ctx, client)
	if err != nil {
		log.Fatalf("fetch S&P 500 constituents: %v", err)
	}
	changes, err := fetchHistoricalConstituentChanges(ctx, client)
	if err != nil {
		log.Fatalf("fetch S&P 500 constituent changes: %v", err)
	}
	memberships := buildMonthlyMemberships(currentConstituents, changes, calcFrom, calcTo)
	unionSymbols := unionMembershipSymbols(memberships)
	if *symbolLimit > 0 && *symbolLimit < len(unionSymbols) {
		unionSymbols = unionSymbols[:*symbolLimit]
		memberships = trimMemberships(memberships, unionSymbols)
	}

	quarterLimit := ((to.Year()-calcFrom.Year())+2)*4 + 8
	loadedSymbols, symbolErrors := fetchSymbolDataset(ctx, client, unionSymbols, calcFrom, calcTo, quarterLimit, *workers)
	for _, fetchErr := range symbolErrors {
		log.Printf("warning: %v", fetchErr)
	}

	points, err := buildMonthlyPoints(calcFrom, calcTo, prices, cpiSeries, rateSeries, anchors, memberships, loadedSymbols, *rollingQuarters, *minQuarters)
	if err != nil {
		log.Fatalf("build monthly shiller points: %v", err)
	}
	filtered := filterMonthlyPoints(points, from, to)
	if len(filtered) == 0 {
		log.Fatalf("no monthly points computed in requested range")
	}

	catalogRows := buildCatalogRows(strings.ToUpper(strings.TrimSpace(*referenceSymbol)))
	observationRows := buildObservationRows(filtered, anchors, strings.ToUpper(strings.TrimSpace(*referenceSymbol)))

	if err := writeDebugCSV(*debugCSV, filtered); err != nil {
		log.Fatalf("write debug csv: %v", err)
	}
	liveSeries := buildDailyLiveSeries(points, filtered, anchors, conn, strings.ToUpper(strings.TrimSpace(*referenceSymbol)), from, to)
	if err := writeLiveDebugCSV(*debugLiveCSV, liveSeries); err != nil {
		log.Fatalf("write live debug csv: %v", err)
	}

	if *dryRun {
		log.Printf("dry-run complete: dataset=%s rows=%d factors=%d debug_csv=%s live_csv=%s", macroDataset, len(observationRows), len(catalogRows), *debugCSV, *debugLiveCSV)
		return
	}
	if err := upsertMacroCatalog(ctx, conn, catalogRows, *batchSize); err != nil {
		log.Fatalf("upsert macro catalog: %v", err)
	}
	if err := insertMacroObservations(ctx, conn, observationRows, *batchSize); err != nil {
		log.Fatalf("insert macro observations: %v", err)
	}
	log.Printf("fmp macro sync complete: dataset=%s points=%d observation_rows=%d debug_csv=%s", macroDataset, len(filtered), len(observationRows), *debugCSV)
	log.Printf("coverage snapshot: latest_month=%s covered_constituents=%d total_constituents=%d", filtered[len(filtered)-1].Month.Format("2006-01"), filtered[len(filtered)-1].CoveredConstituents, filtered[len(filtered)-1].ConstituentCount)
}

func fetchCurrentConstituents(ctx context.Context, client *fmp.Client) ([]string, error) {
	rows, err := client.SP500Constituents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
		if symbol == "" || seen[symbol] {
			continue
		}
		seen[symbol] = true
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out, nil
}

func fetchHistoricalConstituentChanges(ctx context.Context, client *fmp.Client) ([]constituentChange, error) {
	rows, err := client.HistoricalSP500Changes(ctx, 5000)
	if err != nil {
		return nil, err
	}
	out := make([]constituentChange, 0, len(rows))
	for _, row := range rows {
		date, ok := parseFMPDate(row.Date)
		if !ok {
			continue
		}
		out = append(out, constituentChange{
			Date:          date,
			AddedSymbol:   strings.ToUpper(strings.TrimSpace(row.Symbol)),
			RemovedSymbol: strings.ToUpper(strings.TrimSpace(row.RemovedTicker)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out, nil
}

func buildMonthlyMemberships(current []string, changes []constituentChange, from, to time.Time) map[string][]string {
	currentSet := make(map[string]struct{}, len(current))
	for _, symbol := range current {
		currentSet[symbol] = struct{}{}
	}
	months := monthStarts(from, to)
	out := make(map[string][]string, len(months))
	changeIndex := 0
	for index := len(months) - 1; index >= 0; index-- {
		monthStart := months[index]
		monthEnd := monthStart.AddDate(0, 1, 0)
		for changeIndex < len(changes) && !changes[changeIndex].Date.Before(monthEnd) {
			change := changes[changeIndex]
			if change.AddedSymbol != "" {
				delete(currentSet, change.AddedSymbol)
			}
			if change.RemovedSymbol != "" {
				currentSet[change.RemovedSymbol] = struct{}{}
			}
			changeIndex++
		}
		members := make([]string, 0, len(currentSet))
		for symbol := range currentSet {
			members = append(members, symbol)
		}
		sort.Strings(members)
		out[monthStart.Format("2006-01")] = members
	}
	return out
}

func unionMembershipSymbols(memberships map[string][]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, members := range memberships {
		for _, symbol := range members {
			if seen[symbol] {
				continue
			}
			seen[symbol] = true
			out = append(out, symbol)
		}
	}
	sort.Strings(out)
	return out
}

func trimMemberships(memberships map[string][]string, keep []string) map[string][]string {
	allowed := map[string]bool{}
	for _, symbol := range keep {
		allowed[symbol] = true
	}
	out := make(map[string][]string, len(memberships))
	for month, members := range memberships {
		filtered := make([]string, 0, len(members))
		for _, symbol := range members {
			if allowed[symbol] {
				filtered = append(filtered, symbol)
			}
		}
		out[month] = filtered
	}
	return out
}

func fetchSymbolDataset(ctx context.Context, client *fmp.Client, symbols []string, from, to time.Time, quarterLimit, workers int) (map[string]symbolData, []error) {
	if workers <= 0 {
		workers = 1
	}
	jobs := make(chan string)
	results := make(chan symbolFetchResult, len(symbols))
	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for symbol := range jobs {
				data, err := fetchOneSymbol(ctx, client, symbol, from, to, quarterLimit)
				results <- symbolFetchResult{Data: data, Err: err}
			}
		}()
	}
	go func() {
		for _, symbol := range symbols {
			jobs <- symbol
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	loaded := make(map[string]symbolData, len(symbols))
	errs := make([]error, 0)
	for result := range results {
		if result.Err != nil {
			errs = append(errs, result.Err)
			continue
		}
		loaded[result.Data.Symbol] = result.Data
	}
	return loaded, errs
}

func fetchOneSymbol(ctx context.Context, client *fmp.Client, symbol string, from, to time.Time, quarterLimit int) (symbolData, error) {
	incomeRows, err := client.IncomeStatements(ctx, symbol, "quarter", quarterLimit)
	if err != nil {
		return symbolData{}, fmt.Errorf("%s income statements: %w", symbol, err)
	}
	marketCapRows, err := client.HistoricalMarketCap(ctx, symbol, from.Format("2006-01-02"), to.Format("2006-01-02"), 0)
	if err != nil {
		return symbolData{}, fmt.Errorf("%s market cap history: %w", symbol, err)
	}
	quarterly := make([]quarterlyEarningsRecord, 0, len(incomeRows))
	for _, row := range incomeRows {
		knownAt := parseFMPKnownAt(row.AcceptedDate, row.FilingDate, row.Date)
		if knownAt.IsZero() {
			continue
		}
		if row.NetIncome == 0 || math.IsNaN(row.NetIncome) || math.IsInf(row.NetIncome, 0) {
			continue
		}
		quarterly = append(quarterly, quarterlyEarningsRecord{KnownAt: knownAt, NetIncome: row.NetIncome})
	}
	sort.Slice(quarterly, func(i, j int) bool { return quarterly[i].KnownAt.Before(quarterly[j].KnownAt) })
	monthCap := buildMonthMarketCap(marketCapRows)
	if len(quarterly) < 4 || len(monthCap) == 0 {
		return symbolData{}, fmt.Errorf("%s incomplete FMP coverage: quarterly=%d marketcap_months=%d", symbol, len(quarterly), len(monthCap))
	}
	return symbolData{Symbol: symbol, QuarterlyEarnings: quarterly, MonthMarketCap: monthCap}, nil
}

func buildMonthMarketCap(rows []fmp.MarketCapHistory) map[string]float64 {
	type point struct {
		Date      time.Time
		MarketCap float64
	}
	points := make([]point, 0, len(rows))
	for _, row := range rows {
		date, ok := parseFMPDate(row.Date)
		if !ok || row.MarketCap <= 0 {
			continue
		}
		points = append(points, point{Date: date, MarketCap: float64(row.MarketCap)})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) })
	out := map[string]float64{}
	for _, item := range points {
		out[item.Date.Format("2006-01")] = item.MarketCap
	}
	return out
}

func fetchMonthEndPrices(ctx context.Context, client *fmp.Client, symbol string, from, to time.Time) (map[string]float64, error) {
	rows, err := client.HistoricalPrices(ctx, symbol, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	type point struct {
		Date  time.Time
		Close float64
	}
	points := make([]point, 0, len(rows))
	for _, row := range rows {
		date, ok := parseFMPDate(row.Date)
		if !ok || row.Close <= 0 {
			continue
		}
		points = append(points, point{Date: date, Close: row.Close})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) })
	out := make(map[string]float64)
	for _, item := range points {
		out[item.Date.Format("2006-01")] = item.Close
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no monthly prices returned for %s", symbol)
	}
	return out, nil
}

func fetchMonthlyCPI(ctx context.Context, client *fmp.Client, from, to time.Time) (map[string]float64, error) {
	rows, err := client.EconomicIndicators(ctx, "CPI", 400)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64)
	for _, row := range rows {
		date, ok := parseFMPDate(row.Date)
		if !ok || date.Before(from.AddDate(0, -1, 0)) || !date.Before(to.AddDate(0, 1, 0)) {
			continue
		}
		if row.Value <= 0 || math.IsNaN(row.Value) || math.IsInf(row.Value, 0) {
			continue
		}
		out[date.Format("2006-01")] = row.Value
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no CPI rows in requested range")
	}
	return out, nil
}

func fetchMonthlyGS10(ctx context.Context, client *fmp.Client, from, to time.Time) (map[string]float64, error) {
	rows, err := client.TreasuryRates(ctx, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	type point struct {
		Date time.Time
		Rate float64
	}
	points := make([]point, 0, len(rows))
	for _, row := range rows {
		date, ok := parseFMPDate(row.Date)
		if !ok || row.Year10 <= 0 || math.IsNaN(row.Year10) || math.IsInf(row.Year10, 0) {
			continue
		}
		points = append(points, point{Date: date, Rate: row.Year10})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) })
	out := make(map[string]float64)
	for _, item := range points {
		out[item.Date.Format("2006-01")] = item.Rate
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no GS10 rows in requested range")
	}
	return out, nil
}

func buildMonthlyPoints(from, to time.Time, prices, cpiSeries, rateSeries map[string]float64, anchors map[string]monthAnchor, memberships map[string][]string, symbols map[string]symbolData, rollingQuarters, minQuarters int) ([]monthlyPoint, error) {
	months := monthStarts(from, to)
	points := make([]monthlyPoint, 0, len(months))
	latestCPI := latestMonthlyValue(cpiSeries)
	if latestCPI <= 0 {
		return nil, fmt.Errorf("latest CPI is unavailable")
	}
	for _, month := range months {
		monthKey := month.Format("2006-01")
		price := prices[monthKey]
		cpi := cpiSeries[monthKey]
		rate := rateSeries[monthKey]
		if price <= 0 || cpi <= 0 || rate <= 0 {
			continue
		}
		anchor, hasAnchor := anchors[monthKey]
		knownAt := month.AddDate(0, 1, 0)
		anchorValue := price
		if hasAnchor {
			knownAt = anchor.FirstTS
			if anchor.LastClose > 0 {
				anchorValue = anchor.LastClose
			}
		}
		members := memberships[monthKey]
		monthEnd := month.AddDate(0, 1, 0)
		totalMarketCap := 0.0
		totalNetIncome := 0.0
		covered := 0
		for _, symbol := range members {
			data, ok := symbols[symbol]
			if !ok {
				continue
			}
			marketCap := data.MonthMarketCap[monthKey]
			if marketCap <= 0 {
				continue
			}
			netIncome, ok := latestTTMNetIncome(data.QuarterlyEarnings, monthEnd)
			if !ok {
				continue
			}
			totalMarketCap += marketCap
			totalNetIncome += netIncome
			covered++
		}
		if totalMarketCap <= 0 {
			continue
		}
		nominalEarnings := price * (totalNetIncome / totalMarketCap)
		if nominalEarnings == 0 || math.IsNaN(nominalEarnings) || math.IsInf(nominalEarnings, 0) {
			continue
		}
		realSP := price * latestCPI / cpi
		realEarnings := nominalEarnings * latestCPI / cpi
		points = append(points, monthlyPoint{
			Month:               month,
			PeriodEnd:           monthEnd,
			KnownAt:             knownAt,
			AnchorValue:         anchorValue,
			Price:               price,
			CPI:                 cpi,
			RateGS10:            rate,
			NominalEarnings:     nominalEarnings,
			RealSP:              realSP,
			RealEarnings:        realEarnings,
			ConstituentCount:    len(members),
			CoveredConstituents: covered,
			TotalMarketCap:      totalMarketCap,
			TotalNetIncome:      totalNetIncome,
		})
	}
	rollingMonths := rollingQuarters * 3
	minimumMonths := minQuarters * 3
	realWindow := make([]float64, 0, rollingMonths)
	for index := range points {
		realWindow = append(realWindow, points[index].RealEarnings)
		if len(realWindow) > rollingMonths {
			realWindow = realWindow[1:]
		}
		if len(realWindow) < minimumMonths {
			continue
		}
		avgRealEarnings := average(realWindow)
		if avgRealEarnings <= 0 {
			continue
		}
		points[index].PE10 = points[index].Price / avgRealEarnings
		if points[index].PE10 > 0 {
			points[index].ExcessCAPEYield = (100 / points[index].PE10) - points[index].RateGS10
		}
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("no monthly points built")
	}
	return points, nil
}

func latestTTMNetIncome(records []quarterlyEarningsRecord, asOf time.Time) (float64, bool) {
	window := make([]float64, 0, 4)
	for _, record := range records {
		if record.KnownAt.After(asOf) {
			break
		}
		window = append(window, record.NetIncome)
		if len(window) > 4 {
			window = window[1:]
		}
	}
	if len(window) < 4 {
		return 0, false
	}
	total := 0.0
	for _, value := range window {
		if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		total += value
	}
	if total == 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, false
	}
	return total, true
}

func filterMonthlyPoints(points []monthlyPoint, from, to time.Time) []monthlyPoint {
	out := make([]monthlyPoint, 0, len(points))
	for _, point := range points {
		if point.Month.Before(from) || !point.Month.Before(to) {
			continue
		}
		out = append(out, point)
	}
	return out
}

func buildCatalogRows(referenceSymbol string) []macroCatalogRow {
	definitions := []factorDefinition{
		{Code: "sp500", DisplayName: "S&P 500 Price", Description: "Monthly S&P 500 level derived from FMP index history", ValueType: "index", RealtimeMode: realtimePriceScaled},
		{Code: "earnings", DisplayName: "Earnings", Description: "Monthly index earnings-per-unit derived from FMP S&P 500 constituent penetration", ValueType: "float", RealtimeMode: realtimeForwardFill},
		{Code: "CPI", DisplayName: "CPI", Description: "Monthly CPI field from FMP economic indicators", ValueType: "float", RealtimeMode: realtimeForwardFill},
		{Code: "rate_GS10", DisplayName: "GS10 Rate", Description: "10-year treasury yield from FMP treasury rates", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		{Code: "real_sp", DisplayName: "Real S&P 500", Description: "Monthly inflation-adjusted S&P 500 level derived from FMP data", ValueType: "index", RealtimeMode: realtimePriceScaled},
		{Code: "real_earnings", DisplayName: "Real Earnings", Description: "Monthly inflation-adjusted earnings derived from FMP constituent penetration", ValueType: "float", RealtimeMode: realtimeForwardFill},
		{Code: "pe10", DisplayName: "Shiller PE", Description: "Monthly CAPE ratio computed from FMP index price, CPI, and constituent earnings penetration", ValueType: "ratio", RealtimeMode: realtimePriceScaled},
		{Code: "excess_cape_yield", DisplayName: "Excess CAPE Yield", Description: "Monthly excess CAPE yield computed as 100/pe10 - rate_GS10", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
	}
	rows := make([]macroCatalogRow, 0, len(definitions))
	for _, definition := range definitions {
		rows = append(rows, macroCatalogRow{
			Dataset:            macroDataset,
			FactorCode:         definition.Code,
			DisplayName:        definition.DisplayName,
			Description:        definition.Description,
			ValueType:          definition.ValueType,
			Unit:               definition.Unit,
			PreferredFrequency: "monthly",
			FillPolicy:         "forward_fill",
			FillMaxDays:        0,
			PointInTime:        1,
			Source:             macroSourceName,
			ReferenceMarket:    defaultReferenceMarket,
			ReferenceSymbol:    referenceSymbol,
			RealtimeMode:       definition.RealtimeMode,
			Active:             1,
			SLAHours:           24 * 7,
			Metadata:           fmt.Sprintf(`{"dataset":"%s","source":"%s","factor":"%s"}`, macroDataset, macroSourceName, definition.Code),
		})
	}
	return rows
}

func buildObservationRows(points []monthlyPoint, anchors map[string]monthAnchor, referenceSymbol string) []macroObservationRow {
	rows := make([]macroObservationRow, 0, len(points)*8)
	for _, point := range points {
		monthKey := point.Month.Format("2006-01")
		anchor, ok := anchors[monthKey]
		if !ok {
			anchor = monthAnchor{LastTS: point.PeriodEnd.Add(-time.Second), FirstTS: point.KnownAt, LastClose: point.AnchorValue}
		}
		rows = appendMacroObservation(rows, "sp500", point.Price, point.Month, point.PeriodEnd, anchor, referenceSymbol, true)
		rows = appendMacroObservation(rows, "earnings", point.NominalEarnings, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		rows = appendMacroObservation(rows, "CPI", point.CPI, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		rows = appendMacroObservation(rows, "rate_GS10", point.RateGS10, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		rows = appendMacroObservation(rows, "real_sp", point.RealSP, point.Month, point.PeriodEnd, anchor, referenceSymbol, true)
		rows = appendMacroObservation(rows, "real_earnings", point.RealEarnings, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		if point.PE10 > 0 {
			rows = appendMacroObservation(rows, "pe10", point.PE10, point.Month, point.PeriodEnd, anchor, referenceSymbol, true)
		}
		if point.ExcessCAPEYield != 0 && !math.IsNaN(point.ExcessCAPEYield) && !math.IsInf(point.ExcessCAPEYield, 0) {
			rows = appendMacroObservation(rows, "excess_cape_yield", point.ExcessCAPEYield, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		}
	}
	return rows
}

func appendMacroObservation(rows []macroObservationRow, factor string, value float64, periodStart, periodEnd time.Time, anchor monthAnchor, referenceSymbol string, priceScaled bool) []macroObservationRow {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return rows
	}
	anchorValue := math.NaN()
	if priceScaled {
		anchorValue = anchor.LastClose
	}
	return append(rows, macroObservationRow{
		Dataset:         macroDataset,
		FactorCode:      factor,
		EventTS:         anchor.LastTS,
		KnownAt:         anchor.FirstTS,
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
		Source:          macroSourceName,
		Value:           value,
		ReferenceMarket: defaultReferenceMarket,
		ReferenceSymbol: referenceSymbol,
		AnchorValue:     anchorValue,
	})
}

func writeDebugCSV(path string, points []monthlyPoint) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"month", "known_at", "sp500", "earnings", "cpi", "rate_gs10", "real_earnings", "pe10", "excess_cape_yield", "covered_constituents", "constituent_count"}); err != nil {
		return err
	}
	for _, point := range points {
		row := []string{
			point.Month.Format("2006-01-02"),
			point.KnownAt.Format(time.RFC3339),
			formatFloat(point.Price),
			formatFloat(point.NominalEarnings),
			formatFloat(point.CPI),
			formatFloat(point.RateGS10),
			formatFloat(point.RealEarnings),
			formatOptionalFloat(point.PE10),
			formatOptionalFloat(point.ExcessCAPEYield),
			strconv.Itoa(point.CoveredConstituents),
			strconv.Itoa(point.ConstituentCount),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}

func buildDailyLiveSeries(allPoints, filteredPoints []monthlyPoint, anchors map[string]monthAnchor, conn driver.Conn, referenceSymbol string, from, to time.Time) []dailyLivePoint {
	referenceBars, err := loadDailyReferenceBars(context.Background(), conn, referenceSymbol, from, to)
	if err != nil || len(referenceBars) == 0 {
		return nil
	}
	anchorPoints := make([]monthlyPoint, 0, len(allPoints))
	for _, point := range allPoints {
		if point.PE10 <= 0 {
			continue
		}
		anchorPoints = append(anchorPoints, point)
	}
	if len(anchorPoints) == 0 {
		return nil
	}
	sort.Slice(anchorPoints, func(i, j int) bool { return anchorPoints[i].KnownAt.Before(anchorPoints[j].KnownAt) })
	out := make([]dailyLivePoint, 0, len(referenceBars))
	anchorIndex := 0
	for _, bar := range referenceBars {
		for anchorIndex+1 < len(anchorPoints) && !anchorPoints[anchorIndex+1].KnownAt.After(bar.Timestamp) {
			anchorIndex++
		}
		current := anchorPoints[anchorIndex]
		if current.KnownAt.After(bar.Timestamp) || current.AnchorValue <= 0 {
			continue
		}
		peLive := current.PE10 * (bar.Close / current.AnchorValue)
		out = append(out, dailyLivePoint{
			Date:             bar.Timestamp,
			Price:            bar.Close,
			PELive:           peLive,
			AnchorMonth:      current.Month,
			CoveredCount:     current.CoveredConstituents,
			ConstituentCount: current.ConstituentCount,
		})
	}
	return out
}

func writeLiveDebugCSV(path string, points []dailyLivePoint) error {
	if path == "" || len(points) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"date", "spx_close", "pe_live", "anchor_month", "covered_constituents", "constituent_count"}); err != nil {
		return err
	}
	for _, point := range points {
		if err := writer.Write([]string{
			point.Date.Format("2006-01-02"),
			formatFloat(point.Price),
			formatFloat(point.PELive),
			point.AnchorMonth.Format("2006-01-02"),
			strconv.Itoa(point.CoveredCount),
			strconv.Itoa(point.ConstituentCount),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func formatOptionalFloat(value float64) string {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	return formatFloat(value)
}

func latestMonthlyValue(series map[string]float64) float64 {
	keys := make([]string, 0, len(series))
	for key := range series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return 0
	}
	return series[keys[len(keys)-1]]
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func monthStarts(from, to time.Time) []time.Time {
	start := time.Date(from.UTC().Year(), from.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(to.UTC().Year(), to.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]time.Time, 0)
	for month := start; month.Before(end); month = month.AddDate(0, 1, 0) {
		out = append(out, month)
	}
	return out
}

func loadMonthAnchors(ctx context.Context, conn driver.Conn, referenceSymbol string, from, to time.Time) (map[string]monthAnchor, error) {
	queryStart := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	queryEnd := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 2, 0)
	rows, err := conn.Query(ctx, `SELECT
		toStartOfMonth(timestamp) AS month_start,
		max(timestamp) AS last_ts,
		toFloat64(argMax(close, timestamp)) AS last_close,
		min(timestamp) AS first_ts
	FROM us_stocks_bar_1m
	WHERE symbol = {symbol:String}
	  AND timestamp >= toDateTime({from:String}, 'UTC')
	  AND timestamp < toDateTime({to:String}, 'UTC')
	  AND is_regular_session = 1
	GROUP BY month_start
	ORDER BY month_start`,
		clickhouse.Named("symbol", referenceSymbol),
		clickhouse.Named("from", queryStart.Format("2006-01-02 15:04:05")),
		clickhouse.Named("to", queryEnd.Format("2006-01-02 15:04:05")),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]monthAnchor{}
	for rows.Next() {
		var monthStart, lastTS, firstTS time.Time
		var lastClose float64
		if err := rows.Scan(&monthStart, &lastTS, &lastClose, &firstTS); err != nil {
			return nil, err
		}
		out[monthStart.UTC().Format("2006-01")] = monthAnchor{StartMonth: monthStart.UTC(), LastTS: lastTS.UTC(), LastClose: lastClose, FirstTS: firstTS.UTC()}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no month anchors found for %s", referenceSymbol)
	}
	for monthKey, anchor := range out {
		nextMonth := anchor.StartMonth.AddDate(0, 1, 0).Format("2006-01")
		if next, ok := out[nextMonth]; ok {
			anchor.FirstTS = next.FirstTS
		} else if anchor.FirstTS.IsZero() {
			anchor.FirstTS = anchor.LastTS
		}
		out[monthKey] = anchor
	}
	return out, nil
}

func loadLegacyMonthlySeries(ctx context.Context, conn driver.Conn, dataset, factor string, from, to time.Time) (map[string]float64, error) {
	rows, err := conn.Query(ctx, `SELECT
		toStartOfMonth(event_ts) AS month_start,
		argMax(value, (known_at, revision)) AS latest_value
	FROM macro_observation
	WHERE dataset = {dataset:String}
	  AND factor_code = {factor:String}
	  AND event_ts >= toDateTime({from:String}, 'UTC')
	  AND event_ts < toDateTime({to:String}, 'UTC')
	GROUP BY month_start
	ORDER BY month_start`,
		clickhouse.Named("dataset", dataset),
		clickhouse.Named("factor", factor),
		clickhouse.Named("from", from.Format("2006-01-02 15:04:05")),
		clickhouse.Named("to", to.Format("2006-01-02 15:04:05")),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var monthStart time.Time
		var latestValue float64
		if err := rows.Scan(&monthStart, &latestValue); err != nil {
			return nil, err
		}
		if latestValue == 0 || math.IsNaN(latestValue) || math.IsInf(latestValue, 0) {
			continue
		}
		out[monthStart.UTC().Format("2006-01")] = latestValue
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type dailyReferenceBar struct {
	Timestamp time.Time
	Close     float64
}

func loadDailyReferenceBars(ctx context.Context, conn driver.Conn, referenceSymbol string, from, to time.Time) ([]dailyReferenceBar, error) {
	rows, err := conn.Query(ctx, `SELECT
		timestamp,
		toFloat64(close) AS close
	FROM us_stocks_bar_1d
	WHERE symbol = {symbol:String}
	  AND timestamp >= toDateTime({from:String}, 'UTC')
	  AND timestamp < toDateTime({to:String}, 'UTC')
	ORDER BY timestamp`,
		clickhouse.Named("symbol", referenceSymbol),
		clickhouse.Named("from", from.Format("2006-01-02 15:04:05")),
		clickhouse.Named("to", to.Format("2006-01-02 15:04:05")),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]dailyReferenceBar, 0)
	for rows.Next() {
		var bar dailyReferenceBar
		if err := rows.Scan(&bar.Timestamp, &bar.Close); err != nil {
			return nil, err
		}
		if bar.Close <= 0 {
			continue
		}
		out = append(out, bar)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func upsertMacroCatalog(ctx context.Context, conn driver.Conn, rows []macroCatalogRow, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	prepare := func() (driver.Batch, error) {
		return conn.PrepareBatch(ctx, `INSERT INTO macro_factor_catalog (
			dataset, factor_code, display_name, description, value_type, unit,
			preferred_frequency, fill_policy, fill_max_days, point_in_time, source,
			reference_market, reference_symbol, realtime_mode, active, sla_hours, metadata
		)`)
	}
	batch, err := prepare()
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		if err := batch.Append(
			row.Dataset,
			row.FactorCode,
			row.DisplayName,
			row.Description,
			row.ValueType,
			row.Unit,
			row.PreferredFrequency,
			row.FillPolicy,
			row.FillMaxDays,
			row.PointInTime,
			row.Source,
			row.ReferenceMarket,
			row.ReferenceSymbol,
			row.RealtimeMode,
			row.Active,
			row.SLAHours,
			row.Metadata,
		); err != nil {
			return err
		}
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return err
			}
			batch, err = prepare()
			if err != nil {
				return err
			}
			pending = 0
		}
	}
	if pending > 0 {
		return batch.Send()
	}
	return nil
}

func insertMacroObservations(ctx context.Context, conn driver.Conn, rows []macroObservationRow, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(rows)
	}
	existing, err := loadExistingMacroRevisions(ctx, conn, rows)
	if err != nil {
		return err
	}
	prepare := func() (driver.Batch, error) {
		return conn.PrepareBatch(ctx, `INSERT INTO macro_observation (
			dataset, factor_code, event_ts, known_at, period_start, period_end, source,
			value, reference_market, reference_symbol, anchor_value, revision
		)`)
	}
	batch, err := prepare()
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		key := fmt.Sprintf("%s|%s|%s|%s", row.Dataset, row.FactorCode, row.EventTS.UTC().Format(time.RFC3339Nano), row.KnownAt.UTC().Format(time.RFC3339Nano))
		if current, ok := existing[key]; ok {
			if almostEqualFloat(current.Value, row.Value) {
				continue
			}
			row.Revision = current.Revision + 1
		}
		if err := batch.Append(
			row.Dataset,
			row.FactorCode,
			row.EventTS,
			row.KnownAt,
			row.PeriodStart,
			row.PeriodEnd,
			row.Source,
			row.Value,
			row.ReferenceMarket,
			row.ReferenceSymbol,
			row.AnchorValue,
			row.Revision,
		); err != nil {
			return err
		}
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return err
			}
			batch, err = prepare()
			if err != nil {
				return err
			}
			pending = 0
		}
	}
	if pending > 0 {
		return batch.Send()
	}
	return nil
}

type existingMacroObservation struct {
	Revision uint32
	Value    float64
}

func loadExistingMacroRevisions(ctx context.Context, conn driver.Conn, rows []macroObservationRow) (map[string]existingMacroObservation, error) {
	if len(rows) == 0 {
		return map[string]existingMacroObservation{}, nil
	}
	type timeRange struct {
		From time.Time
		To   time.Time
	}
	byDataset := map[string]timeRange{}
	for _, row := range rows {
		current, ok := byDataset[row.Dataset]
		if !ok {
			byDataset[row.Dataset] = timeRange{From: row.EventTS, To: row.KnownAt}
			continue
		}
		if row.EventTS.Before(current.From) {
			current.From = row.EventTS
		}
		if row.KnownAt.After(current.To) {
			current.To = row.KnownAt
		}
		byDataset[row.Dataset] = current
	}
	out := map[string]existingMacroObservation{}
	for dataset, timeWindow := range byDataset {
		queryRows, err := conn.Query(ctx, `SELECT dataset, factor_code, event_ts, known_at,
		argMax(value, revision) AS value,
		max(revision) AS revision
		FROM macro_observation
		WHERE dataset = {dataset:String}
		  AND event_ts >= toDateTime({from:String}, 'UTC')
		  AND known_at <= toDateTime({to:String}, 'UTC')
		GROUP BY dataset, factor_code, event_ts, known_at`,
			clickhouse.Named("dataset", dataset),
			clickhouse.Named("from", timeWindow.From.UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("to", timeWindow.To.AddDate(0, 1, 0).UTC().Format("2006-01-02 15:04:05")),
		)
		if err != nil {
			return nil, err
		}
		for queryRows.Next() {
			var loadedDataset, factorCode string
			var eventTS, knownAt time.Time
			var value float64
			var revision uint32
			if err := queryRows.Scan(&loadedDataset, &factorCode, &eventTS, &knownAt, &value, &revision); err != nil {
				queryRows.Close()
				return nil, err
			}
			key := fmt.Sprintf("%s|%s|%s|%s", loadedDataset, factorCode, eventTS.UTC().Format(time.RFC3339Nano), knownAt.UTC().Format(time.RFC3339Nano))
			out[key] = existingMacroObservation{Revision: revision, Value: value}
		}
		if err := queryRows.Err(); err != nil {
			queryRows.Close()
			return nil, err
		}
		queryRows.Close()
	}
	return out, nil
}

func almostEqualFloat(left, right float64) bool {
	const epsilon = 1e-9
	return math.Abs(left-right) <= epsilon
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

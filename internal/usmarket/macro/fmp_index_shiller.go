package macro

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	legacyMacroDataset         = DefaultGurufocusShillerDataset
	DefaultFMPSP500Dataset     = "fmp-sp500-shiller"
	DefaultFMPNasdaq100Dataset = "fmp-nasdaq100-shiller"
	fmpMacroSource             = "fmp"
	defaultFMPMacroWorkers     = 6
	defaultFMPRollingQuarters  = 8
	defaultFMPMinimumQuarters  = 4
	defaultFMPMacroHTTPTimeout = 90 * time.Second
)

type FMPIndexShillerConfig struct {
	APIKey              string
	Dataset             string
	ConstituentUniverse string
	PriceSymbol         string
	ReferenceSymbol     string
	BatchSize           int
	Workers             int
	RollingQuarters     int
	MinQuarters         int
}

type FMPIndexShillerResult struct {
	CatalogRows            int
	ObservationRows        int
	Points                 int
	LatestCoveredCount     int
	LatestConstituentCount int
}

type fmpUniverseConfig struct {
	Name                   string
	DisplayName            string
	PriceFactorCode        string
	DefaultDataset         string
	DefaultPriceSymbol     string
	DefaultReferenceSymbol string
}

var fmpMacroUniverses = map[string]fmpUniverseConfig{
	"sp500": {
		Name:                   "sp500",
		DisplayName:            "S&P 500",
		PriceFactorCode:        "sp500",
		DefaultDataset:         DefaultFMPSP500Dataset,
		DefaultPriceSymbol:     "SPY",
		DefaultReferenceSymbol: "SPY",
	},
	"nasdaq100": {
		Name:                   "nasdaq100",
		DisplayName:            "Nasdaq-100",
		PriceFactorCode:        "ndx",
		DefaultDataset:         DefaultFMPNasdaq100Dataset,
		DefaultPriceSymbol:     "QQQ",
		DefaultReferenceSymbol: "QQQ",
	},
}

type fmpFactorDefinition struct {
	Code         string
	DisplayName  string
	Description  string
	ValueType    string
	Unit         string
	RealtimeMode string
}

type fmpConstituentChange struct {
	Date          time.Time
	AddedSymbol   string
	RemovedSymbol string
}

type fmpQuarterlyEarningsRecord struct {
	KnownAt   time.Time
	NetIncome float64
}

type fmpSymbolData struct {
	Symbol            string
	QuarterlyEarnings []fmpQuarterlyEarningsRecord
	MonthMarketCap    map[string]float64
}

type fmpMonthlyPoint struct {
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

type fmpSymbolFetchResult struct {
	Data fmpSymbolData
	Err  error
}

type fmpMonthAnchor struct {
	StartMonth time.Time
	LastTS     time.Time
	LastClose  float64
	FirstTS    time.Time
}

func SyncFMPIndexShiller(ctx context.Context, conn driver.Conn, cfg FMPIndexShillerConfig, from, to time.Time, dryRun bool) (FMPIndexShillerResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return FMPIndexShillerResult{}, fmt.Errorf("FMP API key is required")
	}
	if !from.Before(to) {
		return FMPIndexShillerResult{}, fmt.Errorf("from must be earlier than to")
	}
	universe, err := resolveFMPMacroUniverse(cfg.ConstituentUniverse)
	if err != nil {
		return FMPIndexShillerResult{}, err
	}
	if strings.TrimSpace(cfg.Dataset) == "" {
		cfg.Dataset = universe.DefaultDataset
	}
	if strings.TrimSpace(cfg.PriceSymbol) == "" {
		cfg.PriceSymbol = universe.DefaultPriceSymbol
	}
	if strings.TrimSpace(cfg.ReferenceSymbol) == "" {
		cfg.ReferenceSymbol = universe.DefaultReferenceSymbol
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = defaultFMPMacroWorkers
	}
	if cfg.RollingQuarters <= 0 {
		cfg.RollingQuarters = defaultFMPRollingQuarters
	}
	if cfg.MinQuarters <= 0 {
		cfg.MinQuarters = defaultFMPMinimumQuarters
	}
	if cfg.MinQuarters > cfg.RollingQuarters {
		return FMPIndexShillerResult{}, fmt.Errorf("min_quarters must be in [1, rolling_quarters]")
	}

	referenceSymbol := strings.ToUpper(strings.TrimSpace(cfg.ReferenceSymbol))
	priceSymbol := strings.TrimSpace(cfg.PriceSymbol)
	client := fmp.New(cfg.APIKey, fmp.WithHTTPClient(&http.Client{Timeout: defaultFMPMacroHTTPTimeout}))
	rollingMonths := cfg.RollingQuarters * 3
	calcFrom := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -rollingMonths, 0)
	calcTo := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)

	anchors, err := loadFMPMonthAnchors(ctx, conn, referenceSymbol, calcFrom, calcTo)
	if err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("load month anchors: %w", err)
	}
	prices, err := fetchFMPMonthEndPrices(ctx, client, priceSymbol, calcFrom, calcTo)
	if err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("fetch price history: %w", err)
	}
	cpiSeries, err := fetchFMPMonthlyCPI(ctx, client, calcFrom, calcTo)
	if err != nil {
		cpiSeries = map[string]float64{}
	}
	if len(cpiSeries) < rollingMonths {
		legacyCPI, err := loadFMPMacroLegacyMonthlySeries(ctx, conn, legacyMacroDataset, "CPI", calcFrom, calcTo)
		if err != nil {
			return FMPIndexShillerResult{}, fmt.Errorf("load legacy CPI bootstrap: %w", err)
		}
		for month, value := range legacyCPI {
			if _, ok := cpiSeries[month]; !ok {
				cpiSeries[month] = value
			}
		}
	}
	if len(cpiSeries) == 0 {
		return FMPIndexShillerResult{}, fmt.Errorf("CPI series unavailable from both FMP and legacy storage")
	}
	rateSeries, err := fetchFMPMonthlyGS10(ctx, client, calcFrom, calcTo)
	if err != nil {
		rateSeries = map[string]float64{}
	}
	if len(rateSeries) < rollingMonths {
		legacyRates, err := loadFMPMacroLegacyMonthlySeries(ctx, conn, legacyMacroDataset, "rate_GS10", calcFrom, calcTo)
		if err != nil {
			return FMPIndexShillerResult{}, fmt.Errorf("load legacy GS10 bootstrap: %w", err)
		}
		for month, value := range legacyRates {
			if _, ok := rateSeries[month]; !ok {
				rateSeries[month] = value
			}
		}
	}
	if len(rateSeries) == 0 {
		return FMPIndexShillerResult{}, fmt.Errorf("GS10 series unavailable from both FMP and legacy storage")
	}
	currentConstituents, err := fetchFMPCurrentConstituents(ctx, client, universe)
	if err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("fetch %s constituents: %w", universe.DisplayName, err)
	}
	changes, err := fetchFMPHistoricalConstituentChanges(ctx, client, universe)
	if err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("fetch %s constituent changes: %w", universe.DisplayName, err)
	}
	memberships := buildFMPMonthlyMemberships(currentConstituents, changes, calcFrom, calcTo)
	unionSymbols := unionFMPMembershipSymbols(memberships)
	quarterLimit := ((to.Year()-calcFrom.Year())+2)*4 + 8
	loadedSymbols, _ := fetchFMPSymbolDataset(ctx, client, unionSymbols, calcFrom, calcTo, quarterLimit, cfg.Workers)

	points, err := buildFMPMonthlyPoints(calcFrom, calcTo, prices, cpiSeries, rateSeries, anchors, memberships, loadedSymbols, cfg.RollingQuarters, cfg.MinQuarters)
	if err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("build monthly shiller points: %w", err)
	}
	filtered := filterFMPMonthlyPoints(points, from, to)
	if len(filtered) == 0 {
		return FMPIndexShillerResult{}, fmt.Errorf("no monthly points computed in requested range")
	}
	catalogRows := buildFMPIndexCatalogRows(cfg.Dataset, universe, referenceSymbol)
	observationRows := buildFMPIndexObservationRows(cfg.Dataset, filtered, anchors, referenceSymbol, universe.PriceFactorCode)
	if dryRun {
		latest := filtered[len(filtered)-1]
		return FMPIndexShillerResult{CatalogRows: len(catalogRows), ObservationRows: len(observationRows), Points: len(filtered), LatestCoveredCount: latest.CoveredConstituents, LatestConstituentCount: latest.ConstituentCount}, nil
	}
	if err := UpsertCatalog(ctx, conn, catalogRows, cfg.BatchSize); err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("upsert macro catalog: %w", err)
	}
	if err := InsertObservations(ctx, conn, observationRows, cfg.BatchSize); err != nil {
		return FMPIndexShillerResult{}, fmt.Errorf("insert macro observations: %w", err)
	}
	latest := filtered[len(filtered)-1]
	return FMPIndexShillerResult{CatalogRows: len(catalogRows), ObservationRows: len(observationRows), Points: len(filtered), LatestCoveredCount: latest.CoveredConstituents, LatestConstituentCount: latest.ConstituentCount}, nil
}

func resolveFMPMacroUniverse(raw string) (fmpUniverseConfig, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		name = "sp500"
	}
	if cfg, ok := fmpMacroUniverses[name]; ok {
		return cfg, nil
	}
	return fmpUniverseConfig{}, fmt.Errorf("unsupported constituent universe %q", raw)
}

func fetchFMPCurrentConstituents(ctx context.Context, client *fmp.Client, universe fmpUniverseConfig) ([]string, error) {
	var (
		rows []fmp.IndexConstituent
		err  error
	)
	switch universe.Name {
	case "sp500":
		rows, err = client.SP500Constituents(ctx)
	case "nasdaq100":
		rows, err = client.NasdaqConstituents(ctx)
	default:
		return nil, fmt.Errorf("unsupported constituent universe %q", universe.Name)
	}
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

func fetchFMPHistoricalConstituentChanges(ctx context.Context, client *fmp.Client, universe fmpUniverseConfig) ([]fmpConstituentChange, error) {
	var (
		rows []fmp.IndexConstituentChange
		err  error
	)
	switch universe.Name {
	case "sp500":
		rows, err = client.HistoricalSP500Changes(ctx, 5000)
	case "nasdaq100":
		rows, err = client.HistoricalNasdaqChanges(ctx, 5000)
	default:
		return nil, fmt.Errorf("unsupported constituent universe %q", universe.Name)
	}
	if err != nil {
		return nil, err
	}
	out := make([]fmpConstituentChange, 0, len(rows))
	for _, row := range rows {
		date, ok := parseFMPMacroDate(row.Date)
		if !ok {
			continue
		}
		out = append(out, fmpConstituentChange{Date: date, AddedSymbol: strings.ToUpper(strings.TrimSpace(row.Symbol)), RemovedSymbol: strings.ToUpper(strings.TrimSpace(row.RemovedTicker))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out, nil
}

func buildFMPMonthlyMemberships(current []string, changes []fmpConstituentChange, from, to time.Time) map[string][]string {
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

func unionFMPMembershipSymbols(memberships map[string][]string) []string {
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

func fetchFMPSymbolDataset(ctx context.Context, client *fmp.Client, symbols []string, from, to time.Time, quarterLimit, workers int) (map[string]fmpSymbolData, []error) {
	if workers <= 0 {
		workers = 1
	}
	jobs := make(chan string)
	results := make(chan fmpSymbolFetchResult, len(symbols))
	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for symbol := range jobs {
				data, err := fetchFMPMacroSymbol(ctx, client, symbol, from, to, quarterLimit)
				results <- fmpSymbolFetchResult{Data: data, Err: err}
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
	loaded := make(map[string]fmpSymbolData, len(symbols))
	errList := make([]error, 0)
	for result := range results {
		if result.Err != nil {
			errList = append(errList, result.Err)
			continue
		}
		loaded[result.Data.Symbol] = result.Data
	}
	return loaded, errList
}

func fetchFMPMacroSymbol(ctx context.Context, client *fmp.Client, symbol string, from, to time.Time, quarterLimit int) (fmpSymbolData, error) {
	incomeRows, err := client.IncomeStatements(ctx, symbol, "quarter", quarterLimit)
	if err != nil {
		return fmpSymbolData{}, fmt.Errorf("%s income statements: %w", symbol, err)
	}
	marketCapRows, err := client.HistoricalMarketCap(ctx, symbol, from.Format("2006-01-02"), to.Format("2006-01-02"), 0)
	if err != nil {
		return fmpSymbolData{}, fmt.Errorf("%s market cap history: %w", symbol, err)
	}
	quarterly := make([]fmpQuarterlyEarningsRecord, 0, len(incomeRows))
	for _, row := range incomeRows {
		knownAt := parseFMPMacroKnownAt(row.AcceptedDate, row.FilingDate, row.Date)
		if knownAt.IsZero() {
			continue
		}
		if row.NetIncome == 0 || math.IsNaN(row.NetIncome) || math.IsInf(row.NetIncome, 0) {
			continue
		}
		quarterly = append(quarterly, fmpQuarterlyEarningsRecord{KnownAt: knownAt, NetIncome: row.NetIncome})
	}
	sort.Slice(quarterly, func(i, j int) bool { return quarterly[i].KnownAt.Before(quarterly[j].KnownAt) })
	monthCap := buildFMPMacroMonthMarketCap(marketCapRows)
	if len(quarterly) < 4 || len(monthCap) == 0 {
		return fmpSymbolData{}, fmt.Errorf("%s incomplete FMP coverage: quarterly=%d marketcap_months=%d", symbol, len(quarterly), len(monthCap))
	}
	return fmpSymbolData{Symbol: symbol, QuarterlyEarnings: quarterly, MonthMarketCap: monthCap}, nil
}

func buildFMPMacroMonthMarketCap(rows []fmp.MarketCapHistory) map[string]float64 {
	type point struct {
		Date      time.Time
		MarketCap float64
	}
	points := make([]point, 0, len(rows))
	for _, row := range rows {
		date, ok := parseFMPMacroDate(row.Date)
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

func fetchFMPMonthEndPrices(ctx context.Context, client *fmp.Client, symbol string, from, to time.Time) (map[string]float64, error) {
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
		date, ok := parseFMPMacroDate(row.Date)
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

func fetchFMPMonthlyCPI(ctx context.Context, client *fmp.Client, from, to time.Time) (map[string]float64, error) {
	rows, err := client.EconomicIndicators(ctx, "CPI", 400)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64)
	for _, row := range rows {
		date, ok := parseFMPMacroDate(row.Date)
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

func fetchFMPMonthlyGS10(ctx context.Context, client *fmp.Client, from, to time.Time) (map[string]float64, error) {
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
		date, ok := parseFMPMacroDate(row.Date)
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

func buildFMPMonthlyPoints(from, to time.Time, prices, cpiSeries, rateSeries map[string]float64, anchors map[string]fmpMonthAnchor, memberships map[string][]string, symbols map[string]fmpSymbolData, rollingQuarters, minQuarters int) ([]fmpMonthlyPoint, error) {
	months := monthStarts(from, to)
	points := make([]fmpMonthlyPoint, 0, len(months))
	latestCPI := latestFMPMacroMonthlyValue(cpiSeries)
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
			netIncome, ok := latestFMPMacroTTMNetIncome(data.QuarterlyEarnings, monthEnd)
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
		points = append(points, fmpMonthlyPoint{Month: month, PeriodEnd: monthEnd, KnownAt: knownAt, AnchorValue: anchorValue, Price: price, CPI: cpi, RateGS10: rate, NominalEarnings: nominalEarnings, RealSP: realSP, RealEarnings: realEarnings, ConstituentCount: len(members), CoveredConstituents: covered, TotalMarketCap: totalMarketCap, TotalNetIncome: totalNetIncome})
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
		avgRealEarnings := averageFMPMacro(realWindow)
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

func latestFMPMacroTTMNetIncome(records []fmpQuarterlyEarningsRecord, asOf time.Time) (float64, bool) {
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

func filterFMPMonthlyPoints(points []fmpMonthlyPoint, from, to time.Time) []fmpMonthlyPoint {
	out := make([]fmpMonthlyPoint, 0, len(points))
	for _, point := range points {
		if point.Month.Before(from) || !point.Month.Before(to) {
			continue
		}
		out = append(out, point)
	}
	return out
}

func buildFMPIndexCatalogRows(dataset string, universe fmpUniverseConfig, referenceSymbol string) []CatalogRow {
	definitions := []fmpFactorDefinition{
		{Code: universe.PriceFactorCode, DisplayName: universe.DisplayName + " Price", Description: "Monthly " + universe.DisplayName + " level derived from FMP price history", ValueType: "index", RealtimeMode: realtimePriceScaled},
		{Code: "earnings", DisplayName: "Earnings", Description: "Monthly index earnings-per-unit derived from FMP " + universe.DisplayName + " constituent penetration", ValueType: "float", RealtimeMode: realtimeForwardFill},
		{Code: "CPI", DisplayName: "CPI", Description: "Monthly CPI field from FMP economic indicators", ValueType: "float", RealtimeMode: realtimeForwardFill},
		{Code: "rate_GS10", DisplayName: "GS10 Rate", Description: "10-year treasury yield from FMP treasury rates", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		{Code: "real_sp", DisplayName: "Real " + universe.DisplayName, Description: "Monthly inflation-adjusted " + universe.DisplayName + " level derived from FMP data", ValueType: "index", RealtimeMode: realtimePriceScaled},
		{Code: "real_earnings", DisplayName: "Real Earnings", Description: "Monthly inflation-adjusted earnings derived from FMP constituent penetration", ValueType: "float", RealtimeMode: realtimeForwardFill},
		{Code: "pe10", DisplayName: universe.DisplayName + " PE10", Description: "Monthly CAPE ratio computed from FMP price, CPI, and constituent earnings penetration", ValueType: "ratio", RealtimeMode: realtimePriceScaled},
		{Code: "excess_cape_yield", DisplayName: "Excess CAPE Yield", Description: "Monthly excess CAPE yield computed as 100/pe10 - rate_GS10", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
	}
	rows := make([]CatalogRow, 0, len(definitions))
	for _, definition := range definitions {
		rows = append(rows, CatalogRow{Dataset: dataset, FactorCode: definition.Code, DisplayName: definition.DisplayName, Description: definition.Description, ValueType: definition.ValueType, Unit: definition.Unit, PreferredFrequency: "monthly", FillPolicy: "forward_fill", PointInTime: 1, Source: fmpMacroSource, ReferenceMarket: DefaultReferenceMarket, ReferenceSymbol: referenceSymbol, RealtimeMode: definition.RealtimeMode, Active: 1, SLAHours: 24 * 7, Metadata: fmt.Sprintf(`{"dataset":"%s","source":"%s","factor":"%s","universe":"%s"}`, dataset, fmpMacroSource, definition.Code, universe.Name)})
	}
	return rows
}

func buildFMPIndexObservationRows(dataset string, points []fmpMonthlyPoint, anchors map[string]fmpMonthAnchor, referenceSymbol, priceFactorCode string) []ObservationRow {
	rows := make([]ObservationRow, 0, len(points)*8)
	for _, point := range points {
		monthKey := point.Month.Format("2006-01")
		anchor, ok := anchors[monthKey]
		if !ok {
			anchor = fmpMonthAnchor{LastTS: point.PeriodEnd.Add(-time.Second), FirstTS: point.KnownAt, LastClose: point.AnchorValue}
		}
		rows = appendFMPObservation(rows, dataset, priceFactorCode, point.Price, point.Month, point.PeriodEnd, anchor, referenceSymbol, true)
		rows = appendFMPObservation(rows, dataset, "earnings", point.NominalEarnings, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		rows = appendFMPObservation(rows, dataset, "CPI", point.CPI, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		rows = appendFMPObservation(rows, dataset, "rate_GS10", point.RateGS10, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		rows = appendFMPObservation(rows, dataset, "real_sp", point.RealSP, point.Month, point.PeriodEnd, anchor, referenceSymbol, true)
		rows = appendFMPObservation(rows, dataset, "real_earnings", point.RealEarnings, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		if point.PE10 > 0 {
			rows = appendFMPObservation(rows, dataset, "pe10", point.PE10, point.Month, point.PeriodEnd, anchor, referenceSymbol, true)
		}
		if point.ExcessCAPEYield != 0 && !math.IsNaN(point.ExcessCAPEYield) && !math.IsInf(point.ExcessCAPEYield, 0) {
			rows = appendFMPObservation(rows, dataset, "excess_cape_yield", point.ExcessCAPEYield, point.Month, point.PeriodEnd, anchor, referenceSymbol, false)
		}
	}
	return rows
}

func appendFMPObservation(rows []ObservationRow, dataset, factor string, value float64, periodStart, periodEnd time.Time, anchor fmpMonthAnchor, referenceSymbol string, priceScaled bool) []ObservationRow {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return rows
	}
	anchorValue := math.NaN()
	if priceScaled {
		anchorValue = anchor.LastClose
	}
	return append(rows, ObservationRow{Dataset: dataset, FactorCode: factor, EventTS: anchor.LastTS, KnownAt: anchor.FirstTS, PeriodStart: periodStart, PeriodEnd: periodEnd, Source: fmpMacroSource, Value: value, ReferenceMarket: DefaultReferenceMarket, ReferenceSymbol: referenceSymbol, AnchorValue: anchorValue})
}

func loadFMPMonthAnchors(ctx context.Context, conn driver.Conn, referenceSymbol string, from, to time.Time) (map[string]fmpMonthAnchor, error) {
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
	out := map[string]fmpMonthAnchor{}
	for rows.Next() {
		var monthStart, lastTS, firstTS time.Time
		var lastClose float64
		if err := rows.Scan(&monthStart, &lastTS, &lastClose, &firstTS); err != nil {
			return nil, err
		}
		out[monthStart.UTC().Format("2006-01")] = fmpMonthAnchor{StartMonth: monthStart.UTC(), LastTS: lastTS.UTC(), LastClose: lastClose, FirstTS: firstTS.UTC()}
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

func loadFMPMacroLegacyMonthlySeries(ctx context.Context, conn driver.Conn, dataset, factor string, from, to time.Time) (map[string]float64, error) {
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

func latestFMPMacroMonthlyValue(series map[string]float64) float64 {
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

func averageFMPMacro(values []float64) float64 {
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

func parseFMPMacroKnownAt(values ...string) time.Time {
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

func parseFMPMacroDate(value string) (time.Time, bool) {
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

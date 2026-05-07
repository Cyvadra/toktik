package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

type coverageRecord struct {
	Underlying          string
	FallbackSymbol      string
	CandidateTried      string
	QuoteCovered        bool
	EODCovered          bool
	IntradayCovered     bool
	ResolvedVia         string
	IndexCandidates     string
	NoCoverageCategory  string
	QuoteError          string
	EODError            string
	IntradayError       string
	SearchSuggestions   string
	StockFallbackExists bool
}

type categorizedRecords struct {
	Order  []string
	Groups map[string][]coverageRecord
}

type coverageSummary struct {
	Tested              int
	AnyCoverage         int
	QuoteCoverage       int
	EODCoverage         int
	IntradayCoverage    int
	DirectResolved      int
	FallbackResolved    int
	SearchResolved      int
	NoCoverage          int
	RawMissingCount     int
	FallbackMappedCount int
	StillMissingCount   int
	OptionUnderlyingCnt int
	StockSymbolCnt      int
	TestSampleLimit     int
	OptionsSourceTable  string
	StocksSourceTable   string
	GeneratedAt         time.Time
	CoverageScope       string
	CoverageWindowStart string
	CoverageWindowEnd   string
	IntradayWindowStart string
	IntradayWindowEnd   string
}

func main() {
	runtimeCfg := cli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	optionsTable := flag.String("options-table", "us_options_bar_1d", "Source options table/view used to enumerate underlyings")
	stocksTable := flag.String("stocks-table", "us_stocks_bar_1d", "Source stocks table/view used to enumerate symbols")
	outputDir := flag.String("output-dir", "docs", "Directory where report artifacts are written")
	limit := flag.Int("limit", 25, "Optional limit for FMP coverage tests; 0 means test all missing symbols")
	skipFMP := flag.Bool("skip-fmp", false, "Only export ClickHouse symbol lists and gap report; skip FMP coverage calls")
	coverageScope := flag.String("coverage-scope", "still-missing", "Which missing universe to test against FMP: raw-missing or still-missing")
	eodDays := flag.Int("eod-days", 30, "How many trailing days to request from FMP historical-price-eod/full")
	intradayDays := flag.Int("intraday-days", 5, "How many trailing days to request from FMP historical-chart")
	intradayInterval := flag.String("intraday-interval", "1min", "FMP intraday interval (1min, 5min, 15min, 30min, 1hour, 4hour)")
	flag.Parse()

	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}

	optionUnderlyings, err := listDistinctStrings(ctx, conn, *optionsTable, "underlying")
	if err != nil {
		log.Fatalf("list option underlyings: %v", err)
	}
	stockSymbols, err := listDistinctStrings(ctx, conn, *stocksTable, "symbol")
	if err != nil {
		log.Fatalf("list stock symbols: %v", err)
	}

	missingRaw := difference(optionUnderlyings, stockSymbols)
	fallbackMapped, stillMissing := splitFallbackMapped(missingRaw, stockSymbols)

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	if err := writeLines(filepath.Join(*outputDir, "us-stocks-symbols-1d.txt"), stockSymbols); err != nil {
		log.Fatalf("write stock symbols: %v", err)
	}
	if err := writeLines(filepath.Join(*outputDir, "us-options-underlyings-1d.txt"), optionUnderlyings); err != nil {
		log.Fatalf("write option underlyings: %v", err)
	}
	if err := writeLines(filepath.Join(*outputDir, "us-options-missing-stock-underlyings-1d.txt"), missingRaw); err != nil {
		log.Fatalf("write raw missing underlyings: %v", err)
	}
	if err := writeLines(filepath.Join(*outputDir, "us-options-missing-stock-underlyings-fallback-mapped-1d.txt"), fallbackMapped); err != nil {
		log.Fatalf("write fallback-mapped underlyings: %v", err)
	}
	if err := writeLines(filepath.Join(*outputDir, "us-options-still-missing-stock-underlyings-1d.txt"), stillMissing); err != nil {
		log.Fatalf("write still-missing underlyings: %v", err)
	}

	summary := coverageSummary{
		RawMissingCount:     len(missingRaw),
		FallbackMappedCount: len(fallbackMapped),
		StillMissingCount:   len(stillMissing),
		OptionUnderlyingCnt: len(optionUnderlyings),
		StockSymbolCnt:      len(stockSymbols),
		TestSampleLimit:     *limit,
		OptionsSourceTable:  *optionsTable,
		StocksSourceTable:   *stocksTable,
		GeneratedAt:         time.Now().UTC(),
		CoverageScope:       normalizeCoverageScope(*coverageScope),
	}

	var records []coverageRecord
	if !*skipFMP {
		apiKey, err := runtimeCfg.FMPAPIKey()
		if err != nil {
			log.Fatalf("load FMP API key: %v", err)
		}
		if strings.TrimSpace(apiKey) == "" {
			log.Fatal("FMP API key is required unless --skip-fmp is set")
		}

		interval := fmp.IntradayInterval(strings.TrimSpace(*intradayInterval))
		if !isValidIntradayInterval(interval) {
			log.Fatalf("invalid --intraday-interval %q", *intradayInterval)
		}

		today := time.Now().UTC().Truncate(24 * time.Hour)
		eodFrom := today.AddDate(0, 0, -*eodDays).Format("2006-01-02")
		eodTo := today.Format("2006-01-02")
		intradayFrom := today.AddDate(0, 0, -*intradayDays).Format("2006-01-02")
		intradayTo := today.Format("2006-01-02")
		summary.CoverageWindowStart = eodFrom
		summary.CoverageWindowEnd = eodTo
		summary.IntradayWindowStart = intradayFrom
		summary.IntradayWindowEnd = intradayTo

		client := fmp.New(apiKey)
		testSymbols := selectCoverageSymbols(summary.CoverageScope, missingRaw, stillMissing)
		if *limit > 0 && *limit < len(testSymbols) {
			testSymbols = append([]string(nil), testSymbols[:*limit]...)
		}
		records = testFMP(ctx, client, testSymbols, stockSymbols, interval, eodFrom, eodTo, intradayFrom, intradayTo)
		summary.Tested = len(records)
		for _, record := range records {
			if !record.QuoteCovered && !record.EODCovered && !record.IntradayCovered {
				record.NoCoverageCategory = classifyNoCoverage(record)
			}
			if record.QuoteCovered {
				summary.QuoteCoverage++
			}
			if record.EODCovered {
				summary.EODCoverage++
			}
			if record.IntradayCovered {
				summary.IntradayCoverage++
			}
			if record.QuoteCovered || record.EODCovered || record.IntradayCovered {
				summary.AnyCoverage++
				switch record.ResolvedVia {
				case "direct":
					summary.DirectResolved++
				case "fallback":
					summary.FallbackResolved++
				case "search":
					summary.SearchResolved++
				}
			} else {
				summary.NoCoverage++
			}
		}

		csvPath := filepath.Join(*outputDir, "us-options-missing-stock-underlyings-fmp-coverage.csv")
		if err := writeCoverageCSV(csvPath, records); err != nil {
			log.Fatalf("write coverage csv: %v", err)
		}

		noCoveragePath := filepath.Join(*outputDir, "us-options-no-fmp-coverage-categories.md")
		if err := writeNoCoverageCategories(noCoveragePath, records); err != nil {
			log.Fatalf("write no-coverage category report: %v", err)
		}
	}

	mdPath := filepath.Join(*outputDir, "us-market-symbol-gap-report.md")
	if err := writeMarkdownSummary(mdPath, summary, records); err != nil {
		log.Fatalf("write markdown summary: %v", err)
	}

	log.Printf("US market symbol gap report complete: options=%d stocks=%d raw_missing=%d fallback_mapped=%d still_missing=%d tested=%d any_coverage=%d",
		summary.OptionUnderlyingCnt,
		summary.StockSymbolCnt,
		summary.RawMissingCount,
		summary.FallbackMappedCount,
		summary.StillMissingCount,
		summary.Tested,
		summary.AnyCoverage,
	)
}

func listDistinctStrings(ctx context.Context, conn clickhouse.Conn, table, column string) ([]string, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s != '' GROUP BY %s ORDER BY %s", column, table, column, column, column)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query %s.%s: %w", table, column, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan %s.%s: %w", table, column, err)
		}
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s.%s: %w", table, column, err)
	}
	return out, nil
}

func difference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	out := make([]string, 0)
	for _, value := range left {
		if _, ok := rightSet[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func splitFallbackMapped(missing, stockSymbols []string) ([]string, []string) {
	stockSet := make(map[string]struct{}, len(stockSymbols))
	for _, symbol := range stockSymbols {
		stockSet[symbol] = struct{}{}
	}

	fallbackMapped := make([]string, 0)
	stillMissing := make([]string, 0)
	for _, symbol := range missing {
		fallback, ok := usmarket.OptionUnderlyingFallbackStockSymbol(symbol)
		if ok {
			if _, exists := stockSet[fallback]; exists {
				fallbackMapped = append(fallbackMapped, symbol)
				continue
			}
		}
		stillMissing = append(stillMissing, symbol)
	}
	return fallbackMapped, stillMissing
}

func testFMP(ctx context.Context, client *fmp.Client, symbols, stockSymbols []string, interval fmp.IntradayInterval, eodFrom, eodTo, intradayFrom, intradayTo string) []coverageRecord {
	stockSet := make(map[string]struct{}, len(stockSymbols))
	for _, symbol := range stockSymbols {
		stockSet[symbol] = struct{}{}
	}
	indexSet, err := loadIndexSymbolSet(ctx, client)
	if err != nil {
		log.Printf("warn: load FMP index list: %v", err)
		indexSet = nil
	}
	records := make([]coverageRecord, 0, len(symbols))
	for _, underlying := range symbols {
		record := coverageRecord{Underlying: underlying}
		fallback, hasFallback := usmarket.OptionUnderlyingFallbackStockSymbol(underlying)
		if hasFallback {
			record.FallbackSymbol = fallback
			_, record.StockFallbackExists = stockSet[fallback]
		}

		candidates := make([]candidateSymbol, 0, 6)
		candidates = append(candidates, candidateSymbol{Symbol: underlying, Source: "direct"})
		if hasFallback {
			candidates = append(candidates, candidateSymbol{Symbol: fallback, Source: "fallback"})
		}
		indexCandidates := indexCoverageCandidates(underlying, indexSet)
		if len(indexCandidates) > 0 {
			record.IndexCandidates = strings.Join(extractCandidateSymbols(indexCandidates), ";")
			candidates = append(candidates, indexCandidates...)
		}

		searchResults, searchErr := client.SearchSymbol(ctx, underlying)
		if searchErr == nil {
			suggestions := make([]string, 0, len(searchResults))
			seen := make(map[string]struct{}, len(candidates))
			for _, candidate := range candidates {
				seen[candidate.Symbol] = struct{}{}
			}
			for _, result := range searchResults {
				symbol := strings.ToUpper(strings.TrimSpace(result.Symbol))
				if symbol == "" {
					continue
				}
				suggestions = append(suggestions, symbol)
				if _, ok := seen[symbol]; ok {
					continue
				}
				seen[symbol] = struct{}{}
				candidates = append(candidates, candidateSymbol{Symbol: symbol, Source: "search"})
				if len(candidates) >= 6 {
					break
				}
			}
			record.SearchSuggestions = strings.Join(uniqueStrings(suggestions), ";")
		} else {
			record.SearchSuggestions = "search-error: " + compactErr(searchErr)
		}

		best := record
		bestScore := -1
		for _, candidate := range candidates {
			current := record
			current.CandidateTried = candidate.Symbol
			current.ResolvedVia = candidate.Source

			quotes, quoteErr := client.QuoteShort(ctx, candidate.Symbol)
			current.QuoteCovered = len(quotes) > 0
			current.QuoteError = compactErr(quoteErr)

			eod, eodErr := client.HistoricalPrices(ctx, candidate.Symbol, eodFrom, eodTo)
			current.EODCovered = len(eod) > 0
			current.EODError = compactErr(eodErr)

			intraday, intradayErr := client.IntradayPrices(ctx, candidate.Symbol, interval, intradayFrom, intradayTo)
			current.IntradayCovered = len(intraday) > 0
			current.IntradayError = compactErr(intradayErr)

			score := coverageScore(current)
			if score > bestScore {
				best = current
				bestScore = score
			}
			if score == 3 {
				break
			}
		}
		records = append(records, best)
	}
	return records
}

type candidateSymbol struct {
	Symbol string
	Source string
}

func coverageScore(record coverageRecord) int {
	score := 0
	if record.QuoteCovered {
		score++
	}
	if record.EODCovered {
		score++
	}
	if record.IntradayCovered {
		score++
	}
	return score
}

func compactErr(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > 180 {
		return value[:180]
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func writeLines(path string, values []string) error {
	data := strings.Join(values, "\n")
	if len(values) > 0 {
		data += "\n"
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

func writeCoverageCSV(path string, records []coverageRecord) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{
		"underlying",
		"fallback_symbol",
		"stock_fallback_exists",
		"index_candidates",
		"no_coverage_category",
		"candidate_tried",
		"resolved_via",
		"quote_covered",
		"eod_covered",
		"intraday_covered",
		"quote_error",
		"eod_error",
		"intraday_error",
		"search_suggestions",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, record := range records {
		row := []string{
			record.Underlying,
			record.FallbackSymbol,
			fmt.Sprintf("%t", record.StockFallbackExists),
			record.IndexCandidates,
			record.NoCoverageCategory,
			record.CandidateTried,
			record.ResolvedVia,
			fmt.Sprintf("%t", record.QuoteCovered),
			fmt.Sprintf("%t", record.EODCovered),
			fmt.Sprintf("%t", record.IntradayCovered),
			record.QuoteError,
			record.EODError,
			record.IntradayError,
			record.SearchSuggestions,
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeMarkdownSummary(path string, summary coverageSummary, records []coverageRecord) error {
	var builder strings.Builder
	builder.WriteString("# US Market Symbol Gap Report\n\n")
	builder.WriteString(fmt.Sprintf("Generated at: %s UTC\n\n", summary.GeneratedAt.Format(time.RFC3339)))
	builder.WriteString("## ClickHouse Universe Diff\n\n")
	builder.WriteString(fmt.Sprintf("- Options source table: `%s`\n", summary.OptionsSourceTable))
	builder.WriteString(fmt.Sprintf("- Stocks source table: `%s`\n", summary.StocksSourceTable))
	builder.WriteString(fmt.Sprintf("- Distinct option underlyings: %d\n", summary.OptionUnderlyingCnt))
	builder.WriteString(fmt.Sprintf("- Distinct stock symbols: %d\n", summary.StockSymbolCnt))
	builder.WriteString(fmt.Sprintf("- Raw missing underlyings: %d\n", summary.RawMissingCount))
	builder.WriteString(fmt.Sprintf("- Raw-missing symbols that map to dotted stock symbols already in stocks table: %d\n", summary.FallbackMappedCount))
	builder.WriteString(fmt.Sprintf("- Still missing after dotted fallback normalization: %d\n\n", summary.StillMissingCount))
	builder.WriteString("Artifacts:\n")
	builder.WriteString("- `docs/us-stocks-symbols-1d.txt`\n")
	builder.WriteString("- `docs/us-options-underlyings-1d.txt`\n")
	builder.WriteString("- `docs/us-options-missing-stock-underlyings-1d.txt`\n")
	builder.WriteString("- `docs/us-options-missing-stock-underlyings-fallback-mapped-1d.txt`\n")
	builder.WriteString("- `docs/us-options-still-missing-stock-underlyings-1d.txt`\n\n")

	if summary.Tested > 0 {
		builder.WriteString("## FMP Coverage Sample\n\n")
		builder.WriteString(fmt.Sprintf("- Coverage scope: %s\n", summary.CoverageScope))
		builder.WriteString(fmt.Sprintf("- Tested symbols: %d\n", summary.Tested))
		builder.WriteString(fmt.Sprintf("- Any FMP coverage: %d\n", summary.AnyCoverage))
		builder.WriteString(fmt.Sprintf("- QuoteShort coverage: %d\n", summary.QuoteCoverage))
		builder.WriteString(fmt.Sprintf("- Historical EOD coverage: %d\n", summary.EODCoverage))
		builder.WriteString(fmt.Sprintf("- Intraday coverage: %d\n", summary.IntradayCoverage))
		builder.WriteString(fmt.Sprintf("- Resolved directly with raw underlying: %d\n", summary.DirectResolved))
		builder.WriteString(fmt.Sprintf("- Resolved via dotted fallback alias: %d\n", summary.FallbackResolved))
		builder.WriteString(fmt.Sprintf("- Resolved via FMP search suggestions: %d\n", summary.SearchResolved))
		indexResolved := summary.AnyCoverage - summary.DirectResolved - summary.FallbackResolved - summary.SearchResolved
		builder.WriteString(fmt.Sprintf("- Resolved via index candidates: %d\n", indexResolved))
		builder.WriteString(fmt.Sprintf("- No FMP coverage in sample: %d\n", summary.NoCoverage))
		builder.WriteString(fmt.Sprintf("- EOD window: %s .. %s\n", summary.CoverageWindowStart, summary.CoverageWindowEnd))
		builder.WriteString(fmt.Sprintf("- Intraday window: %s .. %s\n\n", summary.IntradayWindowStart, summary.IntradayWindowEnd))
		builder.WriteString("Top sample results:\n\n")
		builder.WriteString("| underlying | candidate | via | quote | eod | intraday | fallback_exists |\n")
		builder.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
		limit := len(records)
		if limit > 20 {
			limit = 20
		}
		for i := 0; i < limit; i++ {
			record := records[i]
			builder.WriteString(fmt.Sprintf("| %s | %s | %s | %t | %t | %t | %t |\n",
				record.Underlying,
				record.CandidateTried,
				record.ResolvedVia,
				record.QuoteCovered,
				record.EODCovered,
				record.IntradayCovered,
				record.StockFallbackExists,
			))
		}
		builder.WriteString("\nArtifacts:\n")
		builder.WriteString("- `docs/us-options-missing-stock-underlyings-fmp-coverage.csv`\n\n")
		groups := groupNoCoverage(records)
		if len(groups.Order) > 0 {
			builder.WriteString("## No-Coverage Breakdown\n\n")
			for _, category := range groups.Order {
				builder.WriteString(fmt.Sprintf("- %s: %d\n", category, len(groups.Groups[category])))
			}
			builder.WriteString("\nArtifacts:\n")
			builder.WriteString("- `docs/us-options-no-fmp-coverage-categories.md`\n\n")
		}
	}

	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func isValidIntradayInterval(iv fmp.IntradayInterval) bool {
	switch iv {
	case fmp.Interval1Min, fmp.Interval5Min, fmp.Interval15Min, fmp.Interval30Min, fmp.Interval1Hour, fmp.Interval4Hour:
		return true
	default:
		return false
	}
}

func normalizeCoverageScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "raw-missing":
		return "raw-missing"
	default:
		return "still-missing"
	}
}

func selectCoverageSymbols(scope string, missingRaw, stillMissing []string) []string {
	if scope == "raw-missing" {
		return missingRaw
	}
	return stillMissing
}

func loadIndexSymbolSet(ctx context.Context, client *fmp.Client) (map[string]struct{}, error) {
	indexes, err := client.IndexList(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(indexes))
	for _, index := range indexes {
		symbol := strings.ToUpper(strings.TrimSpace(index.Symbol))
		if symbol != "" {
			set[symbol] = struct{}{}
		}
	}
	return set, nil
}

func indexCoverageCandidates(underlying string, indexSet map[string]struct{}) []candidateSymbol {
	known := map[string][]string{
		"DJX":   {"^DJI"},
		"NDX":   {"^NDX", "^IXIC"},
		"NDXP":  {"^NDX", "^IXIC"},
		"NQX":   {"^NDX", "^IXIC"},
		"OEX":   {"^OEX"},
		"RUI":   {"^RUI"},
		"RUT":   {"^RUT"},
		"RUTW":  {"^RUT"},
		"MRUT":  {"^RUT"},
		"SOX":   {"^SOX"},
		"SOXPM": {"^SOX"},
		"SPIKE": {"^VIX"},
		"SPX":   {"^SPX", "^GSPC"},
		"SPXW":  {"^SPX", "^GSPC"},
		"VOLQ":  {"^VIX"},
		"VIX":   {"^VIX"},
		"VIXW":  {"^VIX"},
		"XAU":   {"^XAU"},
		"XEO":   {"^OEX"},
		"XND":   {"^NDX", "^IXIC"},
		"XSP":   {"^SPX", "^GSPC"},
	}

	seen := make(map[string]struct{})
	out := make([]candidateSymbol, 0, 4)
	appendCandidate := func(symbol string) {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			return
		}
		if _, ok := seen[symbol]; ok {
			return
		}
		if indexSet != nil {
			if _, ok := indexSet[symbol]; !ok {
				return
			}
		}
		seen[symbol] = struct{}{}
		out = append(out, candidateSymbol{Symbol: symbol, Source: "index"})
	}

	for _, symbol := range known[underlying] {
		appendCandidate(symbol)
	}
	if strings.HasPrefix(underlying, "^") {
		appendCandidate(underlying)
	} else if looksIndexLike(underlying) {
		appendCandidate("^" + underlying)
	}
	return out
}

func looksIndexLike(symbol string) bool {
	if len(symbol) < 3 || len(symbol) > 6 {
		return false
	}
	for _, r := range symbol {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func extractCandidateSymbols(candidates []candidateSymbol) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Symbol)
	}
	return out
}

func classifyNoCoverage(record coverageRecord) string {
	symbol := record.Underlying
	switch {
	case strings.Contains(symbol, "TEST") || strings.HasPrefix(symbol, "Z"):
		return "test-or-placeholder"
	case strings.HasSuffix(symbol, "Q"):
		return "delisted-or-bankruptcy-otc"
	case strings.HasSuffix(symbol, "W"):
		return "warrant-or-spac-derivative"
	case strings.HasSuffix(symbol, "Y") || strings.HasSuffix(symbol, "F"):
		return "foreign-adr-or-otc"
	case isCustomIndexLike(record):
		return "custom-or-unsupported-index"
	default:
		return "other-unresolved"
	}
}

func isCustomIndexLike(record coverageRecord) bool {
	indexLike := map[string]struct{}{
		"BKXPM": {},
		"FAANG": {},
		"MXEA":  {},
		"MXUSA": {},
		"MXWLD": {},
		"NANOS": {},
		"OSX":   {},
		"OSXPM": {},
		"PTNT":  {},
		"SIXB":  {},
		"SIXM":  {},
		"SIXV":  {},
		"SOXPM": {},
		"SPESG": {},
		"XDZ":   {},
		"ZZK":   {},
	}
	if _, ok := indexLike[record.Underlying]; ok {
		return true
	}
	return strings.Contains(record.IndexCandidates, "^")
}

func groupNoCoverage(records []coverageRecord) categorizedRecords {
	groups := categorizedRecords{
		Order: []string{
			"delisted-or-bankruptcy-otc",
			"warrant-or-spac-derivative",
			"foreign-adr-or-otc",
			"custom-or-unsupported-index",
			"test-or-placeholder",
			"other-unresolved",
		},
		Groups: make(map[string][]coverageRecord),
	}
	for _, record := range records {
		if record.QuoteCovered || record.EODCovered || record.IntradayCovered {
			continue
		}
		category := record.NoCoverageCategory
		if category == "" {
			category = classifyNoCoverage(record)
		}
		groups.Groups[category] = append(groups.Groups[category], record)
	}
	filtered := make([]string, 0, len(groups.Order))
	for _, category := range groups.Order {
		if len(groups.Groups[category]) > 0 {
			filtered = append(filtered, category)
		}
	}
	groups.Order = filtered
	return groups
}

func writeNoCoverageCategories(path string, records []coverageRecord) error {
	groups := groupNoCoverage(records)
	var builder strings.Builder
	builder.WriteString("# No FMP Coverage Categories\n\n")
	builder.WriteString("Symbols below had no QuoteShort, historical EOD, or intraday coverage after direct, fallback, search, and index candidate attempts.\n\n")
	for _, category := range groups.Order {
		items := groups.Groups[category]
		builder.WriteString(fmt.Sprintf("## %s (%d)\n\n", category, len(items)))
		for _, record := range items {
			if record.SearchSuggestions != "" || record.IndexCandidates != "" {
				builder.WriteString(fmt.Sprintf("- %s: index_candidates=%s search=%s\n", record.Underlying, emptyDash(record.IndexCandidates), emptyDash(record.SearchSuggestions)))
			} else {
				builder.WriteString(fmt.Sprintf("- %s\n", record.Underlying))
			}
		}
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

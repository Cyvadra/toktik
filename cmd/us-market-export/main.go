package main

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

type exportFile struct {
	Name        string
	Description string
	Query       string
	Args        []any
}

type exportResult struct {
	Name string
	Path string
	Rows uint64
}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: us-market-export --symbols AAPL,MSFT --start-date 2024-01-01 --end-date 2024-01-31 [flags]

Exports a compact offline bundle for investor delivery. The command does not expose an API endpoint and does not emit JSON. Output files are flat csv.gz: simple schema, good compression, and easy consumption from Python, R, Excel, DuckDB, ClickHouse, or pandas.

The export intentionally contains atomic tables only. Date-specific option selection, expiry/moneyness/volume filters, and other derived contract universes should be handled by the recipient from the exported rows.

Files written to --output-dir:
	manifest.txt              Export parameters, source tables, file list, row counts.
	stocks_bars.csv.gz        Stock OHLCV bars for requested symbols.
	option_contracts.csv.gz   Distinct option contracts observed in the requested date range.
	options_bars.csv.gz       Option OHLCV, underlying close, and Greek bars for requested underlyings.

Date filters are inclusive. --symbols is interpreted as stock symbols for stocks_bars and option underlyings for option exports.

Examples:
	go run ./cmd/us-market-export \
		--symbols AAPL,MSFT,SPY \
		--start-date 2024-01-01 \
		--end-date 2024-01-31 \
		--interval 1m \
		--output-dir exports/investor-jan-2024

	go run ./cmd/us-market-export \
		--symbols AAPL,MSFT \
		--start-date 2024-01-01 \
		--end-date 2024-01-31 \
		--include-stocks=false

Supported intervals: 1m,5m,15m,30m,1h,2h,4h,1d. Higher interval views are regular-session aggregates. Use --regular-session-only only with --interval=1m.

The command reads ClickHouse configuration from toktik.yaml or CLICKHOUSE_DSN. --clickhouse-dsn overrides it for one run.

Flags:
`)
		flag.PrintDefaults()
	}
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbolsFlag := flag.String("symbols", "", "Comma-separated US stock/option underlying symbols to export, e.g. AAPL,MSFT,SPY")
	startDateFlag := flag.String("start-date", "", "Inclusive market date start (YYYY-MM-DD)")
	endDateFlag := flag.String("end-date", "", "Inclusive market date end (YYYY-MM-DD); defaults to --start-date")
	intervalFlag := flag.String("interval", "1m", "Bar interval to export (1m,5m,15m,30m,1h,2h,4h,1d)")
	outputDir := flag.String("output-dir", "", "Output directory; defaults to exports/us-market-<symbols>-<start>-<end>-<interval>")
	regularOnly := flag.Bool("regular-session-only", false, "Export regular-session rows only. Only valid for 1m data because higher interval views are already regular-session aggregates")
	includeStocks := flag.Bool("include-stocks", true, "Export stock bars")
	includeContracts := flag.Bool("include-option-contracts", true, "Export distinct option contracts seen in the date range")
	includeOptions := flag.Bool("include-option-bars", true, "Export option bars")
	flag.Parse()

	symbols := parseSymbols(*symbolsFlag)
	if len(symbols) == 0 {
		fatalUsage("--symbols is required")
	}
	if strings.TrimSpace(*startDateFlag) == "" {
		fatalUsage("--start-date is required")
	}
	startDate := appCli.ParseDate(*startDateFlag, "--start-date")
	endDate := startDate
	if strings.TrimSpace(*endDateFlag) != "" {
		endDate = appCli.ParseDate(*endDateFlag, "--end-date")
	}
	if endDate.Before(startDate) {
		log.Fatal("--end-date must be on or after --start-date")
	}
	interval := strings.ToLower(strings.TrimSpace(*intervalFlag))
	stockTable, err := resolveIntervalTable(chquery.USStockIntervals, interval, "US stock")
	if err != nil {
		log.Fatal(err)
	}
	optionTable, err := resolveIntervalTable(chquery.USOptionIntervals, interval, "US option")
	if err != nil {
		log.Fatal(err)
	}
	if *regularOnly && interval != "1m" {
		log.Fatal("--regular-session-only is only valid with --interval=1m; higher interval views are already regular-session aggregates")
	}
	if !*includeStocks && !*includeContracts && !*includeOptions {
		log.Fatal("at least one export section must be enabled")
	}

	dir := strings.TrimSpace(*outputDir)
	if dir == "" {
		dir = defaultOutputDir(symbols, startDate, endDate, interval)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}

	files := buildExportFiles(symbols, startDate, endDate, interval, stockTable, optionTable, *regularOnly, *includeStocks, *includeContracts, *includeOptions)
	results := make([]exportResult, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.Name+".csv.gz")
		rows, err := exportQueryCSVGzip(ctx, conn, file.Query, file.Args, path)
		if err != nil {
			log.Fatalf("export %s: %v", file.Name, err)
		}
		results = append(results, exportResult{Name: file.Name, Path: path, Rows: rows})
		log.Printf("exported %s rows=%d path=%s", file.Name, rows, path)
	}
	if err := writeManifest(filepath.Join(dir, "manifest.txt"), symbols, startDate, endDate, interval, stockTable, optionTable, *regularOnly, files, results); err != nil {
		log.Fatalf("write manifest: %v", err)
	}
	log.Printf("US market export complete: files=%d dir=%s", len(results), dir)
}

func buildExportFiles(symbols []string, startDate, endDate time.Time, interval, stockTable, optionTable string, regularOnly, includeStocks, includeContracts, includeOptions bool) []exportFile {
	args := []any{
		clickhouse.Named("symbols", symbols),
		clickhouse.Named("start_date", startDate.Format("2006-01-02")),
		clickhouse.Named("end_date", endDate.Format("2006-01-02")),
	}
	files := make([]exportFile, 0, 3)
	if includeStocks {
		files = append(files, exportFile{
			Name:        "stocks_bars",
			Description: "Stock OHLCV bars for requested symbols and market dates.",
			Query:       stockBarsQuery(stockTable, interval, regularOnly),
			Args:        args,
		})
	}
	if includeContracts {
		files = append(files, exportFile{
			Name:        "option_contracts",
			Description: "Distinct option contracts observed for requested underlyings and market dates.",
			Query:       optionContractsQuery(optionTable),
			Args:        args,
		})
	}
	if includeOptions {
		files = append(files, exportFile{
			Name:        "options_bars",
			Description: "Option OHLCV and Greek bars for requested underlyings and market dates.",
			Query:       optionBarsQuery(optionTable, interval, regularOnly),
			Args:        args,
		})
	}
	return files
}

func stockBarsQuery(tableName, interval string, regularOnly bool) string {
	regularCondition := ""
	if interval == "1m" && regularOnly {
		regularCondition = "\n  AND is_regular_session = 1"
	}
	return fmt.Sprintf(`SELECT
    timestamp,
	toDate(timestamp) AS market_date,
    symbol,
    open,
    high,
    low,
    close,
    toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol IN ({symbols:Array(String)})
	AND toDate(timestamp) >= toDate({start_date:String})
	AND toDate(timestamp) <= toDate({end_date:String})%s
ORDER BY symbol, timestamp`, tableName, regularCondition)
}

func optionContractsQuery(tableName string) string {
	return fmt.Sprintf(`SELECT
    underlying,
    symbol,
    CAST(anyLast(option_type), 'String') AS option_type,
    anyLast(expiration) AS expiration,
    anyLast(strike) AS strike,
    min(timestamp) AS first_timestamp,
    max(timestamp) AS last_timestamp,
	min(toDate(timestamp)) AS first_market_date,
	max(toDate(timestamp)) AS last_market_date,
    count() AS bar_count
FROM %s
WHERE underlying IN ({symbols:Array(String)})
	AND toDate(timestamp) >= toDate({start_date:String})
	AND toDate(timestamp) <= toDate({end_date:String})
GROUP BY underlying, symbol
ORDER BY underlying, expiration, strike, option_type, symbol`, tableName)
}

func optionBarsQuery(tableName, interval string, regularOnly bool) string {
	regularCondition := ""
	if interval == "1m" && regularOnly {
		regularCondition = "\n  AND is_regular_session = 1"
	}
	return fmt.Sprintf(`SELECT
    timestamp,
	toDate(timestamp) AS market_date,
    underlying,
    symbol,
    CAST(option_type, 'String') AS option_type,
    expiration,
    strike,
    open,
    high,
    low,
    close,
    underlying_close,
    implied_volatility,
    delta,
    gamma,
    vega,
    theta,
    rho,
    toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE underlying IN ({symbols:Array(String)})
	AND toDate(timestamp) >= toDate({start_date:String})
	AND toDate(timestamp) <= toDate({end_date:String})%s
ORDER BY underlying, symbol, timestamp`, tableName, regularCondition)
}

func exportQueryCSVGzip(ctx context.Context, conn driver.Conn, query string, args []any, path string) (uint64, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query ClickHouse: %w", err)
	}
	defer rows.Close()

	file, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", path, err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	csvWriter := csv.NewWriter(gzipWriter)
	defer csvWriter.Flush()

	columns := rows.Columns()
	if err := csvWriter.Write(columns); err != nil {
		return 0, fmt.Errorf("write header: %w", err)
	}

	columnTypes := rows.ColumnTypes()
	dest := make([]any, len(columns))
	for i := range columns {
		dest[i] = newScanDestination(columnTypes[i])
	}

	var count uint64
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return count, fmt.Errorf("scan row: %w", err)
		}
		record := make([]string, len(dest))
		for i, value := range dest {
			record[i] = formatCSVValue(derefScanValue(value))
		}
		if err := csvWriter.Write(record); err != nil {
			return count, fmt.Errorf("write row: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("iterate rows: %w", err)
	}
	if err := csvWriter.Error(); err != nil {
		return count, fmt.Errorf("flush csv: %w", err)
	}
	return count, nil
}

func newScanDestination(columnType driver.ColumnType) any {
	scanType := columnType.ScanType()
	if scanType == nil {
		var value any
		return &value
	}
	return reflect.New(scanType).Interface()
}

func derefScanValue(value any) any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return value
	}
	return rv.Elem().Interface()
}

func formatCSVValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case time.Time:
		if v.Hour() == 0 && v.Minute() == 0 && v.Second() == 0 && v.Nanosecond() == 0 {
			return v.Format("2006-01-02")
		}
		return v.UTC().Format(time.RFC3339)
	case float32:
		return formatFloat(float64(v), 32)
	case float64:
		return formatFloat(v, 64)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func formatFloat(value float64, bits int) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "+Inf"
	}
	if math.IsInf(value, -1) {
		return "-Inf"
	}
	return strconv.FormatFloat(value, 'g', -1, bits)
}

func writeManifest(path string, symbols []string, startDate, endDate time.Time, interval, stockTable, optionTable string, regularOnly bool, files []exportFile, results []exportResult) error {
	var builder strings.Builder
	builder.WriteString("Toktik US market export\n")
	builder.WriteString("generated_at=" + time.Now().UTC().Format(time.RFC3339) + "\n")
	builder.WriteString("symbols=" + strings.Join(symbols, ",") + "\n")
	builder.WriteString("start_date=" + startDate.Format("2006-01-02") + "\n")
	builder.WriteString("end_date=" + endDate.Format("2006-01-02") + "\n")
	builder.WriteString("interval=" + interval + "\n")
	builder.WriteString("stock_table=" + stockTable + "\n")
	builder.WriteString("option_table=" + optionTable + "\n")
	builder.WriteString("regular_session_only=" + strconv.FormatBool(regularOnly) + "\n")
	builder.WriteString("format=csv.gz\n\n")
	builder.WriteString("files\n")
	fileDescriptions := make(map[string]string, len(files))
	for _, file := range files {
		fileDescriptions[file.Name] = file.Description
	}
	for _, result := range results {
		builder.WriteString(fmt.Sprintf("- name=%s rows=%d path=%s description=%s\n", result.Name, result.Rows, filepath.Base(result.Path), fileDescriptions[result.Name]))
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func resolveIntervalTable(tables map[string]string, interval, label string) (string, error) {
	table, ok := tables[interval]
	if !ok {
		keys := make([]string, 0, len(tables))
		for key := range tables {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return "", fmt.Errorf("unsupported %s interval %q (supported: %s)", label, interval, strings.Join(keys, ","))
	}
	return table, nil
}

func parseSymbols(raw string) []string {
	parts := strings.Split(raw, ",")
	symbols := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		symbol := strings.ToUpper(strings.TrimSpace(part))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func defaultOutputDir(symbols []string, startDate, endDate time.Time, interval string) string {
	symbolPart := strings.ToLower(strings.Join(symbols, "-"))
	if len(symbolPart) > 80 {
		symbolPart = fmt.Sprintf("%d-symbols", len(symbols))
	}
	return filepath.Join("exports", fmt.Sprintf("us-market-%s-%s-%s-%s", symbolPart, startDate.Format("20060102"), endDate.Format("20060102"), interval))
}

func fatalUsage(message string) {
	fmt.Fprintf(os.Stderr, "%s\n\n", message)
	fmt.Fprintln(os.Stderr, "Usage: us-market-export --symbols AAPL,MSFT --start-date 2024-01-01 --end-date 2024-01-31 [flags]")
	os.Exit(2)
}

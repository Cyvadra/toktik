package usexport

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/service"
)

type Config struct {
	Symbols                     []string
	StartDate                   time.Time
	EndDate                     time.Time
	Interval                    string
	OutputDir                   string
	RegularSessionOnly          bool
	AllowAmbiguousCryptoSymbols bool
	IncludeStocks               bool
	IncludeOptionContracts      bool
	IncludeOptionBars           bool
}

type File struct {
	Name        string
	Description string
	Query       string
	Args        []any
}

type Result struct {
	Name string
	Path string
	Rows uint64
}

type BundleResult struct {
	OutputDir string
	Files     []Result
}

func Run(ctx context.Context, conn driver.Conn, cfg Config) (BundleResult, error) {
	cfg.Symbols = NormalizeSymbols(cfg.Symbols)
	if len(cfg.Symbols) == 0 {
		return BundleResult{}, fmt.Errorf("symbols is required")
	}
	if cfg.StartDate.IsZero() {
		return BundleResult{}, fmt.Errorf("start date is required")
	}
	if cfg.EndDate.IsZero() {
		cfg.EndDate = cfg.StartDate
	}
	if cfg.EndDate.Before(cfg.StartDate) {
		return BundleResult{}, fmt.Errorf("end date must be on or after start date")
	}
	if !cfg.AllowAmbiguousCryptoSymbols {
		if symbol, ok := firstAmbiguousCryptoAssetSymbol(cfg.Symbols); ok {
			return BundleResult{}, fmt.Errorf("symbol %q is a bare crypto asset symbol, but us-market-export reads US stock/option tables; use a crypto spot export path for the asset, or pass allow ambiguous crypto symbols only if you intentionally want the US-listed ticker %q", symbol, symbol)
		}
	}
	cfg.Interval = strings.ToLower(strings.TrimSpace(cfg.Interval))
	if cfg.Interval == "" {
		cfg.Interval = "1m"
	}
	stockTable, err := ResolveIntervalTable(chquery.USStockIntervals, cfg.Interval, "US stock")
	if err != nil {
		return BundleResult{}, err
	}
	optionTable, err := ResolveIntervalTable(chquery.USOptionIntervals, cfg.Interval, "US option")
	if err != nil {
		return BundleResult{}, err
	}
	if cfg.RegularSessionOnly && cfg.Interval != "1m" {
		return BundleResult{}, fmt.Errorf("regular-session-only is only valid with interval=1m; higher interval views are already regular-session aggregates")
	}
	if !cfg.IncludeStocks && !cfg.IncludeOptionContracts && !cfg.IncludeOptionBars {
		return BundleResult{}, fmt.Errorf("at least one export section must be enabled")
	}

	dir := strings.TrimSpace(cfg.OutputDir)
	if dir == "" {
		dir = DefaultOutputDir(cfg.Symbols, cfg.StartDate, cfg.EndDate, cfg.Interval)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BundleResult{}, fmt.Errorf("create output dir: %w", err)
	}

	files := BuildFiles(cfg.Symbols, cfg.StartDate, cfg.EndDate, cfg.Interval, stockTable, optionTable, cfg.RegularSessionOnly, cfg.IncludeStocks, cfg.IncludeOptionContracts, cfg.IncludeOptionBars)
	results := make([]Result, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.Name+".csv.gz")
		var rows uint64
		var err error
		if file.Name == "stocks_bars" && containsSymbol(cfg.Symbols, "VIX") {
			rows, err = exportStockBarsViaService(ctx, conn, cfg.Symbols, cfg.StartDate, cfg.EndDate, cfg.Interval, cfg.RegularSessionOnly, path)
		} else {
			rows, err = ExportQueryCSVGzip(ctx, conn, file.Query, file.Args, path)
		}
		if err != nil {
			return BundleResult{}, fmt.Errorf("export %s: %w", file.Name, err)
		}
		results = append(results, Result{Name: file.Name, Path: path, Rows: rows})
	}
	if err := WriteManifest(filepath.Join(dir, "manifest.txt"), cfg.Symbols, cfg.StartDate, cfg.EndDate, cfg.Interval, stockTable, optionTable, cfg.RegularSessionOnly, files, results); err != nil {
		return BundleResult{}, fmt.Errorf("write manifest: %w", err)
	}
	return BundleResult{OutputDir: dir, Files: results}, nil
}

func BuildFiles(symbols []string, startDate, endDate time.Time, interval, stockTable, optionTable string, regularOnly, includeStocks, includeContracts, includeOptions bool) []File {
	args := []any{
		clickhouse.Named("symbols", symbols),
		clickhouse.Named("start_date", startDate.Format("2006-01-02")),
		clickhouse.Named("end_date", endDate.Format("2006-01-02")),
	}
	files := make([]File, 0, 3)
	if includeStocks {
		files = append(files, File{Name: "stocks_bars", Description: "Stock OHLCV bars for requested symbols and market dates.", Query: StockBarsQuery(stockTable, interval, regularOnly), Args: args})
	}
	if includeContracts {
		files = append(files, File{Name: "option_contracts", Description: "Distinct option contracts observed for requested underlyings and market dates.", Query: OptionContractsQuery(optionTable), Args: args})
	}
	if includeOptions {
		files = append(files, File{Name: "options_bars", Description: "Option OHLCV and Greek bars for requested underlyings and market dates.", Query: OptionBarsQuery(optionTable, interval, regularOnly), Args: args})
	}
	return files
}

func StockBarsQuery(tableName, interval string, regularOnly bool) string {
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

func OptionContractsQuery(tableName string) string {
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

func OptionBarsQuery(tableName, interval string, regularOnly bool) string {
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

func ExportQueryCSVGzip(ctx context.Context, conn driver.Conn, query string, args []any, path string) (uint64, error) {
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
			record[i] = FormatCSVValue(derefScanValue(value))
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

func exportStockBarsViaService(ctx context.Context, conn driver.Conn, symbols []string, startDate, endDate time.Time, interval string, regularOnly bool, path string) (uint64, error) {
	file, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", path, err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	csvWriter := csv.NewWriter(gzipWriter)
	defer csvWriter.Flush()

	if err := csvWriter.Write([]string{"timestamp", "market_date", "symbol", "open", "high", "low", "close", "volume", "transactions"}); err != nil {
		return 0, fmt.Errorf("write header: %w", err)
	}

	svc := service.NewUSStocksService(chrepo.NewRepo(conn))
	toExclusive := endDate.AddDate(0, 0, 1).Format("2006-01-02")
	session := ""
	if interval == "1m" {
		if regularOnly {
			session = "regular"
		} else {
			session = "all"
		}
	}

	var count uint64
	for _, symbol := range symbols {
		cursor := ""
		for {
			resp, err := svc.QueryBars(ctx, dto.USStockBarRequest{Symbol: symbol, Interval: interval, From: startDate.Format("2006-01-02"), To: toExclusive, Session: session, Limit: 10000, Cursor: cursor})
			if err != nil {
				return count, fmt.Errorf("query stock bars for %s: %w", symbol, err)
			}
			for _, row := range resp.Data {
				record := []string{FormatCSVValue(row.Timestamp), row.Timestamp.UTC().Format("2006-01-02"), row.Symbol, FormatCSVValue(row.Open), FormatCSVValue(row.High), FormatCSVValue(row.Low), FormatCSVValue(row.Close), FormatCSVValue(row.Volume), FormatCSVValue(row.Transactions)}
				if err := csvWriter.Write(record); err != nil {
					return count, fmt.Errorf("write row: %w", err)
				}
				count++
			}
			if resp.NextCursor == "" {
				break
			}
			cursor = resp.NextCursor
		}
	}
	if err := csvWriter.Error(); err != nil {
		return count, fmt.Errorf("flush csv: %w", err)
	}
	return count, nil
}

func WriteManifest(path string, symbols []string, startDate, endDate time.Time, interval, stockTable, optionTable string, regularOnly bool, files []File, results []Result) error {
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

func ResolveIntervalTable(tables map[string]string, interval, label string) (string, error) {
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

func NormalizeSymbols(rawSymbols []string) []string {
	symbols := make([]string, 0, len(rawSymbols))
	seen := make(map[string]struct{}, len(rawSymbols))
	for _, raw := range rawSymbols {
		for _, part := range strings.Split(raw, ",") {
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
	}
	sort.Strings(symbols)
	return symbols
}

func DefaultOutputDir(symbols []string, startDate, endDate time.Time, interval string) string {
	symbolPart := strings.ToLower(strings.Join(symbols, "-"))
	if len(symbolPart) > 80 {
		symbolPart = fmt.Sprintf("%d-symbols", len(symbols))
	}
	return filepath.Join("exports", fmt.Sprintf("us-market-%s-%s-%s-%s", symbolPart, startDate.Format("20060102"), endDate.Format("20060102"), interval))
}

func FormatCSVValue(value any) string {
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

func containsSymbol(symbols []string, target string) bool {
	for _, symbol := range symbols {
		if strings.EqualFold(strings.TrimSpace(symbol), target) {
			return true
		}
	}
	return false
}

func firstAmbiguousCryptoAssetSymbol(symbols []string) (string, bool) {
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if _, ok := ambiguousCryptoAssetSymbols[symbol]; ok {
			return symbol, true
		}
	}
	return "", false
}

var ambiguousCryptoAssetSymbols = map[string]struct{}{
	"BTC":  {},
	"ETH":  {},
	"SOL":  {},
	"XRP":  {},
	"DOGE": {},
	"ADA":  {},
	"BNB":  {},
	"LTC":  {},
	"BCH":  {},
}

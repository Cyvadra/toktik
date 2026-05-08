package usmarket

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	defaultDialTimeout = 30 * time.Second
	defaultReadTimeout = 30 * time.Minute
)

type FlatFileAssetState struct {
	AssetClass   string
	TableName    string
	HasData      bool
	LastImported time.Time
}

// ConnectClickHouse establishes a connection to ClickHouse.
func ConnectClickHouse(ctx context.Context, dsn string) (driver.Conn, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse ClickHouse DSN: %w", err)
	}
	if opts.DialTimeout == 0 || opts.DialTimeout < defaultDialTimeout {
		opts.DialTimeout = defaultDialTimeout
	}
	if opts.ReadTimeout == 0 || opts.ReadTimeout < defaultReadTimeout {
		opts.ReadTimeout = defaultReadTimeout
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return conn, nil
}

// InitSchema reads and executes a DDL SQL file.
func InitSchema(ctx context.Context, conn driver.Conn, ddlPath string) error {
	if err := execSchemaFile(ctx, conn, ddlPath); err != nil {
		return err
	}
	if err := ensureMarketVolumeColumns(ctx, conn); err != nil {
		return err
	}
	return nil
}

// InitFundamentalsSchema reads and executes the fundamentals DDL without
// applying us_market-specific compatibility migrations.
func InitFundamentalsSchema(ctx context.Context, conn driver.Conn, ddlPath string) error {
	return execSchemaFile(ctx, conn, ddlPath)
}

func execSchemaFile(ctx context.Context, conn driver.Conn, ddlPath string) error {
	data, err := os.ReadFile(ddlPath)
	if err != nil {
		return fmt.Errorf("read DDL file %s: %w", ddlPath, err)
	}

	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if commentIndex := strings.Index(line, "--"); commentIndex >= 0 {
			line = line[:commentIndex]
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		filtered = append(filtered, line)
	}

	parts := strings.Split(strings.Join(filtered, "\n"), ";")
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		if err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec DDL: %w\nStatement: %s", err, stmt)
		}
	}
	return nil
}

func ensureMarketVolumeColumns(ctx context.Context, conn driver.Conn) error {
	stmts := []string{
		"ALTER TABLE us_options_bar_1m MODIFY COLUMN volume Float64",
		"ALTER TABLE us_stocks_bar_1m MODIFY COLUMN volume Float64",
	}
	for _, stmt := range stmts {
		if err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure us market volume columns: %w", err)
		}
	}
	return nil
}

const optionBarInsertSQL = `INSERT INTO us_options_bar_1m (
	timestamp, symbol, underlying, option_type, expiration, strike,
	open, high, low, close,
	underlying_close, implied_volatility, delta, gamma, vega, theta, rho,
	volume, transactions,
	market_date, session_kind, is_regular_session, session_open, session_seq
)`

const stockBarInsertSQL = `INSERT INTO us_stocks_bar_1m (
	timestamp, symbol, open, high, low, close, volume, transactions,
	market_date, session_kind, is_regular_session, session_open, session_seq
)`

// InsertOptionBars batch-inserts option bars from a channel into us_options_bar_1m.
func InsertOptionBars(ctx context.Context, conn driver.Conn, bars <-chan OptionBar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, optionBarInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare option batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.Underlying,
			bar.OptionType,
			bar.Expiration,
			bar.Strike,
			bar.Open, bar.High, bar.Low, bar.Close,
			bar.UnderlyingClose,
			bar.ImpliedVolatility,
			bar.Delta,
			bar.Gamma,
			bar.Vega,
			bar.Theta,
			bar.Rho,
			bar.Volume,
			bar.Transactions,
			bar.MarketDate,
			bar.SessionKind,
			bar.IsRegularSession,
			bar.SessionOpen,
			bar.SessionSeq,
		); err != nil {
			return totalRows, fmt.Errorf("append option row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send option batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d option rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, optionBarInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next option batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final option batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d option rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}

// InsertStockBars batch-inserts stock bars from a channel into us_stocks_bar_1m.
func InsertStockBars(ctx context.Context, conn driver.Conn, bars <-chan StockBar1m, batchSize int) (int64, error) {
	var totalRows int64

	batch, err := conn.PrepareBatch(ctx, stockBarInsertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare stock batch: %w", err)
	}

	batchCount := 0
	for bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.Open, bar.High, bar.Low, bar.Close,
			bar.Volume,
			bar.Transactions,
			bar.MarketDate,
			bar.SessionKind,
			bar.IsRegularSession,
			bar.SessionOpen,
			bar.SessionSeq,
		); err != nil {
			return totalRows, fmt.Errorf("append stock row: %w", err)
		}

		batchCount++
		totalRows++

		if batchCount >= batchSize {
			if err := batch.Send(); err != nil {
				return totalRows, fmt.Errorf("send stock batch: %w", err)
			}
			log.Printf("[clickhouse] inserted %d stock rows (total: %d)", batchCount, totalRows)
			batchCount = 0

			batch, err = conn.PrepareBatch(ctx, stockBarInsertSQL)
			if err != nil {
				return totalRows, fmt.Errorf("prepare next stock batch: %w", err)
			}
		}
	}

	if batchCount > 0 {
		if err := batch.Send(); err != nil {
			return totalRows, fmt.Errorf("send final stock batch: %w", err)
		}
		log.Printf("[clickhouse] inserted final %d stock rows (total: %d)", batchCount, totalRows)
	}

	return totalRows, nil
}

// CountExistingOptionBars checks if option data already exists for a given market date.
func CountExistingOptionBars(ctx context.Context, conn driver.Conn, marketDate time.Time) (uint64, error) {
	var count uint64
	err := conn.QueryRow(ctx,
		`SELECT count() FROM us_options_bar_1m WHERE market_date = ?`,
		marketDate,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count existing option bars: %w", err)
	}
	return count, nil
}

// CountExistingStockBars checks if stock data already exists for a given market date.
func CountExistingStockBars(ctx context.Context, conn driver.Conn, marketDate time.Time) (uint64, error) {
	var count uint64
	err := conn.QueryRow(ctx,
		`SELECT count() FROM us_stocks_bar_1m WHERE market_date = ?`,
		marketDate,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count existing stock bars: %w", err)
	}
	return count, nil
}

func ReplaceOptionMarketDate(ctx context.Context, conn driver.Conn, marketDate time.Time) error {
	return ReplaceOptionMarketDates(ctx, conn, []time.Time{marketDate})
}

func ReplaceStockMarketDate(ctx context.Context, conn driver.Conn, marketDate time.Time) error {
	return ReplaceStockMarketDates(ctx, conn, []time.Time{marketDate})
}

func ReplaceOptionMarketDates(ctx context.Context, conn driver.Conn, marketDates []time.Time) error {
	dates := normalizeUniqueUTCDates(marketDates)
	if len(dates) == 0 {
		return nil
	}
	dateArgs := make([]string, 0, len(dates))
	for _, date := range dates {
		dateArgs = append(dateArgs, date.Format("2006-01-02"))
	}
	if err := conn.Exec(ctx, `ALTER TABLE us_options_bar_1m DELETE WHERE market_date IN {dates:Array(Date)} SETTINGS mutations_sync = 1`, clickhouse.Named("dates", dateArgs)); err != nil {
		return fmt.Errorf("delete option base rows: %w", err)
	}
	for _, iv := range KlineIntervals {
		aggTable := "us_options_bar_" + iv.Suffix + "_agg"
		if err := deleteAggRowsByDates(ctx, conn, aggTable, dateArgs); err != nil {
			return fmt.Errorf("delete option aggregate %s: %w", iv.Suffix, err)
		}
	}
	for _, iv := range DefaultChainCacheIntervals {
		aggTable := "us_options_chain_" + iv.Suffix + "_agg"
		if err := deleteAggRowsByDates(ctx, conn, aggTable, dateArgs); err != nil {
			return fmt.Errorf("delete option chain aggregate %s: %w", iv.Suffix, err)
		}
	}
	return nil
}

func ReplaceStockMarketDates(ctx context.Context, conn driver.Conn, marketDates []time.Time) error {
	dates := normalizeUniqueUTCDates(marketDates)
	if len(dates) == 0 {
		return nil
	}
	dateArgs := make([]string, 0, len(dates))
	for _, date := range dates {
		dateArgs = append(dateArgs, date.Format("2006-01-02"))
	}
	if err := conn.Exec(ctx, `ALTER TABLE us_stocks_bar_1m DELETE WHERE market_date IN {dates:Array(Date)} SETTINGS mutations_sync = 1`, clickhouse.Named("dates", dateArgs)); err != nil {
		return fmt.Errorf("delete stock base rows: %w", err)
	}
	for _, iv := range KlineIntervals {
		aggTable := "us_stocks_bar_" + iv.Suffix + "_agg"
		if err := deleteAggRowsByDates(ctx, conn, aggTable, dateArgs); err != nil {
			return fmt.Errorf("delete stock aggregate %s: %w", iv.Suffix, err)
		}
	}
	return nil
}

// DeleteStockBarsSymbolScope deletes rows from us_stocks_bar_1m for the given
// symbol within [from, to). Both from and to are optional; to is exclusive.
// This is used by the FMP kline re-import (--replace) to clear stale 1m rows
// before re-inserting fresh data from FMP.
func DeleteStockBarsSymbolScope(ctx context.Context, conn driver.Conn, symbol string, from, to time.Time) error {
	if strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("symbol is required")
	}
	parts := []string{"symbol = {symbol:String}"}
	args := []interface{}{clickhouse.Named("symbol", strings.ToUpper(strings.TrimSpace(symbol)))}
	if !from.IsZero() {
		parts = append(parts, "market_date >= {from:Date}")
		args = append(args, clickhouse.Named("from", normalizeUTCDay(from).Format("2006-01-02")))
	}
	if !to.IsZero() {
		parts = append(parts, "market_date < {to:Date}")
		args = append(args, clickhouse.Named("to", normalizeUTCDay(to).Format("2006-01-02")))
	}
	where := "WHERE " + strings.Join(parts, " AND ")
	if err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE us_stocks_bar_1m DELETE %s SETTINGS mutations_sync = 1", where), args...); err != nil {
		return fmt.Errorf("delete stock 1m rows for %s: %w", symbol, err)
	}
	return nil
}

func deleteAggRowsByDates(ctx context.Context, conn driver.Conn, table string, marketDates []string) error {
	if len(marketDates) == 0 {
		return nil
	}
	query := fmt.Sprintf("ALTER TABLE %s DELETE WHERE toDate(ts, 'UTC') IN {dates:Array(Date)} SETTINGS mutations_sync = 1", table)
	if err := conn.Exec(ctx, query, clickhouse.Named("dates", marketDates)); err != nil {
		return fmt.Errorf("delete aggregate rows by dates: %w", err)
	}
	return nil
}

func LatestOptionMarketDate(ctx context.Context, conn driver.Conn) (time.Time, bool, error) {
	return latestMarketDate(ctx, conn, "us_options_bar_1m")
}

func LatestStockMarketDate(ctx context.Context, conn driver.Conn) (time.Time, bool, error) {
	return latestMarketDate(ctx, conn, "us_stocks_bar_1m")
}

func InspectFlatFileAssetStates(ctx context.Context, conn driver.Conn) ([]FlatFileAssetState, error) {
	assets := []FlatFileAssetState{
		{AssetClass: "stocks", TableName: "us_stocks_bar_1m"},
		{AssetClass: "options", TableName: "us_options_bar_1m"},
	}

	for idx := range assets {
		exists, err := tableExists(ctx, conn, assets[idx].TableName)
		if err != nil {
			return nil, fmt.Errorf("inspect %s table %s: %w", assets[idx].AssetClass, assets[idx].TableName, err)
		}
		if !exists {
			continue
		}

		latest, hasData, err := latestMarketDate(ctx, conn, assets[idx].TableName)
		if err != nil {
			return nil, fmt.Errorf("inspect latest %s market date: %w", assets[idx].AssetClass, err)
		}
		assets[idx].HasData = hasData
		assets[idx].LastImported = latest
	}

	return assets, nil
}

func tableExists(ctx context.Context, conn driver.Conn, tableName string) (bool, error) {
	rows, err := conn.Query(ctx, `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = ?`, tableName)
	if err != nil {
		return false, fmt.Errorf("query system.tables: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return false, rows.Err()
	}

	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, fmt.Errorf("scan system.tables count: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate system.tables count: %w", err)
	}
	return count > 0, nil
}

func latestMarketDate(ctx context.Context, conn driver.Conn, table string) (time.Time, bool, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT count(), ifNull(maxOrNull(market_date), toDate('1970-01-01')) FROM %s`, table))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest market date from %s: %w", table, err)
	}
	defer rows.Close()

	var (
		count      uint64
		marketDate time.Time
	)
	for rows.Next() {
		if err := rows.Scan(&count, &marketDate); err != nil {
			return time.Time{}, false, fmt.Errorf("scan latest market date from %s: %w", table, err)
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, fmt.Errorf("iterate latest market date from %s: %w", table, err)
	}
	if count == 0 {
		return time.Time{}, false, nil
	}
	return normalizeUTCDay(marketDate), true, nil
}

// EnsureOptionGreeksColumns makes the option base table compatible with greek enrichment
// when the table already existed before these columns were introduced.
func EnsureOptionGreeksColumns(ctx context.Context, conn driver.Conn) error {
	stmts := []string{
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS underlying_close Float32 AFTER close`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS implied_volatility Float32 AFTER underlying_close`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS delta Float32 AFTER implied_volatility`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS gamma Float32 AFTER delta`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS vega Float32 AFTER gamma`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS theta Float32 AFTER vega`,
		`ALTER TABLE us_options_bar_1m ADD COLUMN IF NOT EXISTS rho Float32 AFTER theta`,
	}

	for _, stmt := range stmts {
		if err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure option greeks columns: %w", err)
		}
	}

	return nil
}

// LoadStockCloseMap loads stock close series for a given market date.
func LoadStockCloseMap(ctx context.Context, conn driver.Conn, symbols []string, marketDate time.Time) (stockCloseSeries, map[string]struct{}, error) {
	stockCloses := make(stockCloseSeries)
	seenSymbols := make(map[string]struct{}, len(symbols))

	if len(symbols) == 0 {
		return stockCloses, seenSymbols, nil
	}

	if err := loadStockCloseRows(ctx, conn, symbols, marketDate, nil, stockCloses, seenSymbols); err != nil {
		return nil, nil, err
	}

	missingSymbols := MissingStockSymbols(symbols, seenSymbols)
	if len(missingSymbols) > 0 {
		fallbackSymbols := make([]string, 0, len(missingSymbols))
		fallbackAliases := make(map[string]string, len(missingSymbols))
		for _, symbol := range missingSymbols {
			fallback, ok := OptionUnderlyingFallbackStockSymbol(symbol)
			if !ok {
				continue
			}
			fallbackSymbols = append(fallbackSymbols, fallback)
			fallbackAliases[fallback] = symbol
		}

		if len(fallbackSymbols) > 0 {
			if err := loadStockCloseRows(ctx, conn, fallbackSymbols, marketDate, fallbackAliases, stockCloses, seenSymbols); err != nil {
				return nil, nil, err
			}
		}
	}

	return stockCloses, seenSymbols, nil
}

func loadStockCloseRows(ctx context.Context, conn driver.Conn, symbols []string, marketDate time.Time, aliasToUnderlying map[string]string, stockCloses stockCloseSeries, seenSymbols map[string]struct{}) error {
	if len(symbols) == 0 {
		return nil
	}

	rows, err := conn.Query(ctx,
		`SELECT symbol, timestamp, close
		FROM us_stocks_bar_1m
		WHERE symbol IN ({symbols:Array(String)})
		  AND market_date = {market_date:Date}
		ORDER BY symbol, timestamp`,
		clickhouse.Named("symbols", symbols),
		clickhouse.Named("market_date", marketDate.Format("2006-01-02")),
	)
	if err != nil {
		return fmt.Errorf("query stock closes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			symbol    string
			timestamp time.Time
			close     float32
		)
		if err := rows.Scan(&symbol, &timestamp, &close); err != nil {
			return fmt.Errorf("scan stock close row: %w", err)
		}

		underlying := symbol
		if aliasToUnderlying != nil {
			if aliased, ok := aliasToUnderlying[symbol]; ok {
				underlying = aliased
			}
		}

		seenSymbols[underlying] = struct{}{}
		stockCloses[underlying] = append(stockCloses[underlying], stockClosePoint{
			timestamp: timestamp.UTC().Unix(),
			close:     float64(close),
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stock close rows: %w", err)
	}

	return nil
}

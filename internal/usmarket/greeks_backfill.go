package usmarket

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const missingOptionGreeksPredicate = `(isNaN(underlying_close) OR isNaN(implied_volatility) OR isNaN(delta) OR isNaN(gamma) OR isNaN(vega) OR isNaN(theta) OR isNaN(rho))`

type MissingOptionGreeksTask struct {
	MarketDate       time.Time
	Underlying       string
	MissingRows      uint64
	MissingContracts uint64
}

type OptionContract struct {
	Underlying string
	Expiration time.Time
	Strike     float64
	OptionType string
}

type OptionGreeksBackfillStats struct {
	RowsScanned        int
	RowsMatched        int
	RowsBackfilled     int
	ContractsMatched   int
	ContractsUnmatched int
	NoData             bool
}

type OptionGreeksBackfillConfig struct {
	Conn              driver.Conn
	DSN               string
	StartDate         time.Time
	EndDate           time.Time
	Underlyings       []string
	Workers           int
	BatchSize         int
	LimitTasks        int
	RiskFreeRate      float64
	DryRun            bool
	RebuildAggregates bool
}

type OptionGreeksBackfillResult struct {
	ProcessedTasks     int64
	FailedTasks        int64
	MatchedRows        int64
	BackfilledRows     int64
	MatchedContracts   int64
	UnmatchedContracts int64
	RemainingTasks     int
}

type optionContractKey struct {
	expiration  string
	strikeMilli int64
	optionType  string
}

func makeOptionContractKey(expiration time.Time, strike float64, optionType string) optionContractKey {
	expiration = normalizeDateOnly(expiration)
	return optionContractKey{
		expiration:  expiration.Format("2006-01-02"),
		strikeMilli: int64(strike*1000 + 0.5),
		optionType:  strings.ToUpper(optionType),
	}
}

func normalizeDateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func ListMissingOptionGreeksTasks(ctx context.Context, conn driver.Conn, startDate, endDate time.Time, underlyings []string, limit int) ([]MissingOptionGreeksTask, error) {
	query := `SELECT market_date, underlying, count() AS missing_rows,
		uniqExact(tuple(expiration, strike, option_type)) AS missing_contracts
	FROM us_options_bar_1m
	WHERE market_date >= {start_date:Date}
	  AND market_date <= {end_date:Date}
	  AND ` + missingOptionGreeksPredicate

	args := []any{
		clickhouse.Named("start_date", startDate.Format("2006-01-02")),
		clickhouse.Named("end_date", endDate.Format("2006-01-02")),
	}
	if len(underlyings) > 0 {
		query += ` AND underlying IN ({underlyings:Array(String)})`
		args = append(args, clickhouse.Named("underlyings", underlyings))
	}
	query += ` GROUP BY market_date, underlying ORDER BY market_date, underlying`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query missing option greek tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]MissingOptionGreeksTask, 0)
	for rows.Next() {
		var task MissingOptionGreeksTask
		if err := rows.Scan(&task.MarketDate, &task.Underlying, &task.MissingRows, &task.MissingContracts); err != nil {
			return nil, fmt.Errorf("scan missing option greek task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing option greek tasks: %w", err)
	}

	return tasks, nil
}

func LoadOptionBarsNeedingBackfill(ctx context.Context, conn driver.Conn, task MissingOptionGreeksTask) ([]OptionBar1m, error) {
	rows, err := conn.Query(ctx,
		`SELECT timestamp, symbol, underlying, option_type, expiration, strike,
			open, high, low, close,
			underlying_close, implied_volatility, delta, gamma, vega, theta, rho,
			volume, transactions,
			market_date, session_kind, is_regular_session, session_open, session_seq
		FROM us_options_bar_1m
		WHERE market_date = {market_date:Date}
		  AND underlying = {underlying:String}
		  AND `+missingOptionGreeksPredicate+`
		ORDER BY expiration, strike, option_type, timestamp`,
		clickhouse.Named("market_date", task.MarketDate.Format("2006-01-02")),
		clickhouse.Named("underlying", task.Underlying),
	)
	if err != nil {
		return nil, fmt.Errorf("query option bars needing backfill: %w", err)
	}
	defer rows.Close()

	optionBars := make([]OptionBar1m, 0)
	for rows.Next() {
		var bar OptionBar1m
		if err := rows.Scan(
			&bar.Timestamp,
			&bar.Symbol,
			&bar.Underlying,
			&bar.OptionType,
			&bar.Expiration,
			&bar.Strike,
			&bar.Open,
			&bar.High,
			&bar.Low,
			&bar.Close,
			&bar.UnderlyingClose,
			&bar.ImpliedVolatility,
			&bar.Delta,
			&bar.Gamma,
			&bar.Vega,
			&bar.Theta,
			&bar.Rho,
			&bar.Volume,
			&bar.Transactions,
			&bar.MarketDate,
			&bar.SessionKind,
			&bar.IsRegularSession,
			&bar.SessionOpen,
			&bar.SessionSeq,
		); err != nil {
			return nil, fmt.Errorf("scan option bar needing backfill: %w", err)
		}
		optionBars = append(optionBars, bar)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate option bars needing backfill: %w", err)
	}

	return optionBars, nil
}

func BackfillMissingOptionGreeks(ctx context.Context, cfg OptionGreeksBackfillConfig) (OptionGreeksBackfillResult, error) {
	if cfg.Conn == nil {
		return OptionGreeksBackfillResult{}, fmt.Errorf("clickhouse connection is required")
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return OptionGreeksBackfillResult{}, fmt.Errorf("clickhouse DSN is required")
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100000
	}
	if cfg.EndDate.Before(cfg.StartDate) {
		return OptionGreeksBackfillResult{}, fmt.Errorf("end date must be on or after start date")
	}

	tasks, err := ListMissingOptionGreeksTasks(ctx, cfg.Conn, cfg.StartDate, cfg.EndDate, cfg.Underlyings, cfg.LimitTasks)
	if err != nil {
		return OptionGreeksBackfillResult{}, err
	}
	if len(tasks) == 0 {
		return OptionGreeksBackfillResult{}, nil
	}

	log.Printf("Found %d underlying/day tasks to backfill between %s and %s", len(tasks), cfg.StartDate.Format("2006-01-02"), cfg.EndDate.Format("2006-01-02"))

	taskCh := make(chan MissingOptionGreeksTask)
	var (
		wg                 sync.WaitGroup
		processedTasks     int64
		failedTasks        int64
		matchedRows        int64
		backfilledRows     int64
		matchedContracts   int64
		unmatchedContracts int64
	)

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerConn, err := ConnectClickHouse(ctx, cfg.DSN)
			if err != nil {
				log.Printf("[ERROR] worker %d connect ClickHouse: %v", workerID, err)
				atomic.AddInt64(&failedTasks, int64(len(tasks)))
				return
			}

			for task := range taskCh {
				stats, err := BackfillOptionGreeksFromLocalData(ctx, workerConn, task, cfg.BatchSize, GreeksConfig{RiskFreeRate: cfg.RiskFreeRate}, cfg.DryRun)
				if err != nil {
					log.Printf("[ERROR] %s %s: %v", task.MarketDate.Format("2006-01-02"), task.Underlying, err)
					atomic.AddInt64(&failedTasks, 1)
					continue
				}

				atomic.AddInt64(&processedTasks, 1)
				atomic.AddInt64(&matchedRows, int64(stats.RowsMatched))
				atomic.AddInt64(&backfilledRows, int64(stats.RowsBackfilled))
				atomic.AddInt64(&matchedContracts, int64(stats.ContractsMatched))
				atomic.AddInt64(&unmatchedContracts, int64(stats.ContractsUnmatched))

				mode := "BACKFILLED"
				if cfg.DryRun {
					mode = "DRYRUN"
				}
				if stats.NoData {
					mode = "SKIPPED"
				}
				log.Printf("[%s] %s %s: scanned=%d matched_rows=%d backfilled_rows=%d matched_contracts=%d unmatched_contracts=%d",
					mode,
					task.MarketDate.Format("2006-01-02"),
					task.Underlying,
					stats.RowsScanned,
					stats.RowsMatched,
					stats.RowsBackfilled,
					stats.ContractsMatched,
					stats.ContractsUnmatched,
				)
			}
		}(i + 1)
	}

	for _, task := range tasks {
		taskCh <- task
	}
	close(taskCh)
	wg.Wait()

	remainingTasks, err := ListMissingOptionGreeksTasks(ctx, cfg.Conn, cfg.StartDate, cfg.EndDate, cfg.Underlyings, 0)
	if err != nil {
		return OptionGreeksBackfillResult{}, fmt.Errorf("re-check missing option greek tasks: %w", err)
	}

	result := OptionGreeksBackfillResult{
		ProcessedTasks:     processedTasks,
		FailedTasks:        failedTasks,
		MatchedRows:        matchedRows,
		BackfilledRows:     backfilledRows,
		MatchedContracts:   matchedContracts,
		UnmatchedContracts: unmatchedContracts,
		RemainingTasks:     len(remainingTasks),
	}

	if failedTasks > 0 {
		return result, fmt.Errorf("local option greek backfill finished with %d failed tasks", failedTasks)
	}

	if !cfg.DryRun && cfg.RebuildAggregates && backfilledRows > 0 {
		log.Printf("Rebuilding higher-interval option kline + chain cache aggregates from updated 1m greeks")
		if err := RebuildOptionKlineAggregates(ctx, cfg.Conn); err != nil {
			return result, fmt.Errorf("rebuild option kline aggregates: %w", err)
		}
		if err := RebuildOptionChainCaches(ctx, cfg.Conn); err != nil {
			return result, fmt.Errorf("rebuild option chain caches: %w", err)
		}
	}

	return result, nil
}

func BackfillOptionGreeksFromLocalData(ctx context.Context, conn driver.Conn, task MissingOptionGreeksTask, batchSize int, cfg GreeksConfig, dryRun bool) (OptionGreeksBackfillStats, error) {
	rows, err := LoadOptionBarsNeedingBackfill(ctx, conn, task)
	if err != nil {
		return OptionGreeksBackfillStats{}, err
	}

	stats := OptionGreeksBackfillStats{RowsScanned: len(rows)}
	if len(rows) == 0 {
		stats.NoData = true
		return stats, nil
	}

	stockCloses, _, err := LoadStockCloseMap(ctx, conn, []string{task.Underlying}, task.MarketDate)
	if err != nil {
		return stats, fmt.Errorf("load stock closes: %w", err)
	}

	updatedRows, originalRows, matchedContracts, unmatchedContracts := ApplyCalculatedGreeks(rows, stockCloses, cfg)
	stats.RowsMatched = len(updatedRows)
	stats.ContractsMatched = len(matchedContracts)
	stats.ContractsUnmatched = len(unmatchedContracts)
	if len(updatedRows) == 0 {
		stats.NoData = true
		return stats, nil
	}
	if dryRun {
		return stats, nil
	}

	if err := DeleteOptionBarsByIdentity(ctx, conn, originalRows); err != nil {
		return stats, err
	}

	if _, err := InsertOptionBarSlice(ctx, conn, updatedRows, batchSize); err != nil {
		if _, restoreErr := InsertOptionBarSlice(ctx, conn, originalRows, batchSize); restoreErr != nil {
			return stats, fmt.Errorf("insert updated option bars: %w (restore original rows failed: %v)", err, restoreErr)
		}
		return stats, fmt.Errorf("insert updated option bars: %w (original rows restored)", err)
	}

	stats.RowsBackfilled = len(updatedRows)
	return stats, nil
}

func ApplyCalculatedGreeks(rows []OptionBar1m, stockCloses stockCloseSeries, cfg GreeksConfig) ([]OptionBar1m, []OptionBar1m, []OptionContract, []OptionContract) {
	updatedRows := make([]OptionBar1m, 0, len(rows))
	originalRows := make([]OptionBar1m, 0, len(rows))
	matchedContracts := make(map[optionContractKey]OptionContract)
	unmatchedContracts := make(map[optionContractKey]OptionContract)

	for _, row := range rows {
		contract := OptionContract{
			Underlying: row.Underlying,
			Expiration: row.Expiration,
			Strike:     row.Strike,
			OptionType: row.OptionType,
		}
		key := makeOptionContractKey(contract.Expiration, contract.Strike, contract.OptionType)

		underlyingClose, ok := stockCloses.Lookup(row.Underlying, row.Timestamp)
		if !ok {
			unmatchedContracts[key] = contract
			continue
		}

		greeks := calculateOptionGreeks(
			underlyingClose,
			row.Strike,
			float64(row.Close),
			row.OptionType,
			row.Timestamp,
			row.Expiration,
			cfg,
		)
		if !isFiniteOptionGreeks(greeks) {
			unmatchedContracts[key] = contract
			continue
		}

		originalRows = append(originalRows, row)
		row.UnderlyingClose = float32(underlyingClose)
		row.ImpliedVolatility = float32(greeks.ImpliedVolatility)
		row.Delta = float32(greeks.Delta)
		row.Gamma = float32(greeks.Gamma)
		row.Vega = float32(greeks.Vega)
		row.Theta = float32(greeks.Theta)
		row.Rho = float32(greeks.Rho)
		updatedRows = append(updatedRows, row)
		matchedContracts[key] = contract
	}

	return updatedRows, originalRows, sortContracts(matchedContracts), sortContracts(unmatchedContracts)
}

func isFiniteOptionGreeks(greeks optionGreeks) bool {
	values := []float64{
		greeks.ImpliedVolatility,
		greeks.Delta,
		greeks.Gamma,
		greeks.Vega,
		greeks.Theta,
		greeks.Rho,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func InsertOptionBarSlice(ctx context.Context, conn driver.Conn, rows []OptionBar1m, batchSize int) (int64, error) {
	ch := make(chan OptionBar1m, len(rows))
	for _, row := range rows {
		ch <- row
	}
	close(ch)
	return InsertOptionBars(ctx, conn, ch, batchSize)
}

func DeleteOptionBarsByIdentity(ctx context.Context, conn driver.Conn, rows []OptionBar1m) error {
	if len(rows) == 0 {
		return nil
	}

	const rowsPerMutation = 200
	for start := 0; start < len(rows); start += rowsPerMutation {
		end := start + rowsPerMutation
		if end > len(rows) {
			end = len(rows)
		}
		if err := deleteOptionBarRowChunk(ctx, conn, rows[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func deleteOptionBarRowChunk(ctx context.Context, conn driver.Conn, rows []OptionBar1m) error {
	clauses := make([]string, 0, len(rows))
	args := make([]any, 0, 1+len(rows)*2)
	args = append(args, rows[0].MarketDate.Format("2006-01-02"))

	for _, row := range rows {
		clauses = append(clauses, `(symbol = ? AND timestamp = ?)`)
		args = append(args, row.Symbol, row.Timestamp)
	}

	query := `ALTER TABLE us_options_bar_1m DELETE
		WHERE market_date = ?
		  AND ` + missingOptionGreeksPredicate + `
		  AND (` + strings.Join(clauses, ` OR `) + `)
		SETTINGS mutations_sync = 1`

	if err := conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("delete option bars for greek backfill rows: %w", err)
	}
	return nil
}

func sortContracts(values map[optionContractKey]OptionContract) []OptionContract {
	contracts := make([]OptionContract, 0, len(values))
	for _, contract := range values {
		contracts = append(contracts, contract)
	}
	sort.Slice(contracts, func(i, j int) bool {
		if !contracts[i].Expiration.Equal(contracts[j].Expiration) {
			return contracts[i].Expiration.Before(contracts[j].Expiration)
		}
		if contracts[i].Strike != contracts[j].Strike {
			return contracts[i].Strike < contracts[j].Strike
		}
		return contracts[i].OptionType < contracts[j].OptionType
	})
	return contracts
}

func ResolveCSVDateRange(paths []string) (time.Time, time.Time, bool, error) {
	if len(paths) == 0 {
		return time.Time{}, time.Time{}, false, nil
	}

	var (
		minDate time.Time
		maxDate time.Time
	)
	for _, path := range paths {
		marketDate, err := ExtractDateFromFilename(path)
		if err != nil {
			return time.Time{}, time.Time{}, false, err
		}
		if minDate.IsZero() || marketDate.Before(minDate) {
			minDate = marketDate
		}
		if maxDate.IsZero() || marketDate.After(maxDate) {
			maxDate = marketDate
		}
	}

	return minDate, maxDate, true, nil
}
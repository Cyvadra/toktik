package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/Cyvadra/toktik/internal/chquery"
)

const (
	turnoverIntersectionPoolTable                   = "turnover_intersection_pool_daily"
	turnoverIntersectionPoolCandidateKeep           = 100
	turnoverIntersectionPoolHistoryBufferMultiplier = 2
)

type turnoverIntersectionPoolRow struct {
	Market              string
	LookbackDays        int
	NonETFOnly          bool
	AsOfDate            time.Time
	Underlying          string
	StockTurnoverUSD    float64
	OptionTurnoverUSD   float64
	CombinedTurnoverUSD float64
	Rank                uint32
}

func (s *UniverseService) ensureTurnoverIntersectionPool(ctx context.Context, market string, lookbackDays []int, nonETFOnly bool, from, to time.Time, force bool) error {
	if s.repo == nil {
		return fmt.Errorf("turnover intersection source materialization requires ClickHouse repository")
	}
	if !to.After(from) {
		return nil
	}
	for _, lookback := range lookbackDays {
		fillFrom := from
		if force {
			if err := s.deleteTurnoverIntersectionPoolRange(ctx, market, lookback, nonETFOnly, from, to); err != nil {
				return err
			}
		} else {
			latest, ok, err := s.latestTurnoverIntersectionPoolDate(ctx, market, lookback, nonETFOnly)
			if err != nil {
				return err
			}
			if ok && !latest.Before(from) {
				fillFrom = latest.AddDate(0, 0, 1)
			}
		}
		if !to.After(fillFrom) {
			slog.Info("turnover intersection source already covered", "market", market, "lookback_days", lookback, "non_etf_only", nonETFOnly, "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"))
			continue
		}
		startedAt := time.Now()
		slog.Info("turnover intersection source materialization started", "market", market, "lookback_days", lookback, "non_etf_only", nonETFOnly, "from", fillFrom.Format("2006-01-02"), "to", to.Format("2006-01-02"), "force", force)
		rows, err := s.materializeTurnoverIntersectionPoolRange(ctx, market, lookback, nonETFOnly, fillFrom, to)
		if err != nil {
			return err
		}
		slog.Info("turnover intersection source materialization completed", "market", market, "lookback_days", lookback, "non_etf_only", nonETFOnly, "from", fillFrom.Format("2006-01-02"), "to", to.Format("2006-01-02"), "rows", rows, "latency_ms", time.Since(startedAt).Milliseconds())
	}
	return nil
}

func (s *UniverseService) latestTurnoverIntersectionPoolDate(ctx context.Context, market string, lookbackDays int, nonETFOnly bool) (time.Time, bool, error) {
	query := fmt.Sprintf(`SELECT maxOrNull(as_of_date) FROM %s WHERE market = %s AND lookback_days = %s AND non_etf_only = %s`, turnoverIntersectionPoolTable, clickhouseStringLiteral(market), clickhouseUInt32Literal(lookbackDays), clickhouseBoolLiteral(nonETFOnly))
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query turnover intersection source coverage: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var latest *time.Time
	if err := rows.Scan(&latest); err != nil {
		return time.Time{}, false, fmt.Errorf("scan turnover intersection source coverage: %w", err)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, fmt.Errorf("iterate turnover intersection source coverage: %w", err)
	}
	if latest == nil {
		return time.Time{}, false, nil
	}
	return normalizeUniverseDate(*latest, *latest), true, nil
}

func (s *UniverseService) deleteTurnoverIntersectionPoolRange(ctx context.Context, market string, lookbackDays int, nonETFOnly bool, from, to time.Time) error {
	query := fmt.Sprintf(`ALTER TABLE %s DELETE
WHERE market = %s
	AND lookback_days = %s
	AND non_etf_only = %s
	AND as_of_date >= toDate(%s)
	AND as_of_date < toDate(%s)
SETTINGS mutations_sync = 1`, turnoverIntersectionPoolTable, clickhouseStringLiteral(market), clickhouseUInt32Literal(lookbackDays), clickhouseBoolLiteral(nonETFOnly), clickhouseStringLiteral(from.Format("2006-01-02")), clickhouseStringLiteral(to.Format("2006-01-02")))
	if err := s.repo.Exec(ctx, query); err != nil {
		return fmt.Errorf("delete turnover intersection source range: %w", err)
	}
	return nil
}

func (s *UniverseService) materializeTurnoverIntersectionPoolRange(ctx context.Context, market string, lookbackDays int, nonETFOnly bool, from, to time.Time) (int, error) {
	scanFrom := from.AddDate(0, 0, -lookbackDays*turnoverIntersectionPoolHistoryBufferMultiplier)
	stockUniverseFilter := ""
	if nonETFOnly {
		stockUniverseFilter = usStocksFundamentalsUniverseFilterClause("b.symbol")
	}
	query := fmt.Sprintf(`
WITH
stock_bars AS (
	SELECT
		toDate(b.timestamp) AS day,
		b.symbol AS underlying,
		%s AS join_underlying,
		%s AS adjusted_close,
		toFloat64(b.volume) AS volume
	FROM us_stocks_bar_1d AS b
	%s
	WHERE b.timestamp >= toDateTime(%s, 'UTC')
		AND b.timestamp < toDateTime(%s, 'UTC')
		%s
	GROUP BY b.symbol, b.timestamp, b.close, b.volume
),
stock_daily AS (
	SELECT
		day,
		underlying,
		join_underlying,
		sum(adjusted_close * volume) AS turnover_usd
	FROM stock_bars
	GROUP BY day, underlying, join_underlying
),
stock_rolling AS (
	SELECT
		day,
		underlying,
		join_underlying,
		sum(turnover_usd) OVER (PARTITION BY join_underlying ORDER BY day ROWS BETWEEN %d PRECEDING AND CURRENT ROW) AS stock_turnover_usd
	FROM stock_daily
),
option_daily AS (
	SELECT
		toDate(timestamp) AS day,
		underlying AS join_underlying,
		sum(toFloat64(close) * toFloat64(volume) * 100.0) AS turnover_usd
	FROM us_options_bar_1d
	WHERE timestamp >= toDateTime(%s, 'UTC')
		AND timestamp < toDateTime(%s, 'UTC')
	GROUP BY day, join_underlying
),
option_rolling AS (
	SELECT
		day,
		join_underlying,
		sum(turnover_usd) OVER (PARTITION BY join_underlying ORDER BY day ROWS BETWEEN %d PRECEDING AND CURRENT ROW) AS option_turnover_usd
	FROM option_daily
),
ranked AS (
	SELECT
		s.day,
		s.underlying,
		s.stock_turnover_usd,
		o.option_turnover_usd,
		s.stock_turnover_usd + o.option_turnover_usd AS combined_turnover_usd,
		row_number() OVER (PARTITION BY s.day ORDER BY s.stock_turnover_usd + o.option_turnover_usd DESC, s.underlying ASC) AS rank
	FROM stock_rolling AS s
	INNER JOIN option_rolling AS o ON o.day = s.day AND o.join_underlying = s.join_underlying
	WHERE s.day >= toDate(%s)
		AND s.day < toDate(%s)
)
SELECT day, underlying, stock_turnover_usd, option_turnover_usd, combined_turnover_usd
FROM ranked
WHERE rank <= %d
ORDER BY day ASC, combined_turnover_usd DESC, underlying ASC`, stockUnderlyingOptionAliasExpr("b.symbol"), chquery.USStockAdjustedPriceSQL("b", "close", "sp"), chquery.USStockSplitJoinSQL("b", "sp"), clickhouseStringLiteral(scanFrom.Format("2006-01-02")), clickhouseStringLiteral(to.Format("2006-01-02")), stockUniverseFilter, lookbackDays-1, clickhouseStringLiteral(scanFrom.Format("2006-01-02")), clickhouseStringLiteral(to.Format("2006-01-02")), lookbackDays-1, clickhouseStringLiteral(from.Format("2006-01-02")), clickhouseStringLiteral(to.Format("2006-01-02")), turnoverIntersectionPoolCandidateKeep)
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("query turnover intersection source rows: %w", err)
	}
	defer rows.Close()
	candidates := make([]turnoverIntersectionPoolRow, 0)
	for rows.Next() {
		var row turnoverIntersectionPoolRow
		if err := rows.Scan(&row.AsOfDate, &row.Underlying, &row.StockTurnoverUSD, &row.OptionTurnoverUSD, &row.CombinedTurnoverUSD); err != nil {
			return 0, fmt.Errorf("scan turnover intersection source row: %w", err)
		}
		row.Market = market
		row.LookbackDays = lookbackDays
		row.NonETFOnly = nonETFOnly
		row.Underlying = normalizeSymbol(row.Underlying)
		if row.Underlying == "" {
			continue
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate turnover intersection source rows: %w", err)
	}
	if nonETFOnly && s.etfClassifier != nil {
		candidates = s.filterTurnoverIntersectionPoolETFs(ctx, candidates)
	} else if nonETFOnly && s.etfClassifier == nil {
		slog.Warn("turnover intersection source ETF classifier is not configured; using fundamentals-only filter", "market", market, "lookback_days", lookbackDays)
	}
	ranked := rankTurnoverIntersectionPoolRows(candidates)
	if err := s.insertTurnoverIntersectionPoolRows(ctx, ranked); err != nil {
		return 0, err
	}
	return len(ranked), nil
}

func (s *UniverseService) filterTurnoverIntersectionPoolETFs(ctx context.Context, rows []turnoverIntersectionPoolRow) []turnoverIntersectionPoolRow {
	if len(rows) == 0 {
		return rows
	}
	seen := make(map[string]struct{})
	symbols := make([]string, 0)
	for _, row := range rows {
		if _, ok := seen[row.Underlying]; ok {
			continue
		}
		seen[row.Underlying] = struct{}{}
		symbols = append(symbols, row.Underlying)
	}
	excluded, err := s.etfClassifier.IsETFLikeBySymbol(ctx, symbols)
	if err != nil {
		slog.Warn("turnover intersection source ETF classifier failed; keeping unclassified rows", "error", err)
		return rows
	}
	filtered := make([]turnoverIntersectionPoolRow, 0, len(rows))
	for _, row := range rows {
		if excluded[row.Underlying] {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func rankTurnoverIntersectionPoolRows(rows []turnoverIntersectionPoolRow) []turnoverIntersectionPoolRow {
	byDay := make(map[time.Time][]turnoverIntersectionPoolRow)
	for _, row := range rows {
		day := normalizeUniverseDate(row.AsOfDate, row.AsOfDate)
		row.AsOfDate = day
		byDay[day] = append(byDay[day], row)
	}
	days := make([]time.Time, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	out := make([]turnoverIntersectionPoolRow, 0, len(rows))
	for _, day := range days {
		dayRows := byDay[day]
		sort.SliceStable(dayRows, func(i, j int) bool {
			if dayRows[i].CombinedTurnoverUSD == dayRows[j].CombinedTurnoverUSD {
				return dayRows[i].Underlying < dayRows[j].Underlying
			}
			return dayRows[i].CombinedTurnoverUSD > dayRows[j].CombinedTurnoverUSD
		})
		if len(dayRows) > turnoverIntersectionPoolCandidateKeep {
			dayRows = dayRows[:turnoverIntersectionPoolCandidateKeep]
		}
		for index := range dayRows {
			dayRows[index].Rank = uint32(index + 1)
			out = append(out, dayRows[index])
		}
	}
	return out
}

func (s *UniverseService) insertTurnoverIntersectionPoolRows(ctx context.Context, rows []turnoverIntersectionPoolRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := s.repo.PrepareBatch(ctx, `INSERT INTO turnover_intersection_pool_daily (
		market, lookback_days, non_etf_only, as_of_date, underlying, stock_turnover_usd, option_turnover_usd, combined_turnover_usd, rank, updated_at
	)`)
	if err != nil {
		return fmt.Errorf("prepare turnover intersection source batch: %w", err)
	}
	updatedAt := s.now().UTC()
	for _, row := range rows {
		if err := batch.Append(row.Market, uint16(row.LookbackDays), row.NonETFOnly, row.AsOfDate, row.Underlying, row.StockTurnoverUSD, row.OptionTurnoverUSD, row.CombinedTurnoverUSD, row.Rank, updatedAt); err != nil {
			_ = batch.Abort()
			return fmt.Errorf("append turnover intersection source row %s %s: %w", row.AsOfDate.Format("2006-01-02"), row.Underlying, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send turnover intersection source batch: %w", err)
	}
	return nil
}

func clickhouseBoolLiteral(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

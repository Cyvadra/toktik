package usmarket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	fmpStatementSource        = "fmp"
	fmpStatementDerivedSource = "fmp_statement_derived_v2"
	fmpIncomeStatementTable   = "fmp_income_statement_quarterly"
	fmpBalanceStatementTable  = "fmp_balance_sheet_quarterly"
	fmpCashFlowStatementTable = "fmp_cash_flow_statement_quarterly"
)

type fmpStatementPersistResult struct {
	InsertedRows int
	SkippedRows  int
}

type fmpStatementKey struct {
	Symbol       string
	Date         time.Time
	Period       string
	FiscalYear   string
	AcceptedDate time.Time
	Source       string
}

type existingFMPStatement struct {
	ContentHash string
	Revision    uint32
}

type fmpIncomeStatementRow struct {
	Stmt        fmp.IncomeStatement
	Symbol      string
	Date        time.Time
	FilingDate  time.Time
	Accepted    time.Time
	ContentHash string
	Revision    uint32
}

type fmpBalanceSheetRow struct {
	Stmt        fmp.BalanceSheet
	Symbol      string
	Date        time.Time
	FilingDate  time.Time
	Accepted    time.Time
	ContentHash string
	Revision    uint32
}

type fmpCashFlowStatementRow struct {
	Stmt        fmp.CashFlowStatement
	Symbol      string
	Date        time.Time
	FilingDate  time.Time
	Accepted    time.Time
	ContentHash string
	Revision    uint32
}

func persistFMPQuarterlyStatements(ctx context.Context, conn driver.Conn, storeSymbol string, income []fmp.IncomeStatement, balance []fmp.BalanceSheet, cashflow []fmp.CashFlowStatement, batchSize int) (fmpStatementPersistResult, error) {
	var out fmpStatementPersistResult
	inserted, skipped, err := persistFMPIncomeStatements(ctx, conn, storeSymbol, income, batchSize)
	if err != nil {
		return out, err
	}
	out.InsertedRows += inserted
	out.SkippedRows += skipped
	inserted, skipped, err = persistFMPBalanceSheets(ctx, conn, storeSymbol, balance, batchSize)
	if err != nil {
		return out, err
	}
	out.InsertedRows += inserted
	out.SkippedRows += skipped
	inserted, skipped, err = persistFMPCashFlows(ctx, conn, storeSymbol, cashflow, batchSize)
	if err != nil {
		return out, err
	}
	out.InsertedRows += inserted
	out.SkippedRows += skipped
	return out, nil
}

func persistFMPIncomeStatements(ctx context.Context, conn driver.Conn, storeSymbol string, rows []fmp.IncomeStatement, batchSize int) (int, int, error) {
	planned := make([]fmpIncomeStatementRow, 0, len(rows))
	keys := make([]fmpStatementKey, 0, len(rows))
	for _, stmt := range rows {
		row, ok := normalizeFMPIncomeStatement(storeSymbol, stmt)
		if !ok {
			continue
		}
		planned = append(planned, row)
		keys = append(keys, fmpStatementKey{Symbol: row.Symbol, Date: row.Date, Period: row.Stmt.Period, FiscalYear: row.Stmt.FiscalYear, AcceptedDate: row.Accepted, Source: fmpStatementSource})
	}
	existing, err := loadExistingFMPStatements(ctx, conn, fmpIncomeStatementTable, keys)
	if err != nil {
		return 0, 0, err
	}
	writeRows := planned[:0]
	skipped := 0
	for _, row := range planned {
		key := fmpStatementKey{Symbol: row.Symbol, Date: row.Date, Period: row.Stmt.Period, FiscalYear: row.Stmt.FiscalYear, AcceptedDate: row.Accepted, Source: fmpStatementSource}
		if current, ok := existing[key]; ok {
			if current.ContentHash == row.ContentHash {
				skipped++
				continue
			}
			row.Revision = current.Revision + 1
		}
		writeRows = append(writeRows, row)
	}
	if len(writeRows) == 0 {
		return 0, skipped, nil
	}
	if batchSize <= 0 {
		batchSize = len(writeRows)
	}
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO fmp_income_statement_quarterly (
		symbol, date, fiscal_year, period, reported_currency, cik, filing_date, accepted_date,
		revenue, cost_of_revenue, gross_profit, operating_income, income_before_tax,
		income_tax_expense, net_income, bottom_line_net_income, eps, eps_diluted,
		weighted_average_shs_out, weighted_average_shs_out_dil, source, content_hash, revision
	)`)
	if err != nil {
		return 0, skipped, fmt.Errorf("prepare FMP income statements: %w", err)
	}
	written := 0
	pending := 0
	for _, row := range writeRows {
		if err := batch.Append(row.Symbol, row.Date, row.Stmt.FiscalYear, row.Stmt.Period, row.Stmt.ReportedCurrency, row.Stmt.CIK, row.FilingDate, row.Accepted,
			row.Stmt.Revenue, row.Stmt.CostOfRevenue, row.Stmt.GrossProfit, row.Stmt.OperatingIncome, row.Stmt.IncomeBeforeTax,
			row.Stmt.IncomeTaxExpense, row.Stmt.NetIncome, row.Stmt.BottomLineNetIncome, row.Stmt.EPS, row.Stmt.EPSDiluted,
			row.Stmt.WeightedAverageShsOut, row.Stmt.WeightedAverageShsOutDil, fmpStatementSource, row.ContentHash, row.Revision); err != nil {
			return written, skipped, fmt.Errorf("append FMP income statement: %w", err)
		}
		written++
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return written, skipped, fmt.Errorf("send FMP income statements: %w", err)
			}
			pending = 0
			batch, err = conn.PrepareBatch(ctx, `INSERT INTO fmp_income_statement_quarterly (
				symbol, date, fiscal_year, period, reported_currency, cik, filing_date, accepted_date,
				revenue, cost_of_revenue, gross_profit, operating_income, income_before_tax,
				income_tax_expense, net_income, bottom_line_net_income, eps, eps_diluted,
				weighted_average_shs_out, weighted_average_shs_out_dil, source, content_hash, revision
			)`)
			if err != nil {
				return written, skipped, fmt.Errorf("prepare next FMP income statements: %w", err)
			}
		}
	}
	if pending > 0 {
		if err := batch.Send(); err != nil {
			return written, skipped, fmt.Errorf("send final FMP income statements: %w", err)
		}
	}
	return written, skipped, nil
}

func persistFMPBalanceSheets(ctx context.Context, conn driver.Conn, storeSymbol string, rows []fmp.BalanceSheet, batchSize int) (int, int, error) {
	planned := make([]fmpBalanceSheetRow, 0, len(rows))
	keys := make([]fmpStatementKey, 0, len(rows))
	for _, stmt := range rows {
		row, ok := normalizeFMPBalanceSheet(storeSymbol, stmt)
		if !ok {
			continue
		}
		planned = append(planned, row)
		keys = append(keys, fmpStatementKey{Symbol: row.Symbol, Date: row.Date, Period: row.Stmt.Period, FiscalYear: row.Stmt.FiscalYear, AcceptedDate: row.Accepted, Source: fmpStatementSource})
	}
	existing, err := loadExistingFMPStatements(ctx, conn, fmpBalanceStatementTable, keys)
	if err != nil {
		return 0, 0, err
	}
	writeRows := planned[:0]
	skipped := 0
	for _, row := range planned {
		key := fmpStatementKey{Symbol: row.Symbol, Date: row.Date, Period: row.Stmt.Period, FiscalYear: row.Stmt.FiscalYear, AcceptedDate: row.Accepted, Source: fmpStatementSource}
		if current, ok := existing[key]; ok {
			if current.ContentHash == row.ContentHash {
				skipped++
				continue
			}
			row.Revision = current.Revision + 1
		}
		writeRows = append(writeRows, row)
	}
	if len(writeRows) == 0 {
		return 0, skipped, nil
	}
	if batchSize <= 0 {
		batchSize = len(writeRows)
	}
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO fmp_balance_sheet_quarterly (
		symbol, date, fiscal_year, period, reported_currency, cik, filing_date, accepted_date,
		cash_and_cash_equivalents, total_current_assets, total_assets, total_current_liabilities,
		total_liabilities, total_stockholders_equity, total_equity, total_debt, net_debt,
		source, content_hash, revision
	)`)
	if err != nil {
		return 0, skipped, fmt.Errorf("prepare FMP balance sheets: %w", err)
	}
	written := 0
	pending := 0
	for _, row := range writeRows {
		if err := batch.Append(row.Symbol, row.Date, row.Stmt.FiscalYear, row.Stmt.Period, row.Stmt.ReportedCurrency, row.Stmt.CIK, row.FilingDate, row.Accepted,
			row.Stmt.CashAndCashEquivalents, row.Stmt.TotalCurrentAssets, row.Stmt.TotalAssets, row.Stmt.TotalCurrentLiabilities,
			row.Stmt.TotalLiabilities, row.Stmt.TotalStockholdersEquity, row.Stmt.TotalEquity, row.Stmt.TotalDebt, row.Stmt.NetDebt,
			fmpStatementSource, row.ContentHash, row.Revision); err != nil {
			return written, skipped, fmt.Errorf("append FMP balance sheet: %w", err)
		}
		written++
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return written, skipped, fmt.Errorf("send FMP balance sheets: %w", err)
			}
			pending = 0
			batch, err = conn.PrepareBatch(ctx, `INSERT INTO fmp_balance_sheet_quarterly (
				symbol, date, fiscal_year, period, reported_currency, cik, filing_date, accepted_date,
				cash_and_cash_equivalents, total_current_assets, total_assets, total_current_liabilities,
				total_liabilities, total_stockholders_equity, total_equity, total_debt, net_debt,
				source, content_hash, revision
			)`)
			if err != nil {
				return written, skipped, fmt.Errorf("prepare next FMP balance sheets: %w", err)
			}
		}
	}
	if pending > 0 {
		if err := batch.Send(); err != nil {
			return written, skipped, fmt.Errorf("send final FMP balance sheets: %w", err)
		}
	}
	return written, skipped, nil
}

func persistFMPCashFlows(ctx context.Context, conn driver.Conn, storeSymbol string, rows []fmp.CashFlowStatement, batchSize int) (int, int, error) {
	planned := make([]fmpCashFlowStatementRow, 0, len(rows))
	keys := make([]fmpStatementKey, 0, len(rows))
	for _, stmt := range rows {
		row, ok := normalizeFMPCashFlow(storeSymbol, stmt)
		if !ok {
			continue
		}
		planned = append(planned, row)
		keys = append(keys, fmpStatementKey{Symbol: row.Symbol, Date: row.Date, Period: row.Stmt.Period, FiscalYear: row.Stmt.FiscalYear, AcceptedDate: row.Accepted, Source: fmpStatementSource})
	}
	existing, err := loadExistingFMPStatements(ctx, conn, fmpCashFlowStatementTable, keys)
	if err != nil {
		return 0, 0, err
	}
	writeRows := planned[:0]
	skipped := 0
	for _, row := range planned {
		key := fmpStatementKey{Symbol: row.Symbol, Date: row.Date, Period: row.Stmt.Period, FiscalYear: row.Stmt.FiscalYear, AcceptedDate: row.Accepted, Source: fmpStatementSource}
		if current, ok := existing[key]; ok {
			if current.ContentHash == row.ContentHash {
				skipped++
				continue
			}
			row.Revision = current.Revision + 1
		}
		writeRows = append(writeRows, row)
	}
	if len(writeRows) == 0 {
		return 0, skipped, nil
	}
	if batchSize <= 0 {
		batchSize = len(writeRows)
	}
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO fmp_cash_flow_statement_quarterly (
		symbol, date, fiscal_year, period, reported_currency, cik, filing_date, accepted_date,
		net_income, depreciation_and_amortization, stock_based_compensation,
		net_cash_provided_by_operating_activities, capital_expenditure, free_cash_flow,
		source, content_hash, revision
	)`)
	if err != nil {
		return 0, skipped, fmt.Errorf("prepare FMP cash flow statements: %w", err)
	}
	written := 0
	pending := 0
	for _, row := range writeRows {
		if err := batch.Append(row.Symbol, row.Date, row.Stmt.FiscalYear, row.Stmt.Period, row.Stmt.ReportedCurrency, row.Stmt.CIK, row.FilingDate, row.Accepted,
			row.Stmt.NetIncome, row.Stmt.DepreciationAndAmortization, row.Stmt.StockBasedCompensation,
			row.Stmt.NetCashProvidedByOperatingActivities, row.Stmt.CapitalExpenditure, row.Stmt.FreeCashFlow,
			fmpStatementSource, row.ContentHash, row.Revision); err != nil {
			return written, skipped, fmt.Errorf("append FMP cash flow statement: %w", err)
		}
		written++
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return written, skipped, fmt.Errorf("send FMP cash flow statements: %w", err)
			}
			pending = 0
			batch, err = conn.PrepareBatch(ctx, `INSERT INTO fmp_cash_flow_statement_quarterly (
				symbol, date, fiscal_year, period, reported_currency, cik, filing_date, accepted_date,
				net_income, depreciation_and_amortization, stock_based_compensation,
				net_cash_provided_by_operating_activities, capital_expenditure, free_cash_flow,
				source, content_hash, revision
			)`)
			if err != nil {
				return written, skipped, fmt.Errorf("prepare next FMP cash flow statements: %w", err)
			}
		}
	}
	if pending > 0 {
		if err := batch.Send(); err != nil {
			return written, skipped, fmt.Errorf("send final FMP cash flow statements: %w", err)
		}
	}
	return written, skipped, nil
}

func loadExistingFMPStatements(ctx context.Context, conn driver.Conn, table string, keys []fmpStatementKey) (map[fmpStatementKey]existingFMPStatement, error) {
	out := map[fmpStatementKey]existingFMPStatement{}
	if len(keys) == 0 {
		return out, nil
	}
	symbols := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	minDate := keys[0].Date
	maxDate := keys[0].Date
	for _, key := range keys {
		if _, ok := seen[key.Symbol]; !ok {
			seen[key.Symbol] = struct{}{}
			symbols = append(symbols, key.Symbol)
		}
		if key.Date.Before(minDate) {
			minDate = key.Date
		}
		if key.Date.After(maxDate) {
			maxDate = key.Date
		}
	}
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT symbol, date, period, fiscal_year, accepted_date, source, content_hash, revision
FROM %s
WHERE symbol IN ({symbols:Array(String)})
  AND date >= {from:Date}
  AND date <= {to:Date}
  AND source = {source:String}
ORDER BY symbol, date, period, fiscal_year, accepted_date, source, revision`, table),
		clickhouse.Named("symbols", symbols),
		clickhouse.Named("from", minDate.Format("2006-01-02")),
		clickhouse.Named("to", maxDate.Format("2006-01-02")),
		clickhouse.Named("source", fmpStatementSource),
	)
	if err != nil {
		return nil, fmt.Errorf("query existing FMP statements %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var key fmpStatementKey
		var hash string
		var revision uint32
		if err := rows.Scan(&key.Symbol, &key.Date, &key.Period, &key.FiscalYear, &key.AcceptedDate, &key.Source, &hash, &revision); err != nil {
			return nil, fmt.Errorf("scan existing FMP statements %s: %w", table, err)
		}
		key.Date = normalizeDateOnly(key.Date)
		key.AcceptedDate = key.AcceptedDate.UTC()
		if current, ok := out[key]; !ok || revision >= current.Revision {
			out[key] = existingFMPStatement{ContentHash: hash, Revision: revision}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing FMP statements %s: %w", table, err)
	}
	return out, nil
}

func normalizeFMPIncomeStatement(storeSymbol string, stmt fmp.IncomeStatement) (fmpIncomeStatementRow, bool) {
	date, ok := parseFMPDate(stmt.Date)
	if !ok {
		return fmpIncomeStatementRow{}, false
	}
	row := fmpIncomeStatementRow{Stmt: stmt, Symbol: normalizeStatementSymbol(storeSymbol, stmt.Symbol), Date: date}
	row.FilingDate = parseFMPKnownAt(stmt.FilingDate)
	row.Accepted = parseFMPKnownAt(stmt.AcceptedDate, stmt.FilingDate, stmt.Date)
	row.ContentHash = fmpStatementHash(stmt)
	return row, row.Symbol != ""
}

func normalizeFMPBalanceSheet(storeSymbol string, stmt fmp.BalanceSheet) (fmpBalanceSheetRow, bool) {
	date, ok := parseFMPDate(stmt.Date)
	if !ok {
		return fmpBalanceSheetRow{}, false
	}
	row := fmpBalanceSheetRow{Stmt: stmt, Symbol: normalizeStatementSymbol(storeSymbol, stmt.Symbol), Date: date}
	row.FilingDate = parseFMPKnownAt(stmt.FilingDate)
	row.Accepted = parseFMPKnownAt(stmt.AcceptedDate, stmt.FilingDate, stmt.Date)
	row.ContentHash = fmpStatementHash(stmt)
	return row, row.Symbol != ""
}

func normalizeFMPCashFlow(storeSymbol string, stmt fmp.CashFlowStatement) (fmpCashFlowStatementRow, bool) {
	date, ok := parseFMPDate(stmt.Date)
	if !ok {
		return fmpCashFlowStatementRow{}, false
	}
	row := fmpCashFlowStatementRow{Stmt: stmt, Symbol: normalizeStatementSymbol(storeSymbol, stmt.Symbol), Date: date}
	row.FilingDate = parseFMPKnownAt(stmt.FilingDate)
	row.Accepted = parseFMPKnownAt(stmt.AcceptedDate, stmt.FilingDate, stmt.Date)
	row.ContentHash = fmpStatementHash(stmt)
	return row, row.Symbol != ""
}

func normalizeStatementSymbol(storeSymbol, statementSymbol string) string {
	if normalized := strings.ToUpper(strings.TrimSpace(storeSymbol)); normalized != "" {
		return normalized
	}
	return strings.ToUpper(strings.TrimSpace(statementSymbol))
}

func fmpStatementHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type fmpDerivedQuarterInput struct {
	Date               time.Time
	KnownAt            time.Time
	EPS                float64
	NetIncome          float64
	WeightedShares     float64
	StockholdersEquity float64
}

func buildFMPDerivedQuarterInputs(incomeStatements []fmp.IncomeStatement, balanceSheets []fmp.BalanceSheet) []fmpDerivedQuarterInput {
	balanceByDate := make(map[string]fmp.BalanceSheet, len(balanceSheets))
	for _, balanceSheet := range balanceSheets {
		date := strings.TrimSpace(balanceSheet.Date)
		if date != "" {
			balanceByDate[date] = balanceSheet
		}
	}
	inputs := make([]fmpDerivedQuarterInput, 0, len(incomeStatements))
	for _, incomeStatement := range incomeStatements {
		date, ok := parseFMPDate(incomeStatement.Date)
		if !ok {
			continue
		}
		balanceSheet, ok := balanceByDate[incomeStatement.Date]
		if !ok {
			continue
		}
		inputs = append(inputs, fmpDerivedQuarterInput{
			Date:               date,
			KnownAt:            parseFMPKnownAt(incomeStatement.AcceptedDate, incomeStatement.FilingDate, incomeStatement.Date),
			EPS:                incomeStatement.EPS,
			NetIncome:          incomeStatement.NetIncome,
			WeightedShares:     incomeStatement.WeightedAverageShsOut,
			StockholdersEquity: incomeStatementFallbackEquity(balanceSheet),
		})
	}
	sort.Slice(inputs, func(left, right int) bool { return inputs[left].Date.Before(inputs[right].Date) })
	return inputs
}

func (input fmpDerivedQuarterInput) BasicEPS() (float64, bool) {
	if validFiniteNonZero(input.EPS) {
		return input.EPS, true
	}
	if input.WeightedShares > 0 && validFiniteNonZero(input.NetIncome) {
		value := input.NetIncome / input.WeightedShares
		if validFiniteNonZero(value) {
			return value, true
		}
	}
	if input.EPS == 0 && !math.IsNaN(input.EPS) && !math.IsInf(input.EPS, 0) {
		return 0, true
	}
	return 0, false
}

func (input fmpDerivedQuarterInput) BookValuePerBasicShare() (float64, bool) {
	if input.WeightedShares <= 0 || input.StockholdersEquity == 0 {
		return 0, false
	}
	value := input.StockholdersEquity / input.WeightedShares
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func trailingTwelveMonthBasicEPS(inputs []fmpDerivedQuarterInput, quarterIndex int) (float64, bool) {
	if quarterIndex < 3 || quarterIndex >= len(inputs) {
		return 0, false
	}
	total := 0.0
	for offset := 0; offset < 4; offset++ {
		eps, ok := inputs[quarterIndex-offset].BasicEPS()
		if !ok {
			return 0, false
		}
		total += eps
	}
	if total == 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, false
	}
	return total, true
}

func validFiniteNonZero(value float64) bool {
	return value != 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func deriveFMPStatementRatioObservations(ctx context.Context, conn driver.Conn, symbol string, inputs []fmpDerivedQuarterInput, startDate, endDate time.Time) ([]fundamentalObservationInsert, PEFetchDiagnostics, error) {
	diagnostics := PEFetchDiagnostics{}
	if len(inputs) == 0 {
		diagnostics.NoQuarterInputs = 1
		return nil, diagnostics, nil
	}
	priceFrom := inputs[0].Date.AddDate(0, 0, -14)
	priceTo := inputs[len(inputs)-1].Date.AddDate(0, 0, 1)
	priceSeries, err := loadUSStockAdjustedDailyCloses(ctx, conn, symbol, priceFrom, priceTo)
	if err != nil {
		return nil, diagnostics, err
	}
	startBound := normalizeDateOnly(startDate)
	endBound := normalizeDateOnly(endDate)
	observations := make([]fundamentalObservationInsert, 0, len(inputs)*2)
	for quarterIndex, input := range inputs {
		if input.Date.Before(startBound) || input.Date.After(endBound) {
			continue
		}
		closePrice, ok := priceSeries.closeOnOrBefore(input.Date)
		if !ok || closePrice <= 0 {
			diagnostics.MissingPrice++
			continue
		}
		knownAt := input.KnownAt
		if knownAt.IsZero() {
			knownAt = input.Date
		}
		if ttmEPS, ok := trailingTwelveMonthBasicEPS(inputs, quarterIndex); ok {
			observations = appendRatioObservationWithSource(observations, symbol, usStocksPEFactorCode, input.Date, knownAt, fmpStatementDerivedSource, closePrice/ttmEPS)
		} else {
			diagnostics.MissingTTMEPS++
		}
		if bookValuePerShare, ok := input.BookValuePerBasicShare(); ok {
			observations = appendRatioObservationWithSource(observations, symbol, usStocksPBFactorCode, input.Date, knownAt, fmpStatementDerivedSource, closePrice/bookValuePerShare)
		} else {
			diagnostics.MissingBookValue++
		}
	}
	return observations, diagnostics, nil
}

type adjustedDailyClose struct {
	Timestamp time.Time
	Close     float64
}

type adjustedDailyCloseSeries []adjustedDailyClose

func loadUSStockAdjustedDailyCloses(ctx context.Context, conn driver.Conn, symbol string, from, to time.Time) (adjustedDailyCloseSeries, error) {
	query := fmt.Sprintf(`SELECT
		b.timestamp,
		%s AS close
FROM %s AS b
%s
WHERE b.symbol = %s
	AND b.timestamp >= toDateTime(%s, 'UTC')
	AND b.timestamp < toDateTime(%s, 'UTC')
GROUP BY b.timestamp, b.symbol, b.close
ORDER BY b.timestamp`, chquery.USStockAdjustedPriceSQL("b", "close", "sp"), chquery.USStockIntervals["1d"], chquery.USStockSplitJoinSQL("b", "sp"), usMarketClickHouseStringLiteral(symbol), usMarketClickHouseDateTimeLiteral(from), usMarketClickHouseDateTimeLiteral(to))
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query adjusted US stock closes: %w", err)
	}
	defer rows.Close()
	series := make(adjustedDailyCloseSeries, 0, 64)
	for rows.Next() {
		var point adjustedDailyClose
		if err := rows.Scan(&point.Timestamp, &point.Close); err != nil {
			return nil, fmt.Errorf("scan adjusted US stock close: %w", err)
		}
		series = append(series, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate adjusted US stock closes: %w", err)
	}
	return series, nil
}

func usMarketClickHouseStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func usMarketClickHouseDateTimeLiteral(value time.Time) string {
	return usMarketClickHouseStringLiteral(value.UTC().Format("2006-01-02 15:04:05"))
}

func (s adjustedDailyCloseSeries) closeOnOrBefore(ts time.Time) (float64, bool) {
	if len(s) == 0 {
		return 0, false
	}
	idx := sort.Search(len(s), func(i int) bool { return s[i].Timestamp.After(ts.UTC()) }) - 1
	if idx < 0 {
		return 0, false
	}
	return s[idx].Close, true
}

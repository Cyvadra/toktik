package usmarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const missingOptionGreeksPredicate = `(isNaN(underlying_close) OR isNaN(implied_volatility) OR isNaN(delta) OR isNaN(gamma) OR isNaN(vega) OR isNaN(theta) OR isNaN(rho))`

const availableOptionGreeksPredicate = `(NOT isNaN(underlying_close) AND NOT isNaN(implied_volatility) AND NOT isNaN(delta) AND NOT isNaN(gamma) AND NOT isNaN(vega) AND NOT isNaN(theta) AND NOT isNaN(rho))`

const defaultThetaDataBaseURL = "http://127.0.0.1:25503/v3"

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

type DailyGreekValues struct {
	UnderlyingClose   float32
	ImpliedVolatility float32
	Delta             float32
	Gamma             float32
	Vega              float32
	Theta             float32
	Rho               float32
}

type OptionGreeksBackfillStats struct {
	RowsScanned        int
	RowsMatched        int
	RowsBackfilled     int
	ContractsMatched   int
	ContractsUnmatched int
	RowsFallback       int
	ContractsFallback  int
	NoData             bool
}

type thetaDataNoDataError struct {
	Symbols []string
}

func (e *thetaDataNoDataError) Error() string {
	if len(e.Symbols) == 0 {
		return "ThetaData EOD greeks: no data found"
	}
	return fmt.Sprintf("ThetaData EOD greeks: no data found for symbols %s", strings.Join(e.Symbols, ","))
}

type optionContractKey struct {
	expiration  string
	strikeMilli int64
	optionType  string
}

type thetaDataEODGreeksResponse struct {
	Response []thetaDataEODGreeksItem `json:"response"`
}

type thetaDataEODGreeksItem struct {
	Contract thetaDataContract         `json:"contract"`
	Data     []thetaDataEODGreeksPoint `json:"data"`
}

type thetaDataContract struct {
	Symbol     string  `json:"symbol"`
	Expiration string  `json:"expiration"`
	Right      string  `json:"right"`
	Strike     float64 `json:"strike"`
}

type thetaDataEODGreeksPoint struct {
	UnderlyingPrice float64 `json:"underlying_price"`
	ImpliedVol      float64 `json:"implied_vol"`
	Delta           float64 `json:"delta"`
	Gamma           float64 `json:"gamma"`
	Vega            float64 `json:"vega"`
	Theta           float64 `json:"theta"`
	Rho             float64 `json:"rho"`
}

func normalizeDateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func makeOptionContractKey(expiration time.Time, strike float64, optionType string) optionContractKey {
	expiration = normalizeDateOnly(expiration)
	return optionContractKey{
		expiration:  expiration.Format("2006-01-02"),
		strikeMilli: int64(strike*1000 + 0.5),
		optionType:  strings.ToUpper(optionType),
	}
}

func contractKeyFromContract(contract OptionContract) optionContractKey {
	return makeOptionContractKey(contract.Expiration, contract.Strike, contract.OptionType)
}

func thetaDataCandidateSymbols(underlying string) []string {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	candidates := []string{underlying}

	switch underlying {
	case "SPX":
		candidates = append(candidates, "SPXW", "SPXQ", "SPXPM")
	case "SPXW":
		candidates = append(candidates, "SPX")
	case "RUT":
		candidates = append(candidates, "RUTW", "RUTQ")
	case "RUTW":
		candidates = append(candidates, "RUT")
	case "VIX":
		candidates = append(candidates, "VIXW")
	case "VIXW":
		candidates = append(candidates, "VIX")
	case "NDX":
		candidates = append(candidates, "NDXP")
	case "NDXP":
		candidates = append(candidates, "NDX")
	case "XSP":
		candidates = append(candidates, "XSPPM", "XSPAM")
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
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

func FetchThetaDataEODGreeks(ctx context.Context, client *http.Client, baseURL, underlying string, marketDate time.Time) (map[optionContractKey]DailyGreekValues, error) {
	candidates := thetaDataCandidateSymbols(underlying)
	merged := make(map[optionContractKey]DailyGreekValues)
	noDataSymbols := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		greeksByContract, err := fetchThetaDataEODGreeksForSymbol(ctx, client, baseURL, candidate, marketDate)
		if err != nil {
			var noDataErr *thetaDataNoDataError
			if errors.As(err, &noDataErr) {
				noDataSymbols = append(noDataSymbols, candidate)
				continue
			}
			return nil, err
		}
		for key, value := range greeksByContract {
			merged[key] = value
		}
	}

	if len(merged) == 0 {
		return nil, &thetaDataNoDataError{Symbols: noDataSymbols}
	}

	return merged, nil
}

func fetchThetaDataEODGreeksForSymbol(ctx context.Context, client *http.Client, baseURL, symbol string, marketDate time.Time) (map[optionContractKey]DailyGreekValues, error) {
	requestURL, err := buildThetaDataEODGreeksURL(baseURL, symbol, marketDate)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build ThetaData request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request ThetaData EOD greeks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		trimmedBody := strings.TrimSpace(string(body))
		if resp.StatusCode == 472 && strings.Contains(strings.ToLower(trimmedBody), "no data found") {
			return nil, &thetaDataNoDataError{Symbols: []string{symbol}}
		}
		return nil, fmt.Errorf("ThetaData EOD greeks request failed for %s: status %d body %q", symbol, resp.StatusCode, trimmedBody)
	}

	var payload thetaDataEODGreeksResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode ThetaData EOD greeks response: %w", err)
	}

	greeksByContract := make(map[optionContractKey]DailyGreekValues, len(payload.Response))
	for _, item := range payload.Response {
		if len(item.Data) == 0 {
			continue
		}

		expiration, err := parseThetaDataDate(item.Contract.Expiration)
		if err != nil {
			return nil, fmt.Errorf("parse ThetaData expiration %q: %w", item.Contract.Expiration, err)
		}

		optionType, ok := normalizeThetaDataRight(item.Contract.Right)
		if !ok {
			continue
		}

		point := item.Data[len(item.Data)-1]
		greeksByContract[makeOptionContractKey(expiration, item.Contract.Strike, optionType)] = DailyGreekValues{
			UnderlyingClose:   float32(point.UnderlyingPrice),
			ImpliedVolatility: float32(point.ImpliedVol),
			Delta:             float32(point.Delta),
			Gamma:             float32(point.Gamma),
			Vega:              float32(point.Vega),
			Theta:             float32(point.Theta),
			Rho:               float32(point.Rho),
		}
	}

	if len(greeksByContract) == 0 {
		return nil, &thetaDataNoDataError{Symbols: []string{symbol}}
	}

	return greeksByContract, nil
}

func buildThetaDataEODGreeksURL(baseURL, underlying string, marketDate time.Time) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultThetaDataBaseURL
	}

	u, err := url.Parse(baseURL + "/option/history/greeks/eod")
	if err != nil {
		return "", fmt.Errorf("parse ThetaData base URL: %w", err)
	}

	date := marketDate.Format("2006-01-02")
	query := u.Query()
	query.Set("symbol", underlying)
	query.Set("expiration", "*")
	query.Set("start_date", date)
	query.Set("end_date", date)
	query.Set("format", "json")
	u.RawQuery = query.Encode()

	return u.String(), nil
}

func parseThetaDataDate(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "20060102"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format")
}

func normalizeThetaDataRight(right string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(right)) {
	case "CALL", "C":
		return "C", true
	case "PUT", "P":
		return "P", true
	default:
		return "", false
	}
}

func ApplyThetaDailyGreeks(rows []OptionBar1m, greeksByContract map[optionContractKey]DailyGreekValues) ([]OptionBar1m, []OptionBar1m, []OptionContract, []OptionContract) {
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
		key := contractKeyFromContract(contract)
		values, ok := greeksByContract[key]
		if !ok {
			unmatchedContracts[key] = contract
			continue
		}

		row.UnderlyingClose = values.UnderlyingClose
		row.ImpliedVolatility = values.ImpliedVolatility
		row.Delta = values.Delta
		row.Gamma = values.Gamma
		row.Vega = values.Vega
		row.Theta = values.Theta
		row.Rho = values.Rho

		updatedRows = append(updatedRows, row)
		originalRows = append(originalRows, rows[len(originalRows)])
		matchedContracts[key] = contract
	}

	return updatedRows, originalRows, sortContracts(matchedContracts), sortContracts(unmatchedContracts)
}

func MergeGreekValues(primary, fallback map[optionContractKey]DailyGreekValues) map[optionContractKey]DailyGreekValues {
	if len(primary) == 0 && len(fallback) == 0 {
		return nil
	}

	merged := make(map[optionContractKey]DailyGreekValues, len(primary)+len(fallback))
	for key, value := range fallback {
		merged[key] = value
	}
	for key, value := range primary {
		merged[key] = value
	}
	return merged
}

func FallbackContracts(rows []OptionBar1m, values map[optionContractKey]DailyGreekValues) []OptionContract {
	missing := make(map[optionContractKey]OptionContract)
	for _, row := range rows {
		contract := OptionContract{
			Underlying: row.Underlying,
			Expiration: row.Expiration,
			Strike:     row.Strike,
			OptionType: row.OptionType,
		}
		key := contractKeyFromContract(contract)
		if _, ok := values[key]; ok {
			continue
		}
		missing[key] = contract
	}
	return sortContracts(missing)
}

func LoadPreviousKnownGreeks(ctx context.Context, conn driver.Conn, marketDate time.Time, underlying string, contracts []OptionContract) (map[optionContractKey]DailyGreekValues, error) {
	values := make(map[optionContractKey]DailyGreekValues)
	if len(contracts) == 0 {
		return values, nil
	}

	const contractsPerQuery = 200
	for start := 0; start < len(contracts); start += contractsPerQuery {
		end := start + contractsPerQuery
		if end > len(contracts) {
			end = len(contracts)
		}

		chunkValues, err := loadPreviousKnownGreeksChunk(ctx, conn, marketDate, underlying, contracts[start:end])
		if err != nil {
			return nil, err
		}
		for key, value := range chunkValues {
			values[key] = value
		}
	}

	return values, nil
}

func loadPreviousKnownGreeksChunk(ctx context.Context, conn driver.Conn, marketDate time.Time, underlying string, contracts []OptionContract) (map[optionContractKey]DailyGreekValues, error) {
	clauses := make([]string, 0, len(contracts))
	args := make([]any, 0, 2+len(contracts)*3)
	args = append(args,
		clickhouse.Named("market_date", marketDate.Format("2006-01-02")),
		clickhouse.Named("underlying", underlying),
	)

	for idx, contract := range contracts {
		expirationName := fmt.Sprintf("expiration_%d", idx)
		strikeName := fmt.Sprintf("strike_%d", idx)
		optionTypeName := fmt.Sprintf("option_type_%d", idx)
		clauses = append(clauses,
			fmt.Sprintf("(expiration = {%s:Date} AND strike = {%s:Float64} AND option_type = {%s:String})", expirationName, strikeName, optionTypeName),
		)
		args = append(args,
			clickhouse.Named(expirationName, contract.Expiration.Format("2006-01-02")),
			clickhouse.Named(strikeName, contract.Strike),
			clickhouse.Named(optionTypeName, contract.OptionType),
		)
	}

	query := `SELECT expiration, strike, option_type,
			underlying_close, implied_volatility, delta, gamma, vega, theta, rho
		FROM us_options_bar_1m
		WHERE market_date < {market_date:Date}
		  AND underlying = {underlying:String}
		  AND ` + availableOptionGreeksPredicate + `
		  AND (` + strings.Join(clauses, ` OR `) + `)
		ORDER BY expiration, strike, option_type, market_date DESC, timestamp DESC
		LIMIT 1 BY expiration, strike, option_type`

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query previous known option greeks: %w", err)
	}
	defer rows.Close()

	values := make(map[optionContractKey]DailyGreekValues, len(contracts))
	for rows.Next() {
		var (
			expiration  time.Time
			strike      float64
			optionType  string
			greekValues DailyGreekValues
		)
		if err := rows.Scan(
			&expiration,
			&strike,
			&optionType,
			&greekValues.UnderlyingClose,
			&greekValues.ImpliedVolatility,
			&greekValues.Delta,
			&greekValues.Gamma,
			&greekValues.Vega,
			&greekValues.Theta,
			&greekValues.Rho,
		); err != nil {
			return nil, fmt.Errorf("scan previous known option greeks: %w", err)
		}
		values[makeOptionContractKey(expiration, strike, optionType)] = greekValues
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate previous known option greeks: %w", err)
	}

	return values, nil
}

func BackfillOptionGreeksWithThetaData(ctx context.Context, conn driver.Conn, client *http.Client, baseURL string, task MissingOptionGreeksTask, batchSize int, dryRun bool) (OptionGreeksBackfillStats, error) {
	rows, err := LoadOptionBarsNeedingBackfill(ctx, conn, task)
	if err != nil {
		return OptionGreeksBackfillStats{}, err
	}

	stats := OptionGreeksBackfillStats{RowsScanned: len(rows)}
	if len(rows) == 0 {
		return stats, nil
	}

	thetaGreeks := make(map[optionContractKey]DailyGreekValues)
	greeksByContract, err := FetchThetaDataEODGreeks(ctx, client, baseURL, task.Underlying, task.MarketDate)
	if err != nil {
		var noDataErr *thetaDataNoDataError
		if errors.As(err, &noDataErr) {
			greeksByContract = nil
		} else {
			return stats, err
		}
	} else {
		thetaGreeks = greeksByContract
	}

	fallbackContracts := FallbackContracts(rows, thetaGreeks)
	fallbackGreeks, err := LoadPreviousKnownGreeks(ctx, conn, task.MarketDate, task.Underlying, fallbackContracts)
	if err != nil {
		return stats, err
	}

	greeksByContract = MergeGreekValues(thetaGreeks, fallbackGreeks)
	updatedRows, originalRows, matchedContracts, unmatchedContracts := ApplyThetaDailyGreeks(rows, greeksByContract)
	stats.RowsMatched = len(updatedRows)
	stats.ContractsMatched = len(matchedContracts)
	stats.ContractsUnmatched = len(unmatchedContracts)
	stats.ContractsFallback = len(fallbackGreeks)

	if len(fallbackGreeks) > 0 {
		for _, row := range updatedRows {
			contract := OptionContract{
				Underlying: row.Underlying,
				Expiration: row.Expiration,
				Strike:     row.Strike,
				OptionType: row.OptionType,
			}
			key := contractKeyFromContract(contract)
			if _, ok := thetaGreeks[key]; ok {
				continue
			}
			if _, ok := fallbackGreeks[key]; ok {
				stats.RowsFallback++
			}
		}
	}
	if len(updatedRows) == 0 {
		stats.NoData = true
		return stats, nil
	}

	if dryRun || len(updatedRows) == 0 {
		return stats, nil
	}

	if err := DeleteOptionBarsForContracts(ctx, conn, task.MarketDate, task.Underlying, matchedContracts); err != nil {
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

func InsertOptionBarSlice(ctx context.Context, conn driver.Conn, rows []OptionBar1m, batchSize int) (int64, error) {
	ch := make(chan OptionBar1m, len(rows))
	for _, row := range rows {
		ch <- row
	}
	close(ch)
	return InsertOptionBars(ctx, conn, ch, batchSize)
}

func DeleteOptionBarsForContracts(ctx context.Context, conn driver.Conn, marketDate time.Time, underlying string, contracts []OptionContract) error {
	if len(contracts) == 0 {
		return nil
	}

	const contractsPerMutation = 200
	for start := 0; start < len(contracts); start += contractsPerMutation {
		end := start + contractsPerMutation
		if end > len(contracts) {
			end = len(contracts)
		}
		if err := deleteOptionBarContractChunk(ctx, conn, marketDate, underlying, contracts[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func deleteOptionBarContractChunk(ctx context.Context, conn driver.Conn, marketDate time.Time, underlying string, contracts []OptionContract) error {
	clauses := make([]string, 0, len(contracts))
	args := make([]any, 0, 2+len(contracts)*3)
	args = append(args, marketDate.Format("2006-01-02"), underlying)

	for _, contract := range contracts {
		clauses = append(clauses, `(expiration = ? AND strike = ? AND option_type = ?)`)
		args = append(args, contract.Expiration, contract.Strike, contract.OptionType)
	}

	query := `ALTER TABLE us_options_bar_1m DELETE
		WHERE market_date = ?
		  AND underlying = ?
		  AND ` + missingOptionGreeksPredicate + `
		  AND (` + strings.Join(clauses, ` OR `) + `)
		SETTINGS mutations_sync = 1`

	if err := conn.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("delete option bars for backfill contracts: %w", err)
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

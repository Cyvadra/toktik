package macro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	DefaultGurufocusShillerDataset = "gurufocus-shiller"
	DefaultGurufocusShillerURL     = "https://www.gurufocus.cn/_api/indicator/shiller_pe/data?locale=zh-hans"
	DefaultReferenceSymbol         = "SPX"
	DefaultReferenceMarket         = "us-stocks"
	gurufocusSource                = "gurufocus"
	realtimeForwardFill            = "forward_fill"
	realtimePriceScaled            = "price_scaled"
)

type GurufocusShillerConfig struct {
	URL             string
	ReferenceSymbol string
	BatchSize       int
}

type GurufocusShillerResult struct {
	CatalogRows     int
	ObservationRows int
}

type rawMonthlyRecord map[string]json.RawMessage

type factorDefinition struct {
	Code         string
	DisplayName  string
	Description  string
	ValueType    string
	Unit         string
	RealtimeMode string
}

type CatalogRow struct {
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

type ObservationRow struct {
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

func SyncGurufocusShiller(ctx context.Context, conn driver.Conn, cfg GurufocusShillerConfig, from, to time.Time, dryRun bool) (GurufocusShillerResult, error) {
	if cfg.URL == "" {
		cfg.URL = DefaultGurufocusShillerURL
	}
	if cfg.ReferenceSymbol == "" {
		cfg.ReferenceSymbol = DefaultReferenceSymbol
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	records, err := fetchMonthlyRecords(ctx, cfg.URL)
	if err != nil {
		return GurufocusShillerResult{}, err
	}
	records = filterRecordsByWindow(records, from, to)
	anchors, err := loadMonthAnchors(ctx, conn, strings.ToUpper(strings.TrimSpace(cfg.ReferenceSymbol)), records)
	if err != nil {
		return GurufocusShillerResult{}, err
	}
	catalogRows, observationRows, err := buildRows(records, anchors, strings.ToUpper(strings.TrimSpace(cfg.ReferenceSymbol)))
	if err != nil {
		return GurufocusShillerResult{}, err
	}
	if dryRun {
		return GurufocusShillerResult{CatalogRows: len(catalogRows), ObservationRows: len(observationRows)}, nil
	}
	if err := UpsertCatalog(ctx, conn, catalogRows, cfg.BatchSize); err != nil {
		return GurufocusShillerResult{}, err
	}
	if err := InsertObservations(ctx, conn, observationRows, cfg.BatchSize); err != nil {
		return GurufocusShillerResult{}, err
	}
	return GurufocusShillerResult{CatalogRows: len(catalogRows), ObservationRows: len(observationRows)}, nil
}

func fetchMonthlyRecords(ctx context.Context, url string) ([]rawMonthlyRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("upstream status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var records []rawMonthlyRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, err
	}
	filtered := make([]rawMonthlyRecord, 0, len(records))
	for _, record := range records {
		if _, err := decodeMonthString(mustMonth(record)); err == nil {
			filtered = append(filtered, record)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no valid YYYY-MM records found in upstream payload")
	}
	return filtered, nil
}

func filterRecordsByWindow(records []rawMonthlyRecord, from, to time.Time) []rawMonthlyRecord {
	if from.IsZero() && to.IsZero() {
		return records
	}
	out := make([]rawMonthlyRecord, 0, len(records))
	for _, record := range records {
		month, err := decodeMonthString(mustMonth(record))
		if err != nil {
			continue
		}
		monthEnd := month.AddDate(0, 1, -1)
		if !from.IsZero() && monthEnd.Before(dateOnly(from)) {
			continue
		}
		if !to.IsZero() && month.After(dateOnly(to)) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func loadMonthAnchors(ctx context.Context, conn driver.Conn, referenceSymbol string, records []rawMonthlyRecord) (map[string]monthAnchor, error) {
	if len(records) == 0 {
		return map[string]monthAnchor{}, nil
	}
	firstMonth, err := decodeMonthString(mustMonth(records[0]))
	if err != nil {
		return nil, err
	}
	lastMonth, err := decodeMonthString(mustMonth(records[len(records)-1]))
	if err != nil {
		return nil, err
	}
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
		clickhouse.Named("from", firstMonth.UTC().Format("2006-01-02 15:04:05")),
		clickhouse.Named("to", lastMonth.AddDate(0, 2, 0).UTC().Format("2006-01-02 15:04:05")),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	monthly := map[string]monthAnchor{}
	for rows.Next() {
		var monthStart, lastTS, firstTS time.Time
		var lastClose float64
		if err := rows.Scan(&monthStart, &lastTS, &lastClose, &firstTS); err != nil {
			return nil, err
		}
		monthly[monthStart.UTC().Format("2006-01")] = monthAnchor{StartMonth: monthStart.UTC(), LastTS: lastTS.UTC(), LastClose: lastClose, FirstTS: firstTS.UTC()}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(monthly) == 0 {
		return nil, fmt.Errorf("no us_stocks_bar_1m anchors found for %s", referenceSymbol)
	}
	for monthKey, anchor := range monthly {
		nextMonth := anchor.StartMonth.AddDate(0, 1, 0).Format("2006-01")
		if next, ok := monthly[nextMonth]; ok {
			anchor.FirstTS = next.FirstTS
		} else if anchor.FirstTS.IsZero() {
			anchor.FirstTS = anchor.LastTS
		}
		monthly[monthKey] = anchor
	}
	return monthly, nil
}

func buildRows(records []rawMonthlyRecord, anchors map[string]monthAnchor, referenceSymbol string) ([]CatalogRow, []ObservationRow, error) {
	definitions := make(map[string]factorDefinition)
	observations := make([]ObservationRow, 0, len(records)*8)
	for _, record := range records {
		month := mustMonth(record)
		anchor, ok := anchors[month]
		if !ok {
			continue
		}
		periodStart, err := decodeMonthString(month)
		if err != nil {
			return nil, nil, err
		}
		for key, raw := range record {
			if key == "date" {
				continue
			}
			value, ok := decodeFloat(raw)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			definition := factorDefinitionForKey(key)
			definitions[key] = definition
			anchorValue := math.NaN()
			if definition.RealtimeMode == realtimePriceScaled {
				anchorValue = anchor.LastClose
			}
			observations = append(observations, ObservationRow{Dataset: DefaultGurufocusShillerDataset, FactorCode: key, EventTS: anchor.LastTS, KnownAt: anchor.FirstTS, PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0), Source: gurufocusSource, Value: value, ReferenceMarket: DefaultReferenceMarket, ReferenceSymbol: referenceSymbol, AnchorValue: anchorValue})
		}
	}
	catalogRows := make([]CatalogRow, 0, len(definitions))
	for _, key := range sortedKeys(definitions) {
		definition := definitions[key]
		catalogRows = append(catalogRows, CatalogRow{Dataset: DefaultGurufocusShillerDataset, FactorCode: definition.Code, DisplayName: definition.DisplayName, Description: definition.Description, ValueType: definition.ValueType, Unit: definition.Unit, PreferredFrequency: "monthly", FillPolicy: "forward_fill", PointInTime: 1, Source: gurufocusSource, ReferenceMarket: DefaultReferenceMarket, ReferenceSymbol: referenceSymbol, RealtimeMode: definition.RealtimeMode, Active: 1, SLAHours: 24 * 45, Metadata: fmt.Sprintf(`{"dataset":"%s","raw_field":"%s"}`, DefaultGurufocusShillerDataset, definition.Code)})
	}
	return catalogRows, observations, nil
}

func UpsertCatalog(ctx context.Context, conn driver.Conn, rows []CatalogRow, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	codes := make([]string, 0, len(rows))
	for _, row := range rows {
		codes = append(codes, row.FactorCode)
	}
	if err := conn.Exec(ctx, `ALTER TABLE macro_factor_catalog DELETE WHERE dataset = {dataset:String} AND factor_code IN {codes:Array(String)} SETTINGS mutations_sync = 1`, clickhouse.Named("dataset", rows[0].Dataset), clickhouse.Named("codes", codes)); err != nil {
		return err
	}
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO macro_factor_catalog (
		dataset, factor_code, display_name, description, value_type, unit,
		preferred_frequency, fill_policy, fill_max_days, point_in_time, source,
		reference_market, reference_symbol, realtime_mode, active, sla_hours, metadata
	)`)
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		if err := batch.Append(row.Dataset, row.FactorCode, row.DisplayName, row.Description, row.ValueType, row.Unit, row.PreferredFrequency, row.FillPolicy, row.FillMaxDays, row.PointInTime, row.Source, row.ReferenceMarket, row.ReferenceSymbol, row.RealtimeMode, row.Active, row.SLAHours, row.Metadata); err != nil {
			return err
		}
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return err
			}
			batch, err = conn.PrepareBatch(ctx, `INSERT INTO macro_factor_catalog (
				dataset, factor_code, display_name, description, value_type, unit,
				preferred_frequency, fill_policy, fill_max_days, point_in_time, source,
				reference_market, reference_symbol, realtime_mode, active, sla_hours, metadata
			)`)
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

func InsertObservations(ctx context.Context, conn driver.Conn, rows []ObservationRow, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	from, to := rows[0].EventTS, rows[0].EventTS
	codesSet := map[string]struct{}{}
	for _, row := range rows {
		if row.EventTS.Before(from) {
			from = row.EventTS
		}
		if row.EventTS.After(to) {
			to = row.EventTS
		}
		codesSet[row.FactorCode] = struct{}{}
	}
	codes := sortedStringSet(codesSet)
	if err := conn.Exec(ctx, `ALTER TABLE macro_observation DELETE WHERE dataset = {dataset:String} AND factor_code IN {codes:Array(String)} AND event_ts >= {from:DateTime} AND event_ts <= {to:DateTime} SETTINGS mutations_sync = 1`, clickhouse.Named("dataset", rows[0].Dataset), clickhouse.Named("codes", codes), clickhouse.Named("from", from.UTC()), clickhouse.Named("to", to.UTC())); err != nil {
		return err
	}
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO macro_observation (
		dataset, factor_code, event_ts, known_at, period_start, period_end, source,
		value, reference_market, reference_symbol, anchor_value, revision
	)`)
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		if err := batch.Append(row.Dataset, row.FactorCode, row.EventTS, row.KnownAt, row.PeriodStart, row.PeriodEnd, row.Source, row.Value, row.ReferenceMarket, row.ReferenceSymbol, row.AnchorValue, row.Revision); err != nil {
			return err
		}
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return err
			}
			batch, err = conn.PrepareBatch(ctx, `INSERT INTO macro_observation (
				dataset, factor_code, event_ts, known_at, period_start, period_end, source,
				value, reference_market, reference_symbol, anchor_value, revision
			)`)
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

func factorDefinitionForKey(key string) factorDefinition {
	if def, ok := knownFactorDefinitions()[key]; ok {
		return def
	}
	return factorDefinition{Code: key, DisplayName: humanizeFactorKey(key), Description: fmt.Sprintf("%s monthly macro field from Gurufocus Shiller dataset", humanizeFactorKey(key)), ValueType: "float", RealtimeMode: realtimeForwardFill}
}

func knownFactorDefinitions() map[string]factorDefinition {
	return map[string]factorDefinition{
		"sp500":                              {Code: "sp500", DisplayName: "S&P 500 Price", Description: "Monthly S&P 500 level from Gurufocus Shiller dataset", ValueType: "index", RealtimeMode: realtimePriceScaled},
		"dividend":                           {Code: "dividend", DisplayName: "Dividend", Description: "Monthly dividend field from Gurufocus Shiller dataset", ValueType: "float", RealtimeMode: realtimeForwardFill},
		"earnings":                           {Code: "earnings", DisplayName: "Earnings", Description: "Monthly earnings field from Gurufocus Shiller dataset", ValueType: "float", RealtimeMode: realtimeForwardFill},
		"CPI":                                {Code: "CPI", DisplayName: "CPI", Description: "Monthly CPI field from Gurufocus Shiller dataset", ValueType: "float", RealtimeMode: realtimeForwardFill},
		"rate_GS10":                          {Code: "rate_GS10", DisplayName: "GS10 Rate", Description: "10-year treasury yield field from Gurufocus Shiller dataset", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		"real_sp":                            {Code: "real_sp", DisplayName: "Real S&P 500", Description: "Monthly inflation-adjusted S&P 500 level", ValueType: "index", RealtimeMode: realtimePriceScaled},
		"real_div":                           {Code: "real_div", DisplayName: "Real Dividend", Description: "Monthly inflation-adjusted dividend field", ValueType: "float", RealtimeMode: realtimeForwardFill},
		"real_earnings":                      {Code: "real_earnings", DisplayName: "Real Earnings", Description: "Monthly inflation-adjusted earnings field", ValueType: "float", RealtimeMode: realtimeForwardFill},
		"pe10":                               {Code: "pe10", DisplayName: "Shiller PE", Description: "Monthly Shiller CAPE ratio", ValueType: "ratio", RealtimeMode: realtimePriceScaled},
		"ractual":                            {Code: "ractual", DisplayName: "Actual Real Return", Description: "Monthly actual real return field", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		"rexpect":                            {Code: "rexpect", DisplayName: "Expected Real Return", Description: "Monthly expected real return field", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		"pe_reg":                             {Code: "pe_reg", DisplayName: "Regression PE", Description: "Monthly regression-based PE ratio", ValueType: "ratio", RealtimeMode: realtimePriceScaled},
		"ir10":                               {Code: "ir10", DisplayName: "IR10", Description: "Monthly IR10 field", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		"excess_cape_yield":                  {Code: "excess_cape_yield", DisplayName: "Excess CAPE Yield", Description: "Monthly excess CAPE yield field", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
		"real_excess_annualized_returns_10y": {Code: "real_excess_annualized_returns_10y", DisplayName: "Real Excess Annualized Returns 10Y", Description: "Monthly real excess annualized returns over 10 years", ValueType: "percent", Unit: "%", RealtimeMode: realtimeForwardFill},
	}
}

func mustMonth(record rawMonthlyRecord) string {
	var month string
	_ = json.Unmarshal(record["date"], &month)
	return month
}

func decodeMonthString(month string) (time.Time, error) { return time.Parse("2006-01", month) }

func decodeFloat(raw json.RawMessage) (float64, bool) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := number.Float64()
		return value, err == nil
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		value, err := strconv.ParseFloat(strings.TrimSpace(stringValue), 64)
		return value, err == nil
	}
	return 0, false
}

func sortedKeys[K ~string, V any](in map[K]V) []K {
	out := make([]K, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

func sortedStringSet(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func humanizeFactorKey(key string) string {
	replacer := strings.NewReplacer("_", " ", "-", " ")
	parts := strings.Fields(replacer.Replace(key))
	for index, part := range parts {
		if part == strings.ToUpper(part) {
			continue
		}
		parts[index] = strings.Title(part)
	}
	return strings.Join(parts, " ")
}

func dateOnly(value time.Time) time.Time {
	value = value.UTC()
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

const (
	gurufocusShillerDataset = "gurufocus-shiller"
	gurufocusShillerURL     = "https://www.gurufocus.cn/_api/indicator/shiller_pe/data?locale=zh-hans"
	defaultReferenceSymbol  = "SPY"
	defaultReferenceMarket  = "us-stocks"
	macroSourceName         = "gurufocus"
	realtimeForwardFill     = "forward_fill"
	realtimePriceScaled     = "price_scaled"
	gurufocusPublicationDay = 12
)

type rawMonthlyRecord map[string]json.RawMessage

type factorDefinition struct {
	Code         string
	DisplayName  string
	Description  string
	ValueType    string
	Unit         string
	RealtimeMode string
}

type macroCatalogRow struct {
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

type macroObservationRow struct {
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
	RawMonth        string
}

type monthAnchor struct {
	StartMonth time.Time
	LastTS     time.Time
	LastClose  float64
	FirstTS    time.Time
}

type tradingDayAnchor struct {
	TradingDay time.Time
	FirstTS    time.Time
	LastTS     time.Time
	LastClose  float64
}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	url := flag.String("url", gurufocusShillerURL, "Macro dataset URL")
	referenceSymbol := flag.String("reference-symbol", defaultReferenceSymbol, "Reference symbol used for timestamp alignment and realtime scaling")
	batchSize := flag.Int("batch-size", 1000, "Rows per ClickHouse batch")
	initSchema := flag.Bool("init-schema", true, "Initialize fundamentals schema before sync")
	schemaFile := flag.String("schema", "", "Path to fundamentals.sql DDL (auto-detected if empty)")
	dryRun := flag.Bool("dry-run", false, "Fetch and align without writing to ClickHouse")
	flag.Parse()

	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	if *initSchema {
		ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.FundamentalsSchemaFile)
		if err != nil {
			log.Fatalf("resolve fundamentals.sql schema: %v", err)
		}
		if err := usmarket.InitFundamentalsSchema(ctx, conn, ddlFile); err != nil {
			log.Fatalf("initialize fundamentals schema: %v", err)
		}
	}

	records, err := fetchMonthlyRecords(ctx, *url)
	if err != nil {
		log.Fatalf("fetch monthly records: %v", err)
	}
	anchors, err := loadMonthAnchors(ctx, conn, strings.ToUpper(strings.TrimSpace(*referenceSymbol)), records)
	if err != nil {
		log.Fatalf("load month anchors: %v", err)
	}
	catalogRows, observationRows, err := buildRows(records, anchors, strings.ToUpper(strings.TrimSpace(*referenceSymbol)))
	if err != nil {
		log.Fatalf("build macro rows: %v", err)
	}
	if *dryRun {
		log.Printf("macro sync dry-run: catalog_rows=%d observation_rows=%d reference_symbol=%s", len(catalogRows), len(observationRows), strings.ToUpper(strings.TrimSpace(*referenceSymbol)))
		return
	}
	if err := upsertMacroCatalog(ctx, conn, catalogRows, *batchSize); err != nil {
		log.Fatalf("upsert macro catalog: %v", err)
	}
	if err := insertMacroObservations(ctx, conn, observationRows, *batchSize); err != nil {
		log.Fatalf("insert macro observations: %v", err)
	}
	log.Printf("macro sync complete: dataset=%s catalog_rows=%d observation_rows=%d reference_symbol=%s", gurufocusShillerDataset, len(catalogRows), len(observationRows), strings.ToUpper(strings.TrimSpace(*referenceSymbol)))
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
		month := mustMonth(record)
		if _, err := decodeMonthString(month); err != nil {
			continue
		}
		filtered = append(filtered, record)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no valid YYYY-MM records found in upstream payload")
	}
	return filtered, nil
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
	queryStart := firstMonth.UTC()
	queryEnd := lastMonth.AddDate(0, 1, 0).UTC()

	rows, err := conn.Query(ctx, `SELECT
		toStartOfMonth(timestamp) AS month_start,
		toDate(timestamp) AS trading_day,
		min(timestamp) AS first_ts,
		max(timestamp) AS last_ts,
		toFloat64(argMax(close, timestamp)) AS last_close
	FROM us_stocks_bar_1m
	WHERE symbol = {symbol:String}
	  AND timestamp >= toDateTime({from:String}, 'UTC')
	  AND timestamp < toDateTime({to:String}, 'UTC')
	  AND is_regular_session = 1
	GROUP BY month_start, trading_day
	ORDER BY month_start, trading_day`,
		clickhouse.Named("symbol", referenceSymbol),
		clickhouse.Named("from", queryStart.Format("2006-01-02 15:04:05")),
		clickhouse.Named("to", queryEnd.Format("2006-01-02 15:04:05")),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	monthDays := map[string][]tradingDayAnchor{}
	for rows.Next() {
		var (
			monthStart time.Time
			tradingDay time.Time
			firstTS    time.Time
			lastTS     time.Time
			lastClose  float64
		)
		if err := rows.Scan(&monthStart, &tradingDay, &firstTS, &lastTS, &lastClose); err != nil {
			return nil, err
		}
		key := monthStart.UTC().Format("2006-01")
		monthDays[key] = append(monthDays[key], tradingDayAnchor{TradingDay: tradingDay.UTC(), FirstTS: firstTS.UTC(), LastTS: lastTS.UTC(), LastClose: lastClose})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(monthDays) == 0 {
		return nil, fmt.Errorf("no us_stocks_bar_1m anchors found for %s", referenceSymbol)
	}
	monthly := make(map[string]monthAnchor, len(monthDays))
	for monthKey, days := range monthDays {
		monthStart, err := decodeMonthString(monthKey)
		if err != nil {
			return nil, err
		}
		anchor, ok := selectGurufocusMonthAnchor(monthStart, days)
		if !ok {
			continue
		}
		monthly[monthKey] = anchor
	}
	if len(monthly) == 0 {
		return nil, fmt.Errorf("no publication-day anchors found for %s", referenceSymbol)
	}
	return monthly, nil
}

func selectGurufocusMonthAnchor(monthStart time.Time, days []tradingDayAnchor) (monthAnchor, bool) {
	if len(days) == 0 {
		return monthAnchor{}, false
	}
	sort.Slice(days, func(i, j int) bool { return days[i].TradingDay.Before(days[j].TradingDay) })
	targetDay := time.Date(monthStart.UTC().Year(), monthStart.UTC().Month(), gurufocusPublicationDay, 0, 0, 0, 0, time.UTC)
	selected := days[len(days)-1]
	for _, candidate := range days {
		selected = candidate
		if !candidate.TradingDay.Before(targetDay) {
			break
		}
	}
	return monthAnchor{StartMonth: monthStart.UTC(), LastTS: selected.LastTS.UTC(), LastClose: selected.LastClose, FirstTS: selected.FirstTS.UTC()}, true
}

func buildRows(records []rawMonthlyRecord, anchors map[string]monthAnchor, referenceSymbol string) ([]macroCatalogRow, []macroObservationRow, error) {
	definitions := make(map[string]factorDefinition)
	observations := make([]macroObservationRow, 0, len(records)*8)
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
		periodEnd := periodStart.AddDate(0, 1, 0)
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
			observations = append(observations, macroObservationRow{
				Dataset:         gurufocusShillerDataset,
				FactorCode:      key,
				EventTS:         anchor.LastTS,
				KnownAt:         anchor.FirstTS,
				PeriodStart:     periodStart,
				PeriodEnd:       periodEnd,
				Source:          macroSourceName,
				Value:           value,
				ReferenceMarket: defaultReferenceMarket,
				ReferenceSymbol: referenceSymbol,
				AnchorValue:     anchorValue,
				RawMonth:        month,
			})
		}
	}
	catalogRows := make([]macroCatalogRow, 0, len(definitions))
	for _, key := range sortedKeys(definitions) {
		definition := definitions[key]
		catalogRows = append(catalogRows, macroCatalogRow{
			Dataset:            gurufocusShillerDataset,
			FactorCode:         definition.Code,
			DisplayName:        definition.DisplayName,
			Description:        definition.Description,
			ValueType:          definition.ValueType,
			Unit:               definition.Unit,
			PreferredFrequency: "monthly",
			FillPolicy:         "forward_fill",
			FillMaxDays:        0,
			PointInTime:        1,
			Source:             macroSourceName,
			ReferenceMarket:    defaultReferenceMarket,
			ReferenceSymbol:    referenceSymbol,
			RealtimeMode:       definition.RealtimeMode,
			Active:             1,
			SLAHours:           24 * 45,
			Metadata:           fmt.Sprintf(`{"dataset":"%s","raw_field":"%s"}`, gurufocusShillerDataset, definition.Code),
		})
	}
	return catalogRows, observations, nil
}

func upsertMacroCatalog(ctx context.Context, conn driver.Conn, rows []macroCatalogRow, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	prepare := func() (driver.Batch, error) {
		return conn.PrepareBatch(ctx, `INSERT INTO macro_factor_catalog (
			dataset, factor_code, display_name, description, value_type, unit,
			preferred_frequency, fill_policy, fill_max_days, point_in_time, source,
			reference_market, reference_symbol, realtime_mode, active, sla_hours, metadata
		)`)
	}
	batch, err := prepare()
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		if err := batch.Append(
			row.Dataset,
			row.FactorCode,
			row.DisplayName,
			row.Description,
			row.ValueType,
			row.Unit,
			row.PreferredFrequency,
			row.FillPolicy,
			row.FillMaxDays,
			row.PointInTime,
			row.Source,
			row.ReferenceMarket,
			row.ReferenceSymbol,
			row.RealtimeMode,
			row.Active,
			row.SLAHours,
			row.Metadata,
		); err != nil {
			return err
		}
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return err
			}
			batch, err = prepare()
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

func insertMacroObservations(ctx context.Context, conn driver.Conn, rows []macroObservationRow, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	existing, err := loadExistingMacroRevisions(ctx, conn, rows)
	if err != nil {
		return err
	}
	prepare := func() (driver.Batch, error) {
		return conn.PrepareBatch(ctx, `INSERT INTO macro_observation (
			dataset, factor_code, event_ts, known_at, period_start, period_end, source,
			value, reference_market, reference_symbol, anchor_value, revision
		)`)
	}
	batch, err := prepare()
	if err != nil {
		return err
	}
	pending := 0
	for _, row := range rows {
		key := fmt.Sprintf("%s|%s|%s|%s", row.Dataset, row.FactorCode, row.EventTS.UTC().Format(time.RFC3339Nano), row.KnownAt.UTC().Format(time.RFC3339Nano))
		if rev, ok := existing[key]; ok {
			row.Revision = rev + 1
		}
		if err := batch.Append(
			row.Dataset,
			row.FactorCode,
			row.EventTS,
			row.KnownAt,
			row.PeriodStart,
			row.PeriodEnd,
			row.Source,
			row.Value,
			row.ReferenceMarket,
			row.ReferenceSymbol,
			row.AnchorValue,
			row.Revision,
		); err != nil {
			return err
		}
		pending++
		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return err
			}
			batch, err = prepare()
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

func loadExistingMacroRevisions(ctx context.Context, conn driver.Conn, rows []macroObservationRow) (map[string]uint32, error) {
	if len(rows) == 0 {
		return map[string]uint32{}, nil
	}
	byDataset := map[string]timeRange{}
	for _, row := range rows {
		current, ok := byDataset[row.Dataset]
		if !ok {
			byDataset[row.Dataset] = timeRange{From: row.EventTS, To: row.KnownAt}
			continue
		}
		if row.EventTS.Before(current.From) {
			current.From = row.EventTS
		}
		if row.KnownAt.After(current.To) {
			current.To = row.KnownAt
		}
		byDataset[row.Dataset] = current
	}
	out := map[string]uint32{}
	for dataset, tr := range byDataset {
		queryRows, err := conn.Query(ctx, `SELECT dataset, factor_code, event_ts, known_at, max(revision)
		FROM macro_observation
		WHERE dataset = {dataset:String}
		  AND event_ts >= toDateTime({from:String}, 'UTC')
		  AND known_at <= toDateTime({to:String}, 'UTC')
		GROUP BY dataset, factor_code, event_ts, known_at`,
			clickhouse.Named("dataset", dataset),
			clickhouse.Named("from", tr.From.UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("to", tr.To.AddDate(0, 1, 0).UTC().Format("2006-01-02 15:04:05")),
		)
		if err != nil {
			return nil, err
		}
		for queryRows.Next() {
			var (
				loadedDataset string
				factorCode    string
				eventTS       time.Time
				knownAt       time.Time
				revision      uint32
			)
			if err := queryRows.Scan(&loadedDataset, &factorCode, &eventTS, &knownAt, &revision); err != nil {
				queryRows.Close()
				return nil, err
			}
			key := fmt.Sprintf("%s|%s|%s|%s", loadedDataset, factorCode, eventTS.UTC().Format(time.RFC3339Nano), knownAt.UTC().Format(time.RFC3339Nano))
			out[key] = revision
		}
		if err := queryRows.Err(); err != nil {
			queryRows.Close()
			return nil, err
		}
		queryRows.Close()
	}
	return out, nil
}

type timeRange struct {
	From time.Time
	To   time.Time
}

func factorDefinitionForKey(key string) factorDefinition {
	if def, ok := knownFactorDefinitions()[key]; ok {
		return def
	}
	return factorDefinition{
		Code:         key,
		DisplayName:  humanizeFactorKey(key),
		Description:  fmt.Sprintf("%s monthly macro field from Gurufocus Shiller dataset", humanizeFactorKey(key)),
		ValueType:    "float",
		RealtimeMode: realtimeForwardFill,
	}
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

func sortedKeys[K ~string, V any](in map[K]V) []K {
	out := make([]K, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
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

func mustMonth(record rawMonthlyRecord) string {
	monthRaw := record["date"]
	var month string
	_ = json.Unmarshal(monthRaw, &month)
	return month
}

func decodeMonthString(month string) (time.Time, error) {
	return time.Parse("2006-01", month)
}

func decodeFloat(raw json.RawMessage) (float64, bool) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := number.Float64()
		return value, err == nil
	}
	var floatValue float64
	if err := json.Unmarshal(raw, &floatValue); err == nil {
		return floatValue, true
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		value, err := strconv.ParseFloat(strings.TrimSpace(stringValue), 64)
		return value, err == nil
	}
	return 0, false
}

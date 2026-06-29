package macro

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/feeds/dvol"
)

const (
	DefaultDeribitDVOLBTCDataset = "deribit-dvol-btc"
	DefaultDeribitDVOLETHDataset = "deribit-dvol-eth"
	DefaultDeribitDVOLSource     = "deribit"
	DefaultCryptoReferenceMarket = "crypto"
	defaultHourlyFrequency       = "hourly"
)

type DeribitDVOLConfig struct {
	Symbols   []string
	BatchSize int
}

type DeribitDVOLResult struct {
	CatalogRows     int
	ObservationRows int
	Points          int
}

type DeribitDVOLBar struct {
	Symbol    string
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
}

func SyncDeribitDVOLFromDeribit(ctx context.Context, conn driver.Conn, cfg DeribitDVOLConfig, from, to time.Time, dryRun bool) (DeribitDVOLResult, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	client := dvol.NewClient(dvol.DefaultBaseURL)
	symbols := normalizeDeribitDVOLSymbols(cfg.Symbols)
	result := DeribitDVOLResult{}
	for _, symbol := range symbols {
		dataset, ok := DeribitDVOLDatasetForSymbol(symbol)
		if !ok {
			return DeribitDVOLResult{}, fmt.Errorf("unsupported DVOL symbol %q", symbol)
		}
		bars, err := queryDeribitDVOLAPIBars(ctx, client, symbol, from, to)
		if err != nil {
			return DeribitDVOLResult{}, err
		}
		catalogRows := BuildDeribitDVOLCatalogRows(dataset, symbol)
		observationRows := BuildDeribitDVOLObservationRows(dataset, bars, symbol)
		result.CatalogRows += len(catalogRows)
		result.ObservationRows += len(observationRows)
		result.Points += len(bars)
		if dryRun {
			continue
		}
		if err := UpsertCatalog(ctx, conn, catalogRows, cfg.BatchSize); err != nil {
			return DeribitDVOLResult{}, err
		}
		if err := InsertObservations(ctx, conn, observationRows, cfg.BatchSize); err != nil {
			return DeribitDVOLResult{}, err
		}
	}
	return result, nil
}

func DeribitDVOLDatasetForSymbol(symbol string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "BTC":
		return DefaultDeribitDVOLBTCDataset, true
	case "ETH":
		return DefaultDeribitDVOLETHDataset, true
	default:
		return "", false
	}
}

func DeribitDVOLSymbolForDataset(dataset string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(dataset)) {
	case DefaultDeribitDVOLBTCDataset:
		return "BTC", true
	case DefaultDeribitDVOLETHDataset:
		return "ETH", true
	default:
		return "", false
	}
}

func IsDeribitDVOLDataset(dataset string) bool {
	_, ok := DeribitDVOLSymbolForDataset(dataset)
	return ok
}

func BuildDeribitDVOLCatalogRows(dataset, symbol string) []CatalogRow {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	definitions := []factorDefinition{
		{Code: "open", DisplayName: symbol + " DVOL Open", Description: symbol + " Deribit volatility index hourly opening level", ValueType: "index", RealtimeMode: realtimeForwardFill},
		{Code: "high", DisplayName: symbol + " DVOL High", Description: symbol + " Deribit volatility index hourly high level", ValueType: "index", RealtimeMode: realtimeForwardFill},
		{Code: "low", DisplayName: symbol + " DVOL Low", Description: symbol + " Deribit volatility index hourly low level", ValueType: "index", RealtimeMode: realtimeForwardFill},
		{Code: "close", DisplayName: symbol + " DVOL Close", Description: symbol + " Deribit volatility index hourly closing level", ValueType: "index", RealtimeMode: realtimeForwardFill},
	}
	rows := make([]CatalogRow, 0, len(definitions))
	for _, definition := range definitions {
		rows = append(rows, CatalogRow{Dataset: dataset, FactorCode: definition.Code, DisplayName: definition.DisplayName, Description: definition.Description, ValueType: definition.ValueType, Unit: definition.Unit, PreferredFrequency: defaultHourlyFrequency, FillPolicy: "forward_fill", PointInTime: 1, Source: DefaultDeribitDVOLSource, ReferenceMarket: DefaultCryptoReferenceMarket, ReferenceSymbol: symbol, RealtimeMode: definition.RealtimeMode, Active: 1, SLAHours: 3, Metadata: fmt.Sprintf(`{"dataset":"%s","source":"%s","symbol":"%s","source_interval":"1h","factor":"%s"}`, dataset, DefaultDeribitDVOLSource, symbol, definition.Code)})
	}
	return rows
}

func BuildDeribitDVOLObservationRows(dataset string, bars []DeribitDVOLBar, symbol string) []ObservationRow {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	rows := make([]ObservationRow, 0, len(bars)*4)
	for _, bar := range bars {
		eventTS := bar.Timestamp.UTC()
		periodEnd := eventTS.Add(time.Hour)
		rows = appendDeribitDVOLObservation(rows, dataset, "open", bar.Open, eventTS, periodEnd, symbol)
		rows = appendDeribitDVOLObservation(rows, dataset, "high", bar.High, eventTS, periodEnd, symbol)
		rows = appendDeribitDVOLObservation(rows, dataset, "low", bar.Low, eventTS, periodEnd, symbol)
		rows = appendDeribitDVOLObservation(rows, dataset, "close", bar.Close, eventTS, periodEnd, symbol)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].EventTS.Equal(rows[j].EventTS) {
			return rows[i].FactorCode < rows[j].FactorCode
		}
		return rows[i].EventTS.Before(rows[j].EventTS)
	})
	return rows
}

func appendDeribitDVOLObservation(rows []ObservationRow, dataset, factor string, value float64, eventTS, periodEnd time.Time, symbol string) []ObservationRow {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return rows
	}
	return append(rows, ObservationRow{Dataset: dataset, FactorCode: factor, EventTS: eventTS, KnownAt: eventTS, PeriodStart: eventTS, PeriodEnd: periodEnd, Source: DefaultDeribitDVOLSource, Value: value, ReferenceMarket: DefaultCryptoReferenceMarket, ReferenceSymbol: symbol, AnchorValue: math.NaN()})
}

func normalizeDeribitDVOLSymbols(symbols []string) []string {
	if len(symbols) == 0 {
		return []string{"BTC", "ETH"}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalized := strings.ToUpper(strings.TrimSpace(symbol))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return []string{"BTC", "ETH"}
	}
	return out
}

func queryDeribitDVOLAPIBars(ctx context.Context, client *dvol.Client, symbol string, from, to time.Time) ([]DeribitDVOLBar, error) {
	rows, err := client.GetHistory(ctx, symbol, "3600", from, to)
	if err != nil {
		return nil, fmt.Errorf("fetch Deribit %s DVOL bars: %w", symbol, err)
	}
	bars := make([]DeribitDVOLBar, 0, len(rows))
	for _, row := range rows {
		bars = append(bars, DeribitDVOLBar{Symbol: strings.ToUpper(row.Symbol), Timestamp: row.Timestamp.UTC(), Open: row.Open, High: row.High, Low: row.Low, Close: row.Close})
	}
	return bars, nil
}

package macro

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	cbvix "github.com/Cyvadra/toktik/pkg/cboe/vix"
)

const (
	DefaultCBOEVIXDataset    = "cboe-vix"
	DefaultCBOEVIXSource     = "cboe"
	DefaultCBOEVIXReference  = "SPY"
	defaultDailyFrequency    = "daily"
	defaultDailyMarketCloseH = 16
)

var newYorkLocation = mustLoadLocation("America/New_York")

type CBOEVIXConfig struct {
	HistoryURL       string
	ReferenceSymbol  string
	BatchSize        int
	Client           *cbvix.Client
	MarketCloseHour  int
	MarketCloseMin   int
	MarketCloseSec   int
	PreferredDataset string
}

type CBOEVIXResult struct {
	CatalogRows     int
	ObservationRows int
	Points          int
}

func SyncCBOEVIX(ctx context.Context, conn driver.Conn, cfg CBOEVIXConfig, from, to time.Time, dryRun bool) (CBOEVIXResult, error) {
	client := cfg.Client
	if client == nil {
		options := make([]cbvix.Option, 0, 1)
		if strings.TrimSpace(cfg.HistoryURL) != "" {
			options = append(options, cbvix.WithHistoryURL(cfg.HistoryURL))
		}
		client = cbvix.New(options...)
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	referenceSymbol := strings.ToUpper(strings.TrimSpace(cfg.ReferenceSymbol))
	if referenceSymbol == "" {
		referenceSymbol = DefaultCBOEVIXReference
	}
	dataset := strings.TrimSpace(cfg.PreferredDataset)
	if dataset == "" {
		dataset = DefaultCBOEVIXDataset
	}
	bars, err := client.FetchHistory(ctx)
	if err != nil {
		return CBOEVIXResult{}, err
	}
	filtered := filterCBOEVIXBarsByWindow(bars, from, to)
	catalogRows := buildCBOEVIXCatalogRows(dataset, referenceSymbol)
	observationRows := buildCBOEVIXObservationRows(dataset, filtered, referenceSymbol, cfg)
	if dryRun {
		return CBOEVIXResult{CatalogRows: len(catalogRows), ObservationRows: len(observationRows), Points: len(filtered)}, nil
	}
	if err := UpsertCatalog(ctx, conn, catalogRows, cfg.BatchSize); err != nil {
		return CBOEVIXResult{}, err
	}
	if err := InsertObservations(ctx, conn, observationRows, cfg.BatchSize); err != nil {
		return CBOEVIXResult{}, err
	}
	return CBOEVIXResult{CatalogRows: len(catalogRows), ObservationRows: len(observationRows), Points: len(filtered)}, nil
}

func filterCBOEVIXBarsByWindow(bars []cbvix.Bar, from, to time.Time) []cbvix.Bar {
	if from.IsZero() && to.IsZero() {
		return bars
	}
	filtered := make([]cbvix.Bar, 0, len(bars))
	for _, bar := range bars {
		day := dateOnly(bar.Date)
		if !from.IsZero() && day.Before(dateOnly(from)) {
			continue
		}
		if !to.IsZero() && !day.Before(dateOnly(to)) {
			continue
		}
		filtered = append(filtered, bar)
	}
	return filtered
}

func buildCBOEVIXCatalogRows(dataset, referenceSymbol string) []CatalogRow {
	definitions := []factorDefinition{
		{Code: "open", DisplayName: "VIX Open", Description: "Daily VIX opening level from CBOE official history", ValueType: "index", RealtimeMode: realtimeForwardFill},
		{Code: "high", DisplayName: "VIX High", Description: "Daily VIX high level from CBOE official history", ValueType: "index", RealtimeMode: realtimeForwardFill},
		{Code: "low", DisplayName: "VIX Low", Description: "Daily VIX low level from CBOE official history", ValueType: "index", RealtimeMode: realtimeForwardFill},
		{Code: "close", DisplayName: "VIX Close", Description: "Daily VIX close level from CBOE official history", ValueType: "index", RealtimeMode: realtimeForwardFill},
	}
	rows := make([]CatalogRow, 0, len(definitions))
	for _, definition := range definitions {
		rows = append(rows, CatalogRow{Dataset: dataset, FactorCode: definition.Code, DisplayName: definition.DisplayName, Description: definition.Description, ValueType: definition.ValueType, Unit: definition.Unit, PreferredFrequency: defaultDailyFrequency, FillPolicy: "forward_fill", PointInTime: 1, Source: DefaultCBOEVIXSource, ReferenceMarket: DefaultReferenceMarket, ReferenceSymbol: referenceSymbol, RealtimeMode: definition.RealtimeMode, Active: 1, SLAHours: 24 * 2, Metadata: fmt.Sprintf(`{"dataset":"%s","source":"%s","factor":"%s"}`, dataset, DefaultCBOEVIXSource, definition.Code)})
	}
	return rows
}

func buildCBOEVIXObservationRows(dataset string, bars []cbvix.Bar, referenceSymbol string, cfg CBOEVIXConfig) []ObservationRow {
	rows := make([]ObservationRow, 0, len(bars)*4)
	for _, bar := range bars {
		eventTS := cboeDailyCloseTimestamp(bar.Date, cfg)
		periodStart := dateOnly(bar.Date)
		periodEnd := periodStart.AddDate(0, 0, 1)
		rows = appendCBOEVIXObservation(rows, dataset, "open", bar.Open, eventTS, periodStart, periodEnd, referenceSymbol)
		rows = appendCBOEVIXObservation(rows, dataset, "high", bar.High, eventTS, periodStart, periodEnd, referenceSymbol)
		rows = appendCBOEVIXObservation(rows, dataset, "low", bar.Low, eventTS, periodStart, periodEnd, referenceSymbol)
		rows = appendCBOEVIXObservation(rows, dataset, "close", bar.Close, eventTS, periodStart, periodEnd, referenceSymbol)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].EventTS.Equal(rows[j].EventTS) {
			return rows[i].FactorCode < rows[j].FactorCode
		}
		return rows[i].EventTS.Before(rows[j].EventTS)
	})
	return rows
}

func appendCBOEVIXObservation(rows []ObservationRow, dataset, factor string, value float64, eventTS, periodStart, periodEnd time.Time, referenceSymbol string) []ObservationRow {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return rows
	}
	return append(rows, ObservationRow{Dataset: dataset, FactorCode: factor, EventTS: eventTS, KnownAt: eventTS, PeriodStart: periodStart, PeriodEnd: periodEnd, Source: DefaultCBOEVIXSource, Value: value, ReferenceMarket: DefaultReferenceMarket, ReferenceSymbol: referenceSymbol, AnchorValue: math.NaN()})
}

func cboeDailyCloseTimestamp(day time.Time, cfg CBOEVIXConfig) time.Time {
	hour := cfg.MarketCloseHour
	minute := cfg.MarketCloseMin
	second := cfg.MarketCloseSec
	if hour == 0 && minute == 0 && second == 0 {
		hour = defaultDailyMarketCloseH
	}
	localDay := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), hour, minute, second, 0, newYorkLocation)
	return localDay.UTC()
}

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

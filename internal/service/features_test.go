package service

import (
	"math"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestSummarizeDatasets(t *testing.T) {
	summary := summarizeDatasets([]dto.DatasetDescriptor{
		{Name: "crypto-options-bars", Market: "crypto-options", Status: "ready"},
		{Name: "crypto-spot-bars", Market: "crypto-spot", Status: "stale"},
		{Name: "us-options-bars", Market: "us-options", Status: "missing"},
		{Name: "us-options-chain", Market: "us-options", Status: "empty"},
		{Name: "feature-volatility-snapshots", Market: "feature-store", Status: "ready"},
	})

	if summary.Total != 5 || summary.Ready != 2 || summary.Stale != 1 || summary.Missing != 1 || summary.Empty != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(summary.Markets) != 4 {
		t.Fatalf("expected 4 market summaries, got %d", len(summary.Markets))
	}
}

func TestRollingHistoricalVolatility(t *testing.T) {
	prices := []featurePoint{
		{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Value: 100},
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Value: 102},
		{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Value: 101},
		{Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Value: 103},
	}
	value := rollingHistoricalVolatility(prices, 3, 365)
	if value == nil {
		t.Fatal("expected hv value")
	}
	if *value <= 0 || math.IsNaN(*value) {
		t.Fatalf("unexpected hv value: %v", *value)
	}
}

func TestImpliedVolatilityMetrics(t *testing.T) {
	values := []featurePoint{
		{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Value: 0.20},
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Value: 0.25},
		{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Value: 0.30},
		{Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Value: 0.35},
	}
	percentile := impliedVolatilityPercentile(values)
	rank := impliedVolatilityRank(values)
	if percentile == nil || rank == nil {
		t.Fatal("expected percentile and rank values")
	}
	if *percentile != 100 {
		t.Fatalf("expected percentile 100, got %v", *percentile)
	}
	if *rank != 100 {
		t.Fatalf("expected rank 100, got %v", *rank)
	}
}

func TestBuildVolatilityHistoryRows(t *testing.T) {
	prices := []featurePoint{
		{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Value: 100},
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Value: 101},
		{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Value: 102},
		{Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Value: 103},
	}
	ivs := []featurePoint{
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Value: 0.21},
		{Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Value: 0.22},
		{Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Value: 0.23},
	}
	rows := buildVolatilityHistoryRows(prices, ivs, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), 252, 365)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[2].CurrentIV == nil || *rows[2].CurrentIV != 0.23 {
		t.Fatalf("unexpected current IV: %+v", rows[2].CurrentIV)
	}
	if rows[2].IVPercentile == nil || *rows[2].IVPercentile != 100 {
		t.Fatalf("unexpected IV percentile: %+v", rows[2].IVPercentile)
	}
	if rows[2].PriceObservations != 4 || rows[2].IVObservations != 3 {
		t.Fatalf("unexpected observation counts: %+v", rows[2])
	}
}

func TestBuildUSOptionsCurrentIVSeriesUsesNearest30DayATM(t *testing.T) {
	atmFar := 0.61
	atmNear := 0.49
	atmNext := 0.45
	atmNextNear := 0.44
	series := buildUSOptionsCurrentIVSeries([]usOptionsSurfaceAggregateRow{
		{
			AsOfDate:     time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			Expiration:   time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC),
			DaysToExpiry: 8,
			ATMIV:        &atmFar,
		},
		{
			AsOfDate:     time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			Expiration:   time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
			DaysToExpiry: 28,
			ATMIV:        &atmNear,
		},
		{
			AsOfDate:     time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			Expiration:   time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC),
			DaysToExpiry: 7,
			ATMIV:        &atmNext,
		},
		{
			AsOfDate:     time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			Expiration:   time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
			DaysToExpiry: 28,
			ATMIV:        &atmNextNear,
		},
		{
			AsOfDate:     time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			Expiration:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
			DaysToExpiry: 28,
			ATMIV:        nil,
		},
	}, 30)

	if len(series) != 2 {
		t.Fatalf("expected 2 series points, got %d", len(series))
	}
	if series[0].Date != time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC) || series[0].Value != atmNear {
		t.Fatalf("unexpected first point: %+v", series[0])
	}
	if series[1].Date != time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC) || series[1].Value != atmNextNear {
		t.Fatalf("unexpected second point: %+v", series[1])
	}
}

func TestBuildTermStructureSnapshotRows(t *testing.T) {
	atmIV := 0.24
	callIV := 0.22
	putIV := 0.26
	rows := buildTermStructureSnapshotRows([]usOptionsSurfaceAggregateRow{
		{
			AsOfDate:      time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC),
			Expiration:    time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC),
			DaysToExpiry:  14,
			ATMIV:         &atmIV,
			CallIV:        &callIV,
			PutIV:         &putIV,
			ContractCount: 18,
		},
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ATMIV == nil || *rows[0].ATMIV != atmIV {
		t.Fatalf("unexpected atm iv: %+v", rows[0].ATMIV)
	}
	if rows[0].ContractCount != 18 {
		t.Fatalf("unexpected contract count: %+v", rows[0])
	}
}

func TestBuildSkewSnapshotRows(t *testing.T) {
	otmCallIV := 0.19
	otmPutIV := 0.31
	rows := buildSkewSnapshotRows([]usOptionsSurfaceAggregateRow{
		{
			AsOfDate:      time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC),
			Expiration:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			DaysToExpiry:  28,
			OTMCallIV:     &otmCallIV,
			OTMPutIV:      &otmPutIV,
			ContractCount: 24,
		},
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].PutCallSkew == nil {
		t.Fatal("expected put-call skew")
	}
	if *rows[0].PutCallSkew != 0.12 {
		t.Fatalf("unexpected put-call skew: %v", *rows[0].PutCallSkew)
	}
	if rows[0].ContractCount != 24 {
		t.Fatalf("unexpected contract count: %+v", rows[0])
	}
}

func TestBuildLiquiditySnapshotRows(t *testing.T) {
	bid := 12.5
	ask := 13.0
	mark := 12.75
	spread := 0.0392156862745098
	oi := 1520.0
	rows := buildLiquiditySnapshotRows([]cryptoLiquidityAggregateRow{{
		AsOfDate:              time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC),
		Expiration:            time.Date(2026, 4, 26, 8, 0, 0, 0, time.UTC),
		DaysToExpiry:          23,
		AvgBidClose:           &bid,
		AvgAskClose:           &ask,
		AvgMarkClose:          &mark,
		RelativeSpread:        &spread,
		OpenInterest:          &oi,
		TickCount:             77,
		Volume:                144,
		Transactions:          21,
		ContractCount:         12,
		ActiveContractCount:   10,
		TradableContractCount: 9,
	}})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].TradabilityRatio == nil || *rows[0].TradabilityRatio != 0.75 {
		t.Fatalf("unexpected tradability ratio: %+v", rows[0].TradabilityRatio)
	}
	if rows[0].TickCount != 77 || rows[0].Volume != 144 || rows[0].Transactions != 21 || rows[0].ContractCount != 12 {
		t.Fatalf("unexpected liquidity row: %+v", rows[0])
	}
	if rows[0].ActivityRatio == nil || *rows[0].ActivityRatio != (10.0/12.0) {
		t.Fatalf("unexpected activity ratio: %+v", rows[0].ActivityRatio)
	}
}

func TestBuildLiquiditySnapshotRowsWithoutQuoteSupport(t *testing.T) {
	rows := buildLiquiditySnapshotRows([]cryptoLiquidityAggregateRow{{
		AsOfDate:            time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC),
		Expiration:          time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		DaysToExpiry:        23,
		AvgMarkClose:        floatPtr(12.75),
		Volume:              144,
		Transactions:        21,
		ContractCount:       12,
		ActiveContractCount: 10,
	}})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].TradableContractCount != 0 {
		t.Fatalf("expected zero tradable contracts when quote support is absent, got %d", rows[0].TradableContractCount)
	}
	if rows[0].TradabilityRatio == nil || *rows[0].TradabilityRatio != 0 {
		t.Fatalf("expected zero tradability ratio in generic snapshot rows, got %+v", rows[0].TradabilityRatio)
	}
	if rows[0].AvgBidClose != nil || rows[0].AvgAskClose != nil || rows[0].RelativeSpread != nil || rows[0].OpenInterest != nil {
		t.Fatalf("expected quote-derived fields to stay nil without quote support, got %+v", rows[0])
	}
	if rows[0].ActivityRatio == nil || *rows[0].ActivityRatio != (10.0/12.0) {
		t.Fatalf("unexpected activity ratio: %+v", rows[0].ActivityRatio)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func TestTradabilityRatioValue(t *testing.T) {
	value := tradabilityRatioValue(3, 4)
	if value == nil || *value != 0.75 {
		t.Fatalf("unexpected tradability ratio: %+v", value)
	}
	if tradabilityRatioValue(0, 0) != nil {
		t.Fatal("expected nil ratio when contract count is zero")
	}
}

func TestLiquidityTradabilityRatioValueReturnsNilForUSWithoutQuotes(t *testing.T) {
	value := liquidityTradabilityRatioValue("us-options", cryptoLiquidityAggregateRow{ContractCount: 12})
	if value != nil {
		t.Fatalf("expected nil tradability ratio for US rows without bid/ask support, got %+v", value)
	}

	value = liquidityTradabilityRatioValue("crypto-options", cryptoLiquidityAggregateRow{ContractCount: 12})
	if value == nil || *value != 0 {
		t.Fatalf("expected zero tradability ratio for crypto rows with zero tradable contracts, got %+v", value)
	}
}

func TestLiquidityTradabilityRatioValueReturnsZeroForUSWithQuotes(t *testing.T) {
	bid := 1.2
	ask := 1.4
	value := liquidityTradabilityRatioValue("us-options", cryptoLiquidityAggregateRow{
		ContractCount:         12,
		TradableContractCount: 0,
		AvgBidClose:           &bid,
		AvgAskClose:           &ask,
	})
	if value == nil || *value != 0 {
		t.Fatalf("expected zero tradability ratio for US rows with bid/ask quotes, got %+v", value)
	}
}

func TestActivityRatioValue(t *testing.T) {
	value := activityRatioValue(2, 5)
	if value == nil || *value != 0.4 {
		t.Fatalf("unexpected activity ratio: %+v", value)
	}
	if activityRatioValue(0, 0) != nil {
		t.Fatal("expected nil activity ratio when contract count is zero")
	}
}

func TestBuildLiquidityHistoryRows(t *testing.T) {
	rows := buildLiquidityHistoryRows([]cryptoLiquidityAggregateRow{{
		AsOfDate:              time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC),
		Expiration:            time.Date(2026, 4, 26, 8, 0, 0, 0, time.UTC),
		DaysToExpiry:          23,
		Volume:                100,
		Transactions:          8,
		ContractCount:         10,
		ActiveContractCount:   5,
		TradableContractCount: 4,
	}})
	if len(rows) != 1 {
		t.Fatalf("expected 1 history row, got %d", len(rows))
	}
	if rows[0].AsOfDate.Format("2006-01-02") != "2026-04-03" {
		t.Fatalf("unexpected as_of_date: %+v", rows[0].AsOfDate)
	}
	if rows[0].ActivityRatio == nil || *rows[0].ActivityRatio != 0.5 {
		t.Fatalf("unexpected history activity ratio: %+v", rows[0].ActivityRatio)
	}
}

func TestMergeDailyFeaturePanelRows(t *testing.T) {
	hv20 := 0.24
	currentIV := 0.31
	activityRatio := 0.5
	tradabilityRatio := 0.4
	frontATMIV := 0.29
	frontSkew := 0.08
	frontDTE := 17
	surfaceContracts := 22
	frontExpiration := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	openInterest := 1200.0
	spread := 0.041
	daysFromPrev := 3
	daysToNext := 5

	rows := mergeDailyFeaturePanelRows(
		[]dto.FeatureVolatilityHistoryRow{{
			Date:              time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
			PriceObservations: 252,
			IVObservations:    252,
			HV20:              &hv20,
			CurrentIV:         &currentIV,
		}},
		[]dto.FeatureLiquidityHistoryRow{{
			AsOfDate: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
			FeatureLiquiditySnapshotRow: dto.FeatureLiquiditySnapshotRow{
				OpenInterest:          &openInterest,
				RelativeSpread:        &spread,
				TickCount:             120,
				Volume:                4500,
				Transactions:          210,
				ContractCount:         10,
				ActiveContractCount:   5,
				TradableContractCount: 4,
				ActivityRatio:         &activityRatio,
				TradabilityRatio:      &tradabilityRatio,
			},
		}},
		map[string]featureSurfacePanelSummary{
			"2026-03-31": {
				Expiration:    &frontExpiration,
				DaysToExpiry:  &frontDTE,
				ATMIV:         &frontATMIV,
				PutCallSkew:   &frontSkew,
				ContractCount: &surfaceContracts,
			},
		},
		map[string]dto.FeatureEventWindowHistoryRow{
			"2026-03-31": {
				Date: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
				FeatureEventWindowSnapshotResponse: dto.FeatureEventWindowSnapshotResponse{
					IsEarlyClose:        true,
					DaysFromPrevHoliday: &daysFromPrev,
					DaysToNextHoliday:   &daysToNext,
				},
			},
		},
	)

	if len(rows) != 1 {
		t.Fatalf("expected 1 panel row, got %d", len(rows))
	}
	row := rows[0]
	if row.HV20 == nil || *row.HV20 != hv20 || row.CurrentIV == nil || *row.CurrentIV != currentIV {
		t.Fatalf("unexpected volatility payload: %+v", row)
	}
	if row.LiquidityVolume != 4500 || row.LiquidityTransactions != 210 || row.LiquidityTickCount != 120 {
		t.Fatalf("unexpected liquidity payload: %+v", row)
	}
	if row.FrontExpiration == nil || !row.FrontExpiration.Equal(frontExpiration) || row.FrontDaysToExpiry == nil || *row.FrontDaysToExpiry != frontDTE {
		t.Fatalf("unexpected surface payload: %+v", row)
	}
	if !row.IsEarlyClose || row.DaysFromPrevHoliday == nil || *row.DaysFromPrevHoliday != daysFromPrev || row.DaysToNextHoliday == nil || *row.DaysToNextHoliday != daysToNext {
		t.Fatalf("unexpected event payload: %+v", row)
	}
}

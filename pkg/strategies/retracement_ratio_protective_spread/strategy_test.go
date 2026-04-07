package retracementratioprotectivespread

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestParseSignalSource(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "12h", raw: "12h", want: "12h"},
		{name: "1d", raw: "1d", want: "1d"},
		{name: "trim and lower", raw: " 12H ", want: "12h"},
		{name: "missing", raw: "", wantErr: true},
		{name: "invalid", raw: "6h", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSignalSource(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSignalSource(%q) expected error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSignalSource(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseSignalSource(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadSignalTimesFromCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.csv")
	content := "交易 #,类型,日期和时间,信号\n1,多头进场,2023-01-06 08:00,Long_Init\n2,多头进场,2023-01-06 08:00,Long_Init\n3,多头进场,2023-05-03 08:00,Long_Init\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	times, err := loadSignalTimesFromCSV(path)
	if err != nil {
		t.Fatalf("loadSignalTimesFromCSV() error = %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len(times) = %d, want 2", len(times))
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	for _, raw := range []string{"2023-01-06 08:00", "2023-05-03 08:00"} {
		ts, err := time.ParseInLocation(signalTimeLayout, raw, utc8)
		if err != nil {
			t.Fatalf("parse time %q: %v", raw, err)
		}
		if _, ok := times[ts.UTC().Unix()]; !ok {
			t.Fatalf("missing expected timestamp for %s", raw)
		}
	}
}

func TestDynamicLongDeltaClamps(t *testing.T) {
	tests := []struct {
		name         string
		hvPercentile float64
		ivPercentile float64
		want         float64
	}{
		{name: "lower clamp", hvPercentile: 0, ivPercentile: 0, want: minLongDelta},
		{name: "formula", hvPercentile: 75, ivPercentile: 60, want: 0.7},
		{name: "upper clamp", hvPercentile: 100, ivPercentile: 100, want: maxLongDelta},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dynamicLongDelta(tc.hvPercentile, tc.ivPercentile)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("dynamicLongDelta(%v, %v) = %v, want %v", tc.hvPercentile, tc.ivPercentile, got, tc.want)
			}
		})
	}
}

func TestAmbushTakeProfitTargetsUseInitialLongSpend(t *testing.T) {
	state := activeState{ambushLongSpend: 7.5}
	tp1, tp2 := state.ambushTakeProfitTargets()
	if math.Abs(tp1-2.475) > 1e-9 {
		t.Fatalf("tp1 = %v, want 2.475", tp1)
	}
	if math.Abs(tp2-4.5) > 1e-9 {
		t.Fatalf("tp2 = %v, want 4.5", tp2)
	}
}

func TestSelectAmbushUsesSplitLongLegs(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 63)

	contracts := []backtest.OptionContract{
		{Symbol: "P-50000", Type: backtest.Put, StrikePrice: 50000, Expiration: expiry, Delta: -0.50, BidPrice: 5.0, AskPrice: 5.2, MarkPrice: 5.1},
		{Symbol: "P-48000", Type: backtest.Put, StrikePrice: 48000, Expiration: expiry, Delta: -0.39, BidPrice: 2.4, AskPrice: 2.6, MarkPrice: 2.5},
		{Symbol: "P-47000", Type: backtest.Put, StrikePrice: 47000, Expiration: expiry, Delta: -0.31, BidPrice: 2.2, AskPrice: 2.4, MarkPrice: 2.3},
		{Symbol: "P-46000", Type: backtest.Put, StrikePrice: 46000, Expiration: expiry, Delta: -0.24, BidPrice: 1.8, AskPrice: 2.0, MarkPrice: 1.9},
		{Symbol: "P-45000", Type: backtest.Put, StrikePrice: 45000, Expiration: expiry, Delta: -0.18, BidPrice: 1.4, AskPrice: 1.6, MarkPrice: 1.5},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	selection, ok := s.selectAmbush(backtest.NewOptionsChain(contracts, now), now, sideShort)
	if !ok {
		t.Fatalf("selectAmbush() did not find a valid ratio spread")
	}
	if selection.short.Symbol != "P-50000" {
		t.Fatalf("short = %s, want P-50000", selection.short.Symbol)
	}
	if selection.longLowerHalf.Symbol != "P-47000" {
		t.Fatalf("longLowerHalf = %s, want P-47000", selection.longLowerHalf.Symbol)
	}
	if selection.longUpperHalf.Symbol != "P-48000" {
		t.Fatalf("longUpperHalf = %s, want P-48000", selection.longUpperHalf.Symbol)
	}
	if math.Abs((selection.longLowerPrice+selection.longUpperPrice)-selection.shortPrice) > 1e-9 {
		t.Fatalf("combined long premium = %v, short premium = %v, want zero-cost split", selection.longLowerPrice+selection.longUpperPrice, selection.shortPrice)
	}
}

func TestSelectAmbushLongSideUsesSplitCallLegs(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 63)

	contracts := []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: expiry, Delta: 0.50, BidPrice: 5.0, AskPrice: 5.2, MarkPrice: 5.1},
		{Symbol: "C-52000", Type: backtest.Call, StrikePrice: 52000, Expiration: expiry, Delta: 0.39, BidPrice: 2.4, AskPrice: 2.6, MarkPrice: 2.5},
		{Symbol: "C-53000", Type: backtest.Call, StrikePrice: 53000, Expiration: expiry, Delta: 0.31, BidPrice: 2.2, AskPrice: 2.4, MarkPrice: 2.3},
		{Symbol: "C-54000", Type: backtest.Call, StrikePrice: 54000, Expiration: expiry, Delta: 0.24, BidPrice: 1.8, AskPrice: 2.0, MarkPrice: 1.9},
		{Symbol: "C-55000", Type: backtest.Call, StrikePrice: 55000, Expiration: expiry, Delta: 0.18, BidPrice: 1.4, AskPrice: 1.6, MarkPrice: 1.5},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	selection, ok := s.selectAmbush(backtest.NewOptionsChain(contracts, now), now, sideLong)
	if !ok {
		t.Fatalf("selectAmbush() did not find a valid long-side ratio spread")
	}
	if selection.short.Symbol != "C-50000" {
		t.Fatalf("short = %s, want C-50000", selection.short.Symbol)
	}
	if selection.longLowerHalf.Symbol != "C-53000" {
		t.Fatalf("longLowerHalf = %s, want C-53000", selection.longLowerHalf.Symbol)
	}
	if selection.longUpperHalf.Symbol != "C-52000" {
		t.Fatalf("longUpperHalf = %s, want C-52000", selection.longUpperHalf.Symbol)
	}
	if math.Abs((selection.longLowerPrice+selection.longUpperPrice)-selection.shortPrice) > 1e-9 {
		t.Fatalf("combined long premium = %v, short premium = %v, want zero-cost split", selection.longLowerPrice+selection.longUpperPrice, selection.shortPrice)
	}
}

func TestAmbushLegQuantitiesCanDifferBetweenShortAndLongWings(t *testing.T) {
	selection := &ambushSelection{
		shortPrice:     5.0,
		longLowerPrice: 2.1,
		longUpperPrice: 2.3,
	}

	shortQtyTotal := ambushPremiumBaseBTC / selection.shortPrice
	longQtyTotal := ambushPremiumBaseBTC / (selection.longLowerPrice + selection.longUpperPrice)
	if math.Abs(shortQtyTotal-longQtyTotal) < 1e-9 {
		t.Fatalf("shortQtyTotal = %v, longQtyTotal = %v, want different quantities when wing premium sum != short premium", shortQtyTotal, longQtyTotal)
	}
	if math.Abs(shortQtyTotal-1.0) > 1e-9 {
		t.Fatalf("shortQtyTotal = %v, want 1.0", shortQtyTotal)
	}
	if math.Abs(longQtyTotal-(5.0/4.4)) > 1e-9 {
		t.Fatalf("longQtyTotal = %v, want %v", longQtyTotal, 5.0/4.4)
	}
}

func TestSelectTrendLongSideUsesCallDebitSpread(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 35)

	contracts := []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: expiry, Delta: 0.60, BidPrice: 5.8, AskPrice: 6.2, MarkPrice: 6.0},
		{Symbol: "C-57000", Type: backtest.Call, StrikePrice: 57000, Expiration: expiry, Delta: 0.22, BidPrice: 4.0, AskPrice: 4.2, MarkPrice: 4.1},
		{Symbol: "C-58000", Type: backtest.Call, StrikePrice: 58000, Expiration: expiry, Delta: 0.20, BidPrice: 3.8, AskPrice: 4.2, MarkPrice: 4.0},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	selection, ok := s.selectTrend(backtest.NewOptionsChain(contracts, now), now, 75, 60, 2.0, sideLong)
	if !ok {
		t.Fatalf("selectTrend() did not find a valid long-side debit spread")
	}
	if selection.long.Symbol != "C-50000" {
		t.Fatalf("long = %s, want C-50000", selection.long.Symbol)
	}
	if selection.short.Symbol != "C-58000" {
		t.Fatalf("short = %s, want C-58000", selection.short.Symbol)
	}
	wantSpreadCost := selection.longPrice - selection.shortPrice
	if math.Abs(selection.spreadCost-wantSpreadCost) > 1e-9 {
		t.Fatalf("spreadCost = %v, want %v", selection.spreadCost, wantSpreadCost)
	}
	wantQty := 2.0 / selection.spreadCost
	if math.Abs(selection.qty-wantQty) > 1e-9 {
		t.Fatalf("qty = %v, want %v", selection.qty, wantQty)
	}
}

func TestSelectTrendRejectsLongLegOutsideDocumentedDeltaRange(t *testing.T) {
	now := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	expiry := now.AddDate(0, 0, 35)

	contracts := []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: expiry, Delta: 0.05, BidPrice: 5.8, AskPrice: 6.2, MarkPrice: 6.0},
		{Symbol: "C-58000", Type: backtest.Call, StrikePrice: 58000, Expiration: expiry, Delta: 0.20, BidPrice: 3.8, AskPrice: 4.2, MarkPrice: 4.0},
	}

	s := &strategy{}
	s.ApplyPricingDefaults()
	if _, ok := s.selectTrend(backtest.NewOptionsChain(contracts, now), now, 75, 60, 2.0, sideLong); ok {
		t.Fatalf("selectTrend() unexpectedly accepted a long leg with |delta| below 0.1")
	}
}

type scriptedDataFeed struct {
	timestamps []time.Time
	closes     []float64
}

func (f *scriptedDataFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume"}
}

func (f *scriptedDataFeed) Load(_ context.Context, _ backtest.DataRequest) (*backtest.DataSet, error) {
	ds := backtest.NewDataSet(len(f.timestamps))
	ts := append([]time.Time(nil), f.timestamps...)
	open := append([]float64(nil), f.closes...)
	high := make([]float64, len(f.closes))
	low := make([]float64, len(f.closes))
	volume := make([]float64, len(f.closes))
	for i, closePrice := range f.closes {
		high[i] = closePrice + 1
		low[i] = closePrice - 1
		volume[i] = 1
	}
	ds.SetTimestamps(ts)
	ds.AddColumn("open", open)
	ds.AddColumn("high", high)
	ds.AddColumn("low", low)
	ds.AddColumn("close", append([]float64(nil), f.closes...))
	ds.AddColumn("volume", volume)
	return ds, nil
}

type scriptedFactorFeed struct {
	timestamps []time.Time
	values     []float64
}

func (f *scriptedFactorFeed) Fields() []string { return []string{"close"} }

func (f *scriptedFactorFeed) Load(_ context.Context, _ backtest.FactorRequest) (*backtest.DataSet, error) {
	ds := backtest.NewDataSet(len(f.timestamps))
	ds.SetTimestamps(append([]time.Time(nil), f.timestamps...))
	ds.AddColumn("close", append([]float64(nil), f.values...))
	return ds, nil
}

type ambushExpiryChainProvider struct {
	tpTime time.Time
	expiry time.Time
}

func (p *ambushExpiryChainProvider) AvailableContracts(t time.Time) []backtest.OptionContract {
	shortBid := 5.0
	shortAsk := 5.2
	shortMark := 5.1
	upperBid := 2.4
	upperAsk := 2.6
	upperMark := 2.5
	lowerBid := 2.2
	lowerAsk := 2.4
	lowerMark := 2.3
	farBid := 1.8
	farAsk := 2.0
	farMark := 1.9
	farthestBid := 1.4
	farthestAsk := 1.6
	farthestMark := 1.5

	if !t.Before(p.tpTime) {
		shortBid = 5.3
		shortAsk = 5.5
		shortMark = 5.4
		upperBid = 3.9
		upperAsk = 4.1
		upperMark = 4.0
		lowerBid = 3.4
		lowerAsk = 3.6
		lowerMark = 3.5
		farBid = 2.8
		farAsk = 3.0
		farMark = 2.9
		farthestBid = 2.2
		farthestAsk = 2.4
		farthestMark = 2.3
	}

	return []backtest.OptionContract{
		{Symbol: "C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: p.expiry, Delta: 0.50, BidPrice: shortBid, AskPrice: shortAsk, MarkPrice: shortMark},
		{Symbol: "C-52000", Type: backtest.Call, StrikePrice: 52000, Expiration: p.expiry, Delta: 0.39, BidPrice: upperBid, AskPrice: upperAsk, MarkPrice: upperMark},
		{Symbol: "C-53000", Type: backtest.Call, StrikePrice: 53000, Expiration: p.expiry, Delta: 0.31, BidPrice: lowerBid, AskPrice: lowerAsk, MarkPrice: lowerMark},
		{Symbol: "C-54000", Type: backtest.Call, StrikePrice: 54000, Expiration: p.expiry, Delta: 0.24, BidPrice: farBid, AskPrice: farAsk, MarkPrice: farMark},
		{Symbol: "C-55000", Type: backtest.Call, StrikePrice: 55000, Expiration: p.expiry, Delta: 0.18, BidPrice: farthestBid, AskPrice: farthestAsk, MarkPrice: farthestMark},
	}
}

type ambushToTrendSameBarChainProvider struct {
	tpTime       time.Time
	ambushExpiry time.Time
	trendExpiry  time.Time
}

func (p *ambushToTrendSameBarChainProvider) AvailableContracts(t time.Time) []backtest.OptionContract {
	ambushShortBid := 5.0
	ambushShortAsk := 5.2
	ambushShortMark := 5.1
	ambushUpperBid := 2.4
	ambushUpperAsk := 2.6
	ambushUpperMark := 2.5
	ambushLowerBid := 2.2
	ambushLowerAsk := 2.4
	ambushLowerMark := 2.3
	ambushFarBid := 1.8
	ambushFarAsk := 2.0
	ambushFarMark := 1.9
	ambushFarthestBid := 1.4
	ambushFarthestAsk := 1.6
	ambushFarthestMark := 1.5

	if !t.Before(p.tpTime) {
		ambushShortBid = 4.8
		ambushShortAsk = 5.0
		ambushShortMark = 4.9
		ambushUpperBid = 4.8
		ambushUpperAsk = 5.0
		ambushUpperMark = 4.9
		ambushLowerBid = 4.4
		ambushLowerAsk = 4.6
		ambushLowerMark = 4.5
		ambushFarBid = 3.0
		ambushFarAsk = 3.2
		ambushFarMark = 3.1
		ambushFarthestBid = 2.6
		ambushFarthestAsk = 2.8
		ambushFarthestMark = 2.7
	}

	return []backtest.OptionContract{
		{Symbol: "AMB-C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: p.ambushExpiry, Delta: 0.50, BidPrice: ambushShortBid, AskPrice: ambushShortAsk, MarkPrice: ambushShortMark},
		{Symbol: "AMB-C-52000", Type: backtest.Call, StrikePrice: 52000, Expiration: p.ambushExpiry, Delta: 0.39, BidPrice: ambushUpperBid, AskPrice: ambushUpperAsk, MarkPrice: ambushUpperMark},
		{Symbol: "AMB-C-53000", Type: backtest.Call, StrikePrice: 53000, Expiration: p.ambushExpiry, Delta: 0.31, BidPrice: ambushLowerBid, AskPrice: ambushLowerAsk, MarkPrice: ambushLowerMark},
		{Symbol: "AMB-C-54000", Type: backtest.Call, StrikePrice: 54000, Expiration: p.ambushExpiry, Delta: 0.24, BidPrice: ambushFarBid, AskPrice: ambushFarAsk, MarkPrice: ambushFarMark},
		{Symbol: "AMB-C-55000", Type: backtest.Call, StrikePrice: 55000, Expiration: p.ambushExpiry, Delta: 0.18, BidPrice: ambushFarthestBid, AskPrice: ambushFarthestAsk, MarkPrice: ambushFarthestMark},
		{Symbol: "TRD-C-50000", Type: backtest.Call, StrikePrice: 50000, Expiration: p.trendExpiry, Delta: 0.55, BidPrice: 5.8, AskPrice: 6.2, MarkPrice: 6.0},
		{Symbol: "TRD-C-57000", Type: backtest.Call, StrikePrice: 57000, Expiration: p.trendExpiry, Delta: 0.22, BidPrice: 4.0, AskPrice: 4.2, MarkPrice: 4.1},
		{Symbol: "TRD-C-58000", Type: backtest.Call, StrikePrice: 58000, Expiration: p.trendExpiry, Delta: 0.20, BidPrice: 3.8, AskPrice: 4.2, MarkPrice: 4.0},
	}
}

func TestAmbushClosesRemainingTranchesBeforeExpiryAfterPartialTakeProfit(t *testing.T) {
	openTime := time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC)
	tpTime := openTime.Add(2 * time.Hour)
	expiry := openTime.AddDate(0, 0, 70)
	warmupStart := openTime.Add(-160 * 24 * time.Hour)
	endTime := expiry.Add(48 * time.Hour)

	timestamps := make([]time.Time, 0, int(endTime.Sub(warmupStart)/(2*time.Hour))+1)
	closes := make([]float64, 0, cap(timestamps))
	for ts := warmupStart; !ts.After(endTime); ts = ts.Add(2 * time.Hour) {
		timestamps = append(timestamps, ts)
		closePrice := 100.0
		if !ts.Before(tpTime) {
			closePrice = 150.0
		}
		closes = append(closes, closePrice)
	}

	factorValues := make([]float64, len(timestamps))
	for i := range factorValues {
		factorValues[i] = 50
	}

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 1})
	engine.RegisterDataFeed("test", &scriptedDataFeed{timestamps: timestamps, closes: closes})
	engine.RegisterFactorFeed("dvol", &scriptedFactorFeed{timestamps: timestamps, values: factorValues})
	engine.SetOptionsChainProvider(&ambushExpiryChainProvider{tpTime: tpTime, expiry: expiry})

	s := &strategy{
		longEntryTimes:  map[int64]struct{}{openTime.Unix(): {}},
		shortEntryTimes: map[int64]struct{}{},
	}

	result, err := engine.Run(context.Background(), "test", "BTC", "2h", openTime, endTime, s, nil)
	if err != nil {
		t.Fatalf("engine.Run() error = %v", err)
	}
	if result.SpreadSummary == nil {
		t.Fatal("result.SpreadSummary = nil, want summary")
	}
	if result.SpreadSummary.OpenSpreads != 0 {
		t.Fatalf("OpenSpreads = %d, want 0", result.SpreadSummary.OpenSpreads)
	}
	if len(result.SpreadPositions) != 3 {
		t.Fatalf("len(SpreadPositions) = %d, want 3", len(result.SpreadPositions))
	}

	closedAtTP := 0
	closedNearExpiry := 0
	for _, spread := range result.SpreadPositions {
		if spread.Status != "closed" {
			t.Fatalf("spread %d status = %q, want closed", spread.ID, spread.Status)
		}
		if spread.CloseTime == nil {
			t.Fatalf("spread %d CloseTime = nil, want close timestamp", spread.ID)
		}
		if spread.CloseTime.Equal(tpTime) {
			closedAtTP++
			continue
		}
		if !spread.CloseTime.Before(expiry.Add(-24 * time.Hour)) {
			closedNearExpiry++
		}
	}
	if closedAtTP != 1 {
		t.Fatalf("closedAtTP = %d, want 1", closedAtTP)
	}
	if closedNearExpiry != 2 {
		t.Fatalf("closedNearExpiry = %d, want 2", closedNearExpiry)
	}
	if len(result.SpreadGroups) != 1 {
		t.Fatalf("len(SpreadGroups) = %d, want 1", len(result.SpreadGroups))
	}
	if result.SpreadGroups[0].Status != "closed" {
		t.Fatalf("group status = %q, want closed", result.SpreadGroups[0].Status)
	}
}

func TestAmbushExecutesPartialAndFullTakeProfitOnSameBar(t *testing.T) {
	openTime := time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC)
	tpTime := openTime.Add(2 * time.Hour)
	ambushExpiry := openTime.AddDate(0, 0, 70)
	trendExpiry := openTime.AddDate(0, 0, 35)
	warmupStart := openTime.Add(-160 * 24 * time.Hour)
	endTime := tpTime.Add(2 * time.Hour)

	timestamps := make([]time.Time, 0, int(endTime.Sub(warmupStart)/(2*time.Hour))+1)
	closes := make([]float64, 0, cap(timestamps))
	for ts := warmupStart; !ts.After(endTime); ts = ts.Add(2 * time.Hour) {
		timestamps = append(timestamps, ts)
		closePrice := 100.0
		if !ts.Before(tpTime) {
			closePrice = 150.0
		}
		closes = append(closes, closePrice)
	}

	factorValues := make([]float64, len(timestamps))
	for i := range factorValues {
		factorValues[i] = 50
	}

	engine := backtest.NewEngine(backtest.Config{InitialCapital: 1})
	engine.RegisterDataFeed("test", &scriptedDataFeed{timestamps: timestamps, closes: closes})
	engine.RegisterFactorFeed("dvol", &scriptedFactorFeed{timestamps: timestamps, values: factorValues})
	engine.SetOptionsChainProvider(&ambushToTrendSameBarChainProvider{tpTime: tpTime, ambushExpiry: ambushExpiry, trendExpiry: trendExpiry})

	s := &strategy{
		longEntryTimes:  map[int64]struct{}{openTime.Unix(): {}},
		shortEntryTimes: map[int64]struct{}{},
	}

	result, err := engine.Run(context.Background(), "test", "BTC", "2h", openTime, endTime, s, nil)
	if err != nil {
		t.Fatalf("engine.Run() error = %v", err)
	}
	if len(result.SpreadPositions) != 4 {
		t.Fatalf("len(SpreadPositions) = %d, want 4", len(result.SpreadPositions))
	}

	partialClosed := 0
	convertedClosed := 0
	openTrend := 0
	for _, spread := range result.SpreadPositions {
		switch {
		case strings.Contains(spread.Tag, "一期比例价差"):
			if spread.CloseTime == nil || !spread.CloseTime.Equal(tpTime) {
				t.Fatalf("ambush spread %d close time = %v, want %v", spread.ID, spread.CloseTime, tpTime)
			}
			switch {
			case strings.Contains(spread.CloseNote, "一期止盈33%减仓"):
				partialClosed++
			case strings.Contains(spread.CloseNote, "一期止盈60%转二期"):
				convertedClosed++
			default:
				t.Fatalf("ambush spread %d close note = %q, want same-bar take-profit note", spread.ID, spread.CloseNote)
			}
		case strings.Contains(spread.Tag, "二期借记价差"):
			if spread.Status != "open" {
				t.Fatalf("trend spread %d status = %q, want open", spread.ID, spread.Status)
			}
			if !spread.OpenTime.Equal(tpTime) {
				t.Fatalf("trend spread %d open time = %v, want %v", spread.ID, spread.OpenTime, tpTime)
			}
			openTrend++
		default:
			t.Fatalf("unexpected spread tag %q", spread.Tag)
		}
	}

	if partialClosed != 1 {
		t.Fatalf("partialClosed = %d, want 1", partialClosed)
	}
	if convertedClosed != 2 {
		t.Fatalf("convertedClosed = %d, want 2", convertedClosed)
	}
	if openTrend != 1 {
		t.Fatalf("openTrend = %d, want 1", openTrend)
	}
}

package lvolscalper

import (
	"math"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestInitAppliesDefaultsAndResetsRuntimeState(t *testing.T) {
	s := &strategy{
		activeSpreadID:  7,
		entryPremium:    1.5,
		entryATMIV:      0.42,
		entryDayKey:     20260101,
		lastEntryDayKey: 20260101,
	}

	ctx := backtest.NewSetupContext("crypto", "BTCUSDT", "1m")
	if err := s.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if s.targetExpiryDays != defaultTargetExpiryDays {
		t.Fatalf("targetExpiryDays = %d, want %d", s.targetExpiryDays, defaultTargetExpiryDays)
	}
	if s.marginExit != defaultMarginExit {
		t.Fatalf("marginExit = %.2f, want %.2f", s.marginExit, defaultMarginExit)
	}
	if s.ivSpikeMultiple != defaultIVSpikeMultiple {
		t.Fatalf("ivSpikeMultiple = %.2f, want %.2f", s.ivSpikeMultiple, defaultIVSpikeMultiple)
	}
	if s.activeSpreadID != 0 || s.entryPremium != 0 || s.entryATMIV != 0 || s.entryDayKey != 0 || s.lastEntryDayKey != 0 {
		t.Fatalf("Init() should reset runtime state, got spread=%d premium=%.4f atmIV=%.4f entryDay=%d lastEntry=%d", s.activeSpreadID, s.entryPremium, s.entryATMIV, s.entryDayKey, s.lastEntryDayKey)
	}
}

func TestSelectContractsUsesBTCCaps(t *testing.T) {
	now := time.Date(2026, 1, 3, 2, 15, 0, 0, time.UTC)
	s := &strategy{}
	s.applyDefaults()

	chain := backtest.NewOptionsChain([]backtest.OptionContract{
		{
			Symbol:          "BTC-17JAN26-63000-C",
			Type:            backtest.Call,
			Expiration:      now.Add(14 * 24 * time.Hour),
			StrikePrice:     63000,
			BidPrice:        0.25,
			AskPrice:        0.26,
			MarkPrice:       0.255,
			UnderlyingPrice: 60000,
		},
		{
			Symbol:          "BTC-17JAN26-57000-P",
			Type:            backtest.Put,
			Expiration:      now.Add(14 * 24 * time.Hour),
			StrikePrice:     57000,
			BidPrice:        0.25,
			AskPrice:        0.26,
			MarkPrice:       0.255,
			UnderlyingPrice: 60000,
		},
	}, now)

	selection, ok := s.selectContracts(chain, 60000, 0.02, now, 100)
	if !ok {
		t.Fatal("expected contract selection to succeed")
	}

	const wantQty = 30.0
	if math.Abs(selection.qty-wantQty) > 1e-9 {
		t.Fatalf("selection.qty = %.6f, want %.6f", selection.qty, wantQty)
	}
	if selection.call.Symbol != "BTC-17JAN26-63000-C" {
		t.Fatalf("selected call = %s", selection.call.Symbol)
	}
	if selection.put.Symbol != "BTC-17JAN26-57000-P" {
		t.Fatalf("selected put = %s", selection.put.Symbol)
	}
}

func TestShouldExitForIVSpikeUsesEntryAndRVBaseline(t *testing.T) {
	s := &strategy{entryATMIV: 0.40, ivSpikeMultiple: 1.35}

	if s.shouldExitForIVSpike(0.53, 0.30) {
		t.Fatal("0.53 IV should stay below the spike threshold")
	}
	if !s.shouldExitForIVSpike(0.54, 0.30) {
		t.Fatal("0.54 IV should hit the entry-based spike threshold")
	}

	rvDriven := &strategy{entryATMIV: 0.30, ivSpikeMultiple: 1.35}
	if !rvDriven.shouldExitForIVSpike(0.52, 0.38) {
		t.Fatal("RV floor should be able to raise the spike threshold trigger")
	}
}

func TestSpreadCloseOrderPrioritizesITMThenNearATM(t *testing.T) {
	spot := 60500.0
	spread := &backtest.SpreadPosition{Legs: []backtest.SpreadLeg{
		{Contract: backtest.OptionContract{Symbol: "itm-call", Type: backtest.Call, StrikePrice: 60000}},
		{Contract: backtest.OptionContract{Symbol: "far-call", Type: backtest.Call, StrikePrice: 65000}},
		{Contract: backtest.OptionContract{Symbol: "near-put", Type: backtest.Put, StrikePrice: 59000}},
	}}
	contractMap := optContractMap([]backtest.OptionContract{
		{Symbol: "itm-call", Type: backtest.Call, StrikePrice: 60000, UnderlyingPrice: spot},
		{Symbol: "far-call", Type: backtest.Call, StrikePrice: 65000, UnderlyingPrice: spot},
		{Symbol: "near-put", Type: backtest.Put, StrikePrice: 59000, UnderlyingPrice: spot},
	})

	order := spreadCloseOrder(spread, contractMap, spot)
	if len(order) != 3 {
		t.Fatalf("close order len = %d, want 3", len(order))
	}
	if order[0] != 0 || order[1] != 2 || order[2] != 1 {
		t.Fatalf("close order = %v, want [0 2 1]", order)
	}
}

func TestComputeSessionSigmaSeriesUsesPreviousDayEMA(t *testing.T) {
	timestamps := []time.Time{
		time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC),
	}
	returns := []float64{math.NaN(), 0.0100, -0.0050, math.NaN(), 0.0080, -0.0060}

	series := computeSessionSigmaSeries(timestamps, returns, defaultSessionDays)
	expectedDayOneSigma := stddev([]float64{0.0100, -0.0050}) * math.Sqrt(2)

	if math.IsNaN(series[3]) {
		t.Fatal("expected day-two bars to inherit the previous EMA sigma")
	}
	if math.Abs(series[3]-expectedDayOneSigma) > 1e-9 {
		t.Fatalf("day-two sigma = %.10f, want %.10f", series[3], expectedDayOneSigma)
	}
	if math.Abs(series[4]-expectedDayOneSigma) > 1e-9 || math.Abs(series[5]-expectedDayOneSigma) > 1e-9 {
		t.Fatalf("expected all day-two bars to use prior-day EMA, got %.10f and %.10f", series[4], series[5])
	}
}

func optContractMap(contracts []backtest.OptionContract) map[string]backtest.OptionContract {
	result := make(map[string]backtest.OptionContract, len(contracts))
	for _, contract := range contracts {
		result[contract.Symbol] = contract
	}
	return result
}

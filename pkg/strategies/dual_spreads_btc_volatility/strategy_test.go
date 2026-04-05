package dualspreadsvol

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/optutil"
)

func TestSignalTypeFromIndicator(t *testing.T) {
	tests := []struct {
		name   string
		value  float64
		want   signalType
		wantOK bool
	}{
		{name: "init signal", value: 1, want: signalInit, wantOK: true},
		{name: "add signal", value: 2, want: signalAdd, wantOK: true},
		{name: "nan ignored", value: math.NaN(), want: signalNone, wantOK: false},
		{name: "zero ignored", value: 0, want: signalNone, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := signalTypeFromIndicator(tt.value)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("signalTypeFromIndicator(%v) = (%v, %v), want (%v, %v)", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestLoadSignalsDeduplicatesWithinUTC8HalfDayBucket(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "signals.csv")
	content := strings.Join([]string{
		"交易 #,类型,日期和时间,信号",
		"1,多头进场,2022-07-18 12:00,Long_Init",
		"2,多头进场,2022-07-18 16:00,Long_Add_1",
		"3,多头进场,2022-07-19 00:00,Long_Add_2",
	}, "\n")
	if err := os.WriteFile(tempFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp signal file: %v", err)
	}

	signals, err := loadSignals(tempFile)
	if err != nil {
		t.Fatalf("loadSignals returned error: %v", err)
	}
	if len(signals) != 3 {
		t.Fatalf("len(signals) = %d, want 3", len(signals))
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	firstTime, err := time.ParseInLocation("2006-01-02 15:04", "2022-07-18 12:00", utc8)
	if err != nil {
		t.Fatalf("parse first time: %v", err)
	}
	secondTime, err := time.ParseInLocation("2006-01-02 15:04", "2022-07-19 00:00", utc8)
	if err != nil {
		t.Fatalf("parse second time: %v", err)
	}
	thirdTime, err := time.ParseInLocation("2006-01-02 15:04", "2022-07-18 16:00", utc8)
	if err != nil {
		t.Fatalf("parse third time: %v", err)
	}

	if !signals[0].time.Equal(firstTime.UTC()) || signals[0].sigType != signalInit {
		t.Fatalf("signals[0] = %#v, want init at %s", signals[0], firstTime.UTC())
	}
	if !signals[1].time.Equal(thirdTime.UTC()) || signals[1].sigType != signalAdd {
		t.Fatalf("signals[1] = %#v, want add at %s", signals[1], thirdTime.UTC())
	}
	if !signals[2].time.Equal(secondTime.UTC()) || signals[2].sigType != signalAdd {
		t.Fatalf("signals[2] = %#v, want add at %s", signals[2], secondTime.UTC())
	}
}

func TestBuildSignalColumnAssignsSignalsToPrimaryBars(t *testing.T) {
	primaryTimestamps := []time.Time{
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	}
	events := []signalEvent{
		{time: time.Date(2024, time.January, 1, 2, 0, 0, 0, time.UTC), sigType: signalAdd},
		{time: time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC), sigType: signalInit},
		{time: time.Date(2024, time.January, 1, 13, 0, 0, 0, time.UTC), sigType: signalAdd},
	}

	got := buildSignalColumn(primaryTimestamps, events)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0] != 1 {
		t.Fatalf("got[0] = %v, want 1", got[0])
	}
	if got[1] != 2 {
		t.Fatalf("got[1] = %v, want 2", got[1])
	}
	if got[2] != 0 {
		t.Fatalf("got[2] = %v, want 0", got[2])
	}
}

func TestBuildTriggeredAlignedSignalColumnFiresOncePer12HBar(t *testing.T) {
	alignMap := []int{0, 0, 0, 1, 1, 2, 2}
	values := []float64{1, 0, 2}

	got := buildTriggeredAlignedSignalColumn(alignMap, values, len(alignMap))
	want := []float64{1, 0, 0, 0, 0, 2, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestInitEntryAllowedByMetrics(t *testing.T) {
	tests := []struct {
		name         string
		hv           float64
		dvol         float64
		hvThreshold  float64
		ivThreshold  float64
		dvolBarCount int
		want         bool
	}{
		{
			name:         "allow when hv is below hv threshold",
			hv:           55,
			dvol:         70,
			hvThreshold:  60,
			ivThreshold:  65,
			dvolBarCount: ivPercentileLookback,
			want:         true,
		},
		{
			name:         "allow when iv is below iv threshold",
			hv:           75,
			dvol:         64,
			hvThreshold:  60,
			ivThreshold:  65,
			dvolBarCount: ivPercentileLookback,
			want:         true,
		},
		{
			name:         "allow fallback ratio before 200 dvol bars",
			hv:           1.2,
			dvol:         60,
			hvThreshold:  math.NaN(),
			ivThreshold:  math.NaN(),
			dvolBarCount: ivPercentileLookback - 1,
			want:         true,
		},
		{
			name:         "reject when ratio fallback exceeds max",
			hv:           0.8,
			dvol:         60,
			hvThreshold:  math.NaN(),
			ivThreshold:  math.NaN(),
			dvolBarCount: ivPercentileLookback - 1,
			want:         false,
		},
		{
			name:         "reject when thresholds and fallback both fail",
			hv:           75,
			dvol:         80,
			hvThreshold:  60,
			ivThreshold:  65,
			dvolBarCount: ivPercentileLookback,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initEntryAllowedByMetrics(tt.hv, tt.dvol, tt.hvThreshold, tt.ivThreshold, tt.dvolBarCount)
			if got != tt.want {
				t.Fatalf("initEntryAllowedByMetrics() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitAllowsLowerPrimaryIntervalForScanBars(t *testing.T) {
	s := &strategy{}
	ctx := backtest.NewSetupContext("crypto", "BTC", "1h")
	err := s.Init(ctx)
	if err != nil {
		t.Fatalf("Init() error = %v, want nil", err)
	}
}

func TestAlignSeriesValuesUsesLatestAvailable12HValue(t *testing.T) {
	targetTimes := []time.Time{
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 6, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
	}
	sourceTimes := []time.Time{
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
	}
	values := []float64{10, 20}

	got, err := alignSeriesValues(targetTimes, sourceTimes, values)
	if err != nil {
		t.Fatalf("alignSeriesValues() error = %v", err)
	}
	want := []float64{10, 10, 20}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestSelectSpreadFallsForwardToNextExpiry(t *testing.T) {
	now := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	var logs strings.Builder

	s := &strategy{
		PricingMixin: optutil.PricingMixin{EntryPriceMode: backtest.OptionPriceMarkClose},
		logf: func(format string, args ...any) {
			logs.WriteString(fmt.Sprintf(strings.TrimSpace(format), args...))
			logs.WriteString("\n")
		},
	}

	chain := backtest.NewOptionsChain([]backtest.OptionContract{
		{
			Symbol:      "BTC-20240209-50000-C",
			Type:        backtest.Call,
			Expiration:  now.Add(39 * 24 * time.Hour),
			Delta:       0.50,
			MarkPrice:   0,
			BidPrice:    0,
			AskPrice:    0,
			StrikePrice: 50000,
		},
		{
			Symbol:      "BTC-20240209-60000-C",
			Type:        backtest.Call,
			Expiration:  now.Add(39 * 24 * time.Hour),
			Delta:       0.10,
			MarkPrice:   0,
			BidPrice:    0,
			AskPrice:    0,
			StrikePrice: 60000,
		},
		{
			Symbol:      "BTC-20240126-50000-C",
			Type:        backtest.Call,
			Expiration:  now.Add(25 * 24 * time.Hour),
			Delta:       0.49,
			MarkPrice:   5,
			BidPrice:    4.8,
			AskPrice:    5.2,
			StrikePrice: 50000,
		},
		{
			Symbol:      "BTC-20240126-60000-C",
			Type:        backtest.Call,
			Expiration:  now.Add(25 * 24 * time.Hour),
			Delta:       0.11,
			MarkPrice:   2,
			BidPrice:    1.9,
			AskPrice:    2.1,
			StrikePrice: 60000,
		},
	}, now)

	selection, ok := s.selectSpread(now, chain, amountBase, defaultVolPercentile, defaultVolPercentile, "entry")
	if !ok {
		t.Fatal("expected selection to succeed on later expiry")
	}

	if got, want := selection.expiry, now.Add(25*24*time.Hour); !got.Equal(want) {
		t.Fatalf("selected expiry = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if selection.long.Symbol != "BTC-20240126-50000-C" {
		t.Fatalf("selected long = %s, want BTC-20240126-50000-C", selection.long.Symbol)
	}
	if selection.short.Symbol != "BTC-20240126-60000-C" {
		t.Fatalf("selected short = %s, want BTC-20240126-60000-C", selection.short.Symbol)
	}

	output := logs.String()
	if !strings.Contains(output, "try expiry 2024-02-09") {
		t.Fatalf("expected logs to mention first expiry attempt, got:\n%s", output)
	}
	if !strings.Contains(output, "skip long candidate #1 BTC-20240209-50000-C") {
		t.Fatalf("expected logs to mention skipped long candidate, got:\n%s", output)
	}
	if !strings.Contains(output, "skip expiry 2024-02-09, reason=no valid long contract near delta 0.56") {
		t.Fatalf("expected logs to mention skipped expiry reason, got:\n%s", output)
	}
	if !strings.Contains(output, "selected expiry 2024-01-26") {
		t.Fatalf("expected logs to mention selected later expiry, got:\n%s", output)
	}
}

package dualspreadsvol

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
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

func TestProcessedSignalTimesDeduplicateByTimestamp(t *testing.T) {
	s := &strategy{}
	const signalTime int64 = 1711756800

	if s.signalProcessed(signalTime) {
		t.Fatal("signal should not be marked processed before consumption")
	}

	s.markSignalProcessed(signalTime)

	if !s.signalProcessed(signalTime) {
		t.Fatal("signal should be marked processed after consumption")
	}

	if s.signalProcessed(signalTime + 3600) {
		t.Fatal("different signal timestamp should not be marked processed")
	}
}

func TestSelectSpreadFallsForwardToNextExpiry(t *testing.T) {
	now := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	var logs strings.Builder

	s := &strategy{
		EntryPriceMode: backtest.OptionPriceMarkClose,
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
			Delta:       0.33,
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
			Symbol:      "BTC-20240126-52000-C",
			Type:        backtest.Call,
			Expiration:  now.Add(25 * 24 * time.Hour),
			Delta:       0.32,
			MarkPrice:   5,
			BidPrice:    4.8,
			AskPrice:    5.2,
			StrikePrice: 52000,
		},
		{
			Symbol:      "BTC-20240126-58000-C",
			Type:        backtest.Call,
			Expiration:  now.Add(25 * 24 * time.Hour),
			Delta:       0.11,
			MarkPrice:   2,
			BidPrice:    1.9,
			AskPrice:    2.1,
			StrikePrice: 58000,
		},
	}, now)

	selection, ok := s.selectSpread(now, chain, amountBase, "entry")
	if !ok {
		t.Fatal("expected selection to succeed on later expiry")
	}

	if got, want := selection.expiry, now.Add(25*24*time.Hour); !got.Equal(want) {
		t.Fatalf("selected expiry = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if selection.long.Symbol != "BTC-20240126-52000-C" {
		t.Fatalf("selected long = %s, want BTC-20240126-52000-C", selection.long.Symbol)
	}
	if selection.short.Symbol != "BTC-20240126-58000-C" {
		t.Fatalf("selected short = %s, want BTC-20240126-58000-C", selection.short.Symbol)
	}

	output := logs.String()
	if !strings.Contains(output, "try expiry 2024-02-09") {
		t.Fatalf("expected logs to mention first expiry attempt, got:\n%s", output)
	}
	if !strings.Contains(output, "skip long candidate #1 BTC-20240209-50000-C") {
		t.Fatalf("expected logs to mention skipped long candidate, got:\n%s", output)
	}
	if !strings.Contains(output, "skip expiry 2024-02-09, reason=no valid long contract near delta 0.33") {
		t.Fatalf("expected logs to mention skipped expiry reason, got:\n%s", output)
	}
	if !strings.Contains(output, "selected expiry 2024-01-26") {
		t.Fatalf("expected logs to mention selected later expiry, got:\n%s", output)
	}
}

package service

import (
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestMacroSeriesFactorBarsBuildsSortedOHLCRows(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	points := []dto.MacroSeriesPoint{
		{Factor: "close", Timestamp: start.Add(time.Hour), Value: 12},
		{Factor: "open", Timestamp: start, Value: 10},
		{Factor: "high", Timestamp: start, Value: 13},
		{Factor: "low", Timestamp: start, Value: 9},
		{Factor: "close", Timestamp: start, Value: 11},
		{Factor: "open", Timestamp: start.Add(time.Hour), Value: 11},
		{Factor: "high", Timestamp: start.Add(time.Hour), Value: 14},
		{Factor: "low", Timestamp: start.Add(time.Hour), Value: 10},
	}

	rows := macroSeriesFactorBars("btc", points)
	if len(rows) != 2 {
		t.Fatalf("len(rows)=%d want 2", len(rows))
	}
	if !rows[0].Timestamp.Equal(start) || rows[0].Symbol != "BTC" {
		t.Fatalf("unexpected first row identity: %+v", rows[0])
	}
	if rows[0].Open != 10 || rows[0].High != 13 || rows[0].Low != 9 || rows[0].Close != 11 {
		t.Fatalf("unexpected first row OHLC: %+v", rows[0])
	}
	if !rows[1].Timestamp.Equal(start.Add(time.Hour)) || rows[1].Open != 11 || rows[1].High != 14 || rows[1].Low != 10 || rows[1].Close != 12 {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
}

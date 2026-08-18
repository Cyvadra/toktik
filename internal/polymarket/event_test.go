package polymarket

import (
	"testing"
	"time"
)

func TestCompareEventKeysUsesSelectedClockAndStableTieBreak(t *testing.T) {
	base := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)
	left := EventKey{ExchangeTime: base.Add(2 * time.Millisecond), ReceivedTime: base.Add(3 * time.Millisecond), SourceFile: "hour.parquet", SourceRow: 4}
	right := EventKey{ExchangeTime: base.Add(time.Millisecond), ReceivedTime: base.Add(4 * time.Millisecond), SourceFile: "hour.parquet", SourceRow: 3}

	if got, err := CompareEventKeys(left, right, ReplayClockReceived); err != nil || got >= 0 {
		t.Fatalf("received comparison = %d, %v; want left first", got, err)
	}
	if got, err := CompareEventKeys(left, right, ReplayClockExchange); err != nil || got <= 0 {
		t.Fatalf("exchange comparison = %d, %v; want right first", got, err)
	}

	tied := right
	tied.SourceRow = 5
	if got, err := CompareEventKeys(right, tied, ReplayClockReceived); err != nil || got >= 0 {
		t.Fatalf("source row tie-break = %d, %v; want lower row first", got, err)
	}
}

func TestEventIDIsStableAndSourceScoped(t *testing.T) {
	key := EventKey{SourceFile: "/mnt/raw/hour.parquet", SourceRow: 42}
	if got, want := key.EventID(), "4abec157d1b706d965e0ec9062954fd0"; got != want {
		t.Fatalf("event ID = %q, want %q", got, want)
	}
	other := key
	other.SourceRow++
	if key.EventID() == other.EventID() {
		t.Fatal("event IDs must differ by source row")
	}
}

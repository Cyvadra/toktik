package service

import (
	"testing"
	"time"
)

func TestNextCursorTimeAdvancesSecond(t *testing.T) {
	cursor := time.Date(2026, 5, 12, 9, 30, 0, 0, time.FixedZone("offset", 8*60*60))
	got := nextCursorTime(cursor)
	want := cursor.UTC().Add(time.Second)
	if !got.Equal(want) {
		t.Fatalf("nextCursorTime() = %s, want %s", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("expected UTC cursor time, got %s", got.Location())
	}
}

func TestSampleIVSmileTimestampsUsesRequestFromAsSevenDayAnchor(t *testing.T) {
	anchor := time.Date(2026, time.January, 3, 12, 0, 0, 0, time.UTC)
	timestamps := []time.Time{
		anchor.Add(24 * time.Hour),
		anchor.Add(6*24*time.Hour + time.Hour),
		anchor.Add(7*24*time.Hour + time.Hour),
		anchor.Add(13*24*time.Hour + time.Hour),
		anchor.Add(14*24*time.Hour + time.Hour),
	}
	got := sampleIVSmileTimestamps(timestamps, anchor, "7d")
	want := []time.Time{timestamps[1], timestamps[3], timestamps[4]}
	if len(got) != len(want) {
		t.Fatalf("sample count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("sample[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestIVSmileCursorRoundTrip(t *testing.T) {
	want := ivSmileCursor{Version: 1, Interval: "7d", Anchor: "2026-01-03T12:00:00Z", Offset: 4}
	got, err := decodeIVSmileCursor(encodeIVSmileCursor(want))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cursor = %#v, want %#v", got, want)
	}
}

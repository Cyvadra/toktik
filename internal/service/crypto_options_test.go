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

package backtest

import (
	"testing"
	"time"
)

func TestDataSetSlice(t *testing.T) {
	ds := NewDataSet(10)
	ts := make([]time.Time, 10)
	cl := make([]float64, 10)
	vol := make([]float64, 10)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		ts[i] = base.Add(time.Duration(i) * time.Hour)
		cl[i] = float64(100 + i)
		vol[i] = float64(1000 + i*100)
	}
	ds.SetTimestamps(ts)
	if err := ds.AddColumn("close", cl); err != nil {
		t.Fatalf("AddColumn close: %v", err)
	}
	if err := ds.AddColumn("volume", vol); err != nil {
		t.Fatalf("AddColumn volume: %v", err)
	}

	sliced, err := ds.Slice(2, 5)
	if err != nil {
		t.Fatalf("Slice(2,5) unexpected error: %v", err)
	}

	if sliced.Len != 3 {
		t.Fatalf("expected Len=3, got %d", sliced.Len)
	}
	if len(sliced.Timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(sliced.Timestamps))
	}
	if !sliced.Timestamps[0].Equal(ts[2]) {
		t.Errorf("first timestamp mismatch")
	}
	if !sliced.Timestamps[2].Equal(ts[4]) {
		t.Errorf("last timestamp mismatch")
	}

	clSlice := sliced.Column("close")
	if len(clSlice) != 3 {
		t.Fatalf("expected 3 close values, got %d", len(clSlice))
	}
	if clSlice[0] != 102 || clSlice[2] != 104 {
		t.Errorf("close values mismatch: got %v", clSlice)
	}

	volSlice := sliced.Column("volume")
	if volSlice[0] != 1200 {
		t.Errorf("expected volume[0]=1200, got %v", volSlice[0])
	}

	// Verify deep copy
	clSlice[0] = 999
	if cl[2] == 999 {
		t.Error("Slice should return a copy")
	}
}

func TestDataSetAddColumnLengthMismatch(t *testing.T) {
	ds := NewDataSet(5)
	ts := make([]time.Time, 5)
	for i := range ts {
		ts[i] = time.Now().Add(time.Duration(i) * time.Hour)
	}
	ds.SetTimestamps(ts)

	// Length mismatch should return an error, not panic.
	err := ds.AddColumn("bad", make([]float64, 3))
	if err == nil {
		t.Fatal("expected error for length mismatch, got nil")
	}
}

func TestDataSetSliceErrors(t *testing.T) {
	ds := NewDataSet(5)
	ts := make([]time.Time, 5)
	for i := range ts {
		ts[i] = time.Now().Add(time.Duration(i) * time.Hour)
	}
	ds.SetTimestamps(ts)

	cases := []struct {
		name       string
		start, end int
	}{
		{"negative start", -1, 3},
		{"end > len", 0, 10},
		{"start >= end", 3, 3},
		{"start > end", 4, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ds.Slice(tc.start, tc.end)
			if err == nil {
				t.Errorf("Slice(%d,%d): expected error, got nil", tc.start, tc.end)
			}
		})
	}
}

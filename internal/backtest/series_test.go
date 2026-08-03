package backtest

import (
	"reflect"
	"testing"
	"time"
)

func TestAlignSeries_CloseConfirmedForHigherTimeframe(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	primaryTS := make([]time.Time, 6)
	for i := range primaryTS {
		primaryTS[i] = base.Add(time.Duration(i) * time.Hour)
	}
	secondaryTS := []time.Time{
		base,
		base.Add(3 * time.Hour),
		base.Add(6 * time.Hour),
	}

	primary := &DataSet{Timestamps: primaryTS, Len: len(primaryTS)}
	secondary := &DataSet{Timestamps: secondaryTS, Len: len(secondaryTS)}

	got, err := alignSeries(primary, secondary, "1h", "3h")
	if err != nil {
		t.Fatalf("alignSeries returned error: %v", err)
	}
	want := []int{-1, -1, 0, 0, 0, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alignSeries close-confirmed mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestAlignSeries_RejectsUnknownIntervals(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	primaryTS := []time.Time{base, base.Add(time.Hour), base.Add(2 * time.Hour)}
	secondaryTS := []time.Time{base, base.Add(2 * time.Hour)}

	primary := &DataSet{Timestamps: primaryTS, Len: len(primaryTS)}
	secondary := &DataSet{Timestamps: secondaryTS, Len: len(secondaryTS)}

	if _, err := alignSeries(primary, secondary, "unknown", "unknown"); err == nil {
		t.Fatal("expected alignSeries to reject unknown intervals")
	}
}

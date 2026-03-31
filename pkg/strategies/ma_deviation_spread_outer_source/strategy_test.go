package madeviationspread

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadEntrySignalTimesParsesCSVEntriesInUTC8(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "signals.csv")
	content := "交易 #,类型,日期和时间,信号\n" +
		"1,空头出场,2024/3/14 16:00,空头止损\n" +
		"1,空头进场,2024/3/14 14:00,顶背离做空\n" +
		"2,空头进场,2024/7/30 2:00,顶背离做空\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entryTimes, err := loadEntrySignalTimes(path)
	if err != nil {
		t.Fatalf("loadEntrySignalTimes() error = %v", err)
	}

	if len(entryTimes) != 2 {
		t.Fatalf("len(entryTimes) = %d, want 2", len(entryTimes))
	}

	firstUTC := time.Date(2024, time.March, 14, 6, 0, 0, 0, time.UTC).Unix()
	if _, ok := entryTimes[firstUTC]; !ok {
		t.Fatalf("expected entry time %v to be present", time.Unix(firstUTC, 0).UTC())
	}

	secondUTC := time.Date(2024, time.July, 29, 18, 0, 0, 0, time.UTC).Unix()
	if _, ok := entryTimes[secondUTC]; !ok {
		t.Fatalf("expected entry time %v to be present", time.Unix(secondUTC, 0).UTC())
	}

	exitUTC := time.Date(2024, time.March, 14, 8, 0, 0, 0, time.UTC).Unix()
	if _, ok := entryTimes[exitUTC]; ok {
		t.Fatalf("did not expect exit time %v to be present", time.Unix(exitUTC, 0).UTC())
	}
}

func TestIsEntrySignalRecordPrefersEntryType(t *testing.T) {
	record := []string{"1", "空头进场", "2024/3/14 14:00", "空头止损"}
	if !isEntrySignalRecord(record, 1, true, 3, true) {
		t.Fatal("isEntrySignalRecord() = false, want true for entry type")
	}

	record = []string{"1", "空头出场", "2024/3/14 16:00", "空头止损"}
	if isEntrySignalRecord(record, 1, true, 3, true) {
		t.Fatal("isEntrySignalRecord() = true, want false for exit type")
	}
}

func TestLoadEntrySignalTimesParsesTextEntriesInUTC(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "signals.txt")
	content := "Jan 19, 2023, 08:00\nJan 26, 2023, 08:00\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entryTimes, err := loadEntrySignalTimes(path)
	if err != nil {
		t.Fatalf("loadEntrySignalTimes() error = %v", err)
	}

	if len(entryTimes) != 2 {
		t.Fatalf("len(entryTimes) = %d, want 2", len(entryTimes))
	}

	firstUTC := time.Date(2023, time.January, 19, 8, 0, 0, 0, time.UTC).Unix()
	if _, ok := entryTimes[firstUTC]; !ok {
		t.Fatalf("expected entry time %v to be present", time.Unix(firstUTC, 0).UTC())
	}

	secondUTC := time.Date(2023, time.January, 26, 8, 0, 0, 0, time.UTC).Unix()
	if _, ok := entryTimes[secondUTC]; !ok {
		t.Fatalf("expected entry time %v to be present", time.Unix(secondUTC, 0).UTC())
	}
}

package usmarket

import (
	"testing"
	"time"
)

func TestTradingDaysInRangeSkipsWeekendAndHoliday(t *testing.T) {
	fromDate := time.Date(2024, time.January, 12, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2024, time.January, 16, 0, 0, 0, 0, time.UTC)

	got := tradingDaysInRange(fromDate, toDate)
	if len(got) != 2 {
		t.Fatalf("tradingDaysInRange len = %d, want 2", len(got))
	}
	if got[0].Format("2006-01-02") != "2024-01-12" {
		t.Fatalf("first trading day = %s, want 2024-01-12", got[0].Format("2006-01-02"))
	}
	if got[1].Format("2006-01-02") != "2024-01-16" {
		t.Fatalf("second trading day = %s, want 2024-01-16", got[1].Format("2006-01-02"))
	}
}

func TestMissingBarAssetInfo(t *testing.T) {
	tests := []struct {
		name       string
		assetClass string
		wantTable  string
		wantColumn string
		wantErr    bool
	}{
		{name: "stocks", assetClass: "stocks", wantTable: "us_stocks_bar_1m", wantColumn: "symbol"},
		{name: "options", assetClass: "options", wantTable: "us_options_bar_1m", wantColumn: "underlying"},
		{name: "invalid", assetClass: "crypto", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := missingBarAssetInfo(tt.assetClass)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("missingBarAssetInfo(%q) error = nil, want error", tt.assetClass)
				}
				return
			}
			if err != nil {
				t.Fatalf("missingBarAssetInfo(%q) error = %v", tt.assetClass, err)
			}
			if got.table != tt.wantTable {
				t.Fatalf("table = %q, want %q", got.table, tt.wantTable)
			}
			if got.filterColumn != tt.wantColumn {
				t.Fatalf("filterColumn = %q, want %q", got.filterColumn, tt.wantColumn)
			}
		})
	}
}

package service

import "testing"

func TestResolveUSStockTable(t *testing.T) {
	cases := []struct {
		interval string
		want     string
		wantErr  bool
	}{
		{"1m", "us_stocks_bar_1m", false},
		{"5m", "us_stocks_bar_5m", false},
		{"15m", "us_stocks_bar_15m", false},
		{"30m", "us_stocks_bar_30m", false},
		{"1h", "us_stocks_bar_1h", false},
		{"2h", "us_stocks_bar_2h", false},
		{"4h", "us_stocks_bar_4h", false},
		{"1d", "us_stocks_bar_1d", false},
		{"3m", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := resolveUSStockTable(tc.interval)
		if (err != nil) != tc.wantErr {
			t.Errorf("resolveUSStockTable(%q) error = %v, wantErr %v", tc.interval, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveUSStockTable(%q) = %q, want %q", tc.interval, got, tc.want)
		}
	}
}

func TestResolveUSOptionTable(t *testing.T) {
	cases := []struct {
		interval string
		want     string
		wantErr  bool
	}{
		{"1m", "us_options_bar_1m", false},
		{"5m", "us_options_bar_5m", false},
		{"15m", "us_options_bar_15m", false},
		{"30m", "us_options_bar_30m", false},
		{"1h", "us_options_bar_1h", false},
		{"2h", "us_options_bar_2h", false},
		{"4h", "us_options_bar_4h", false},
		{"1d", "us_options_bar_1d", false},
		{"2m", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := resolveUSOptionTable(tc.interval)
		if (err != nil) != tc.wantErr {
			t.Errorf("resolveUSOptionTable(%q) error = %v, wantErr %v", tc.interval, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveUSOptionTable(%q) = %q, want %q", tc.interval, got, tc.want)
		}
	}
}

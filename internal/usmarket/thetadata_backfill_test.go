package usmarket

import (
	"strings"
	"testing"
	"time"
)

func TestBuildThetaDataEODGreeksURL(t *testing.T) {
	got, err := buildThetaDataEODGreeksURL("http://127.0.0.1:25503/v3", "SPX", time.Date(2025, 12, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, expected := range []string{
		"/option/history/greeks/eod?",
		"symbol=SPX",
		"expiration=%2A",
		"start_date=2025-12-23",
		"end_date=2025-12-23",
		"format=json",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected %q in %q", expected, got)
		}
	}
}

func TestApplyThetaDailyGreeks(t *testing.T) {
	marketDate := time.Date(2025, 12, 23, 0, 0, 0, 0, time.UTC)
	expirationLocation := time.FixedZone("UTC+8", 8*60*60)
	rows := []OptionBar1m{
		{
			Timestamp:         time.Date(2025, 12, 23, 15, 0, 0, 0, time.UTC),
			Underlying:        "DJX",
			OptionType:        "C",
			Expiration:        time.Date(2025, 12, 26, 0, 0, 0, 0, expirationLocation),
			Strike:            480,
			MarketDate:        marketDate,
			UnderlyingClose:   float32(0),
			ImpliedVolatility: float32(0),
			Delta:             float32(0),
		},
		{
			Timestamp:         time.Date(2025, 12, 23, 15, 1, 0, 0, time.UTC),
			Underlying:        "DJX",
			OptionType:        "P",
			Expiration:        time.Date(2025, 12, 26, 0, 0, 0, 0, expirationLocation),
			Strike:            470,
			MarketDate:        marketDate,
			UnderlyingClose:   float32(0),
			ImpliedVolatility: float32(0),
			Delta:             float32(0),
		},
	}

	greeks := map[optionContractKey]DailyGreekValues{
		makeOptionContractKey(time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC), 480, "C"): {
			UnderlyingClose:   484.42,
			ImpliedVolatility: 0.1096,
			Delta:             0.8308,
			Gamma:             0.0523,
			Vega:              11.0772,
			Theta:             -0.2422,
			Rho:               3.2669,
		},
	}

	updatedRows, originalRows, matchedContracts, unmatchedContracts := ApplyThetaDailyGreeks(rows, greeks)
	if len(updatedRows) != 1 {
		t.Fatalf("updated rows: got %d, want 1", len(updatedRows))
	}
	if len(originalRows) != 1 {
		t.Fatalf("original rows: got %d, want 1", len(originalRows))
	}
	if len(matchedContracts) != 1 {
		t.Fatalf("matched contracts: got %d, want 1", len(matchedContracts))
	}
	if len(unmatchedContracts) != 1 {
		t.Fatalf("unmatched contracts: got %d, want 1", len(unmatchedContracts))
	}

	bar := updatedRows[0]
	if bar.UnderlyingClose != 484.42 {
		t.Fatalf("underlying close: got %v, want 484.42", bar.UnderlyingClose)
	}
	if bar.Delta != 0.8308 {
		t.Fatalf("delta: got %v, want 0.8308", bar.Delta)
	}
	if bar.Gamma != 0.0523 {
		t.Fatalf("gamma: got %v, want 0.0523", bar.Gamma)
	}
}

func TestNormalizeThetaDataRight(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
		ok    bool
	}{
		{"CALL", "C", true},
		{"PUT", "P", true},
		{"c", "C", true},
		{"p", "P", true},
		{"BOTH", "", false},
	} {
		got, ok := normalizeThetaDataRight(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("normalizeThetaDataRight(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestMergeGreekValuesPrefersPrimary(t *testing.T) {
	keyA := makeOptionContractKey(time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC), 480, "C")
	keyB := makeOptionContractKey(time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC), 470, "P")

	fallback := map[optionContractKey]DailyGreekValues{
		keyA: {Delta: 0.5},
		keyB: {Delta: -0.4},
	}
	primary := map[optionContractKey]DailyGreekValues{
		keyA: {Delta: 0.8},
	}

	merged := MergeGreekValues(primary, fallback)
	if len(merged) != 2 {
		t.Fatalf("merged size: got %d, want 2", len(merged))
	}
	if merged[keyA].Delta != 0.8 {
		t.Fatalf("primary value should win, got delta %v", merged[keyA].Delta)
	}
	if merged[keyB].Delta != -0.4 {
		t.Fatalf("fallback value missing, got delta %v", merged[keyB].Delta)
	}
}

func TestFallbackContracts(t *testing.T) {
	rows := []OptionBar1m{
		{Underlying: "DJX", Expiration: time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC), Strike: 480, OptionType: "C"},
		{Underlying: "DJX", Expiration: time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC), Strike: 470, OptionType: "P"},
	}
	available := map[optionContractKey]DailyGreekValues{
		makeOptionContractKey(time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC), 480, "C"): {Delta: 0.8},
	}

	contracts := FallbackContracts(rows, available)
	if len(contracts) != 1 {
		t.Fatalf("fallback contracts: got %d, want 1", len(contracts))
	}
	if contracts[0].Strike != 470 || contracts[0].OptionType != "P" {
		t.Fatalf("unexpected fallback contract: %+v", contracts[0])
	}
}

func TestThetaDataCandidateSymbols(t *testing.T) {
	for _, tt := range []struct {
		underlying string
		want       []string
	}{
		{"SPX", []string{"SPX", "SPXW", "SPXQ", "SPXPM"}},
		{"RUT", []string{"RUT", "RUTW", "RUTQ"}},
		{"VIXW", []string{"VIXW", "VIX"}},
		{"NDX", []string{"NDX", "NDXP"}},
		{"XSP", []string{"XSP", "XSPPM", "XSPAM"}},
		{"DJX", []string{"DJX"}},
	} {
		got := thetaDataCandidateSymbols(tt.underlying)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Fatalf("thetaDataCandidateSymbols(%q) = %v, want %v", tt.underlying, got, tt.want)
		}
	}
}

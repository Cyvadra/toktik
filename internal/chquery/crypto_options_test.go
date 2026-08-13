package chquery

import (
	"strings"
	"testing"
	"time"
)

func TestCryptoOptionsDailyChainSQLUsesUTCDateWindow(t *testing.T) {
	day := time.Date(2026, time.August, 13, 22, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	query := CryptoOptionsDailyChainSQL("BTC", day)

	for _, want := range []string{
		"FROM crypto_options_chain_1d",
		"base_asset = 'BTC'",
		"timestamp >= toDateTime('2026-08-13 00:00:00', 'UTC')",
		"timestamp < toDateTime('2026-08-14 00:00:00', 'UTC')",
		"LEFT JOIN crypto_spot_bar_1d",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

func TestCryptoOptionsBarsWithUnderlyingSQLIncludesImpliedVolatility(t *testing.T) {
	query := CryptoOptionsBarsWithUnderlyingSQL("SELECT 1", "SELECT 2", 1001)
	if !strings.Contains(query, "AS implied_volatility") {
		t.Fatalf("expected implied_volatility projection, got %q", query)
	}
	if !strings.Contains(query, "mark_iv_close") {
		t.Fatalf("expected mark IV fields to remain available, got %q", query)
	}
}

func TestCryptoOptionsGreeksSQLIncludesImpliedVolatility(t *testing.T) {
	query := CryptoOptionsGreeksSQL("SELECT 1", "SELECT 2", 1001)
	if !strings.Contains(query, "AS implied_volatility") {
		t.Fatalf("expected implied_volatility projection, got %q", query)
	}
	if !strings.Contains(query, "mark_iv_close") {
		t.Fatalf("expected raw IV columns to remain available, got %q", query)
	}
}

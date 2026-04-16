package chquery

import (
	"strings"
	"testing"
)

func TestCryptoOptionsBarsWithUnderlyingSQLIncludesImpliedVolatility(t *testing.T) {
	query := CryptoOptionsBarsWithUnderlyingSQL("SELECT 1", "SELECT 2")
	if !strings.Contains(query, "AS implied_volatility") {
		t.Fatalf("expected implied_volatility projection, got %q", query)
	}
	if !strings.Contains(query, "mark_iv_close") {
		t.Fatalf("expected mark IV fields to remain available, got %q", query)
	}
}

func TestCryptoOptionsGreeksSQLIncludesImpliedVolatility(t *testing.T) {
	query := CryptoOptionsGreeksSQL("SELECT 1", "SELECT 2")
	if !strings.Contains(query, "AS implied_volatility") {
		t.Fatalf("expected implied_volatility projection, got %q", query)
	}
	if !strings.Contains(query, "mark_iv_close") {
		t.Fatalf("expected raw IV columns to remain available, got %q", query)
	}
}

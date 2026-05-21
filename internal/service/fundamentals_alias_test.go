package service

import "testing"

func TestResolveFundamentalStorageSymbolUsesETFBackfillAliases(t *testing.T) {
	if got := resolveFundamentalStorageSymbol("us-stocks", "NDX", "pe"); got != "QQQ" {
		t.Fatalf("resolveFundamentalStorageSymbol(NDX, pe) = %q, want QQQ", got)
	}
	if got := resolveFundamentalStorageSymbol("us-stocks", "SPX", "pe"); got != "SPY" {
		t.Fatalf("resolveFundamentalStorageSymbol(SPX, pe) = %q, want SPY", got)
	}
	if got := resolveFundamentalStorageSymbol("us-stocks", "NDX", virtualFundamentalFactorPE10Live); got != "NDX" {
		t.Fatalf("resolveFundamentalStorageSymbol(NDX, pe10_live) = %q, want NDX", got)
	}
	if got := resolveFundamentalStorageSymbol("us-stocks", "QQQ", "pe"); got != "QQQ" {
		t.Fatalf("resolveFundamentalStorageSymbol(QQQ, pe) = %q, want QQQ", got)
	}
}

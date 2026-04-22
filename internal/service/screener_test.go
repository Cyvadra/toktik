package service

import "testing"

func TestExpirationColUsesUSArrayJoinAlias(t *testing.T) {
	if got := expirationCol(false); got != "expiration_val" {
		t.Fatalf("expirationCol(false) = %q, want expiration_val", got)
	}
	if got := expirationCol(true); got != "m.expiration" {
		t.Fatalf("expirationCol(true) = %q, want m.expiration", got)
	}
}

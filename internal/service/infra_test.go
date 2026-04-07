package service

import (
	"context"
	"testing"
)

func TestListMarketsIncludesFeatureStore(t *testing.T) {
	svc := NewInfraService(nil)
	resp, err := svc.ListMarkets(context.TODO())
	if err != nil {
		t.Fatalf("ListMarkets returned error: %v", err)
	}
	found := false
	for _, market := range resp.Markets {
		if market.Name != "feature-store" {
			continue
		}
		found = true
		if len(market.Capabilities) == 0 {
			t.Fatal("feature-store capabilities should not be empty")
		}
		hasLiquidity := false
		for _, capability := range market.Capabilities {
			if capability == "liquidity-snapshots" {
				hasLiquidity = true
				break
			}
		}
		if !hasLiquidity {
			t.Fatal("expected feature-store liquidity-snapshots capability")
		}
	}
	if !found {
		t.Fatal("expected feature-store market in catalog")
	}
}

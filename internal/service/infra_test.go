package service

import (
	"context"
	"testing"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestListDatasetsAppliesMarketFilterBeforeInspection(t *testing.T) {
	svc := NewInfraService(nil)
	seen := []string{}
	svc.inspectDatasetFn = func(_ context.Context, spec infraDatasetSpec) (dto.DatasetDescriptor, error) {
		seen = append(seen, spec.Market)
		return dto.DatasetDescriptor{Name: spec.Name, Market: spec.Market, Status: "ready", Freshness: "fresh"}, nil
	}

	resp, err := svc.ListDatasets(context.TODO(), dto.DatasetQueryRequest{Market: "forex"})
	if err != nil {
		t.Fatalf("ListDatasets returned error: %v", err)
	}
	if len(seen) != 1 || seen[0] != "forex" {
		t.Fatalf("expected only forex dataset inspection, got %v", seen)
	}
	if len(resp.Datasets) != 1 || resp.Datasets[0].Market != "forex" {
		t.Fatalf("expected one forex dataset in response, got %+v", resp.Datasets)
	}
	if resp.Summary.Total != 1 || resp.Summary.Ready != 1 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
}

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

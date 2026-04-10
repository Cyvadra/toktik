package service

import (
	"context"
	"testing"

	"github.com/Cyvadra/toktik/internal/dto"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

func TestSliceOrEmptyNil(t *testing.T) {
	values := sliceOrEmpty[polygonpkg.Quote](nil)
	if values == nil {
		t.Fatal("sliceOrEmpty should normalize nil to empty slice")
	}
	if len(values) != 0 {
		t.Fatalf("expected empty slice, got len=%d", len(values))
	}
}

func TestPolygonServiceQueryStockQuotesUsesEmptySlice(t *testing.T) {
	svc := NewPolygonService(&stubPolygonClient{}, nil)
	resp, err := svc.QueryStockQuotes(context.Background(), dto.PolygonStockQuotesRequest{Symbol: "AAPL"})
	if err != nil {
		t.Fatalf("QueryStockQuotes returned error: %v", err)
	}
	if resp.Data == nil {
		t.Fatal("expected empty quotes slice, got nil")
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected no quotes, got %d", len(resp.Data))
	}
}

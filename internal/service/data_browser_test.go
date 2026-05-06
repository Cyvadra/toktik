package service

import (
	"context"
	"testing"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestDataBrowserListPresets(t *testing.T) {
	svc := NewDataBrowserService(nil)
	resp, err := svc.ListBrowserPresets(context.Background())
	if err != nil {
		t.Fatalf("ListBrowserPresets returned error: %v", err)
	}
	if len(resp.Datasets) == 0 {
		t.Fatalf("expected browser datasets")
	}
	if resp.Datasets[0].Name == "" || resp.Datasets[0].Relation == "" {
		t.Fatalf("unexpected empty dataset descriptor: %+v", resp.Datasets[0])
	}
}

func TestDataBrowserRejectsUnknownDataset(t *testing.T) {
	svc := NewDataBrowserService(nil)
	_, err := svc.QueryDatasetSchema(context.Background(), dto.BrowserSchemaRequest{Dataset: "not-a-dataset"})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if _, ok := err.(*dto.ValidationError); !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

func TestSelectBrowserColumnsRejectsUnknownColumn(t *testing.T) {
	columnMap := map[string]dto.BrowserColumn{"timestamp": {Name: "timestamp"}}
	_, err := selectBrowserColumns("timestamp,missing", []string{"timestamp"}, columnMap)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestNormalizeBrowserLimit(t *testing.T) {
	if got := normalizeBrowserLimit(0); got != browserDefaultLimit {
		t.Fatalf("expected default limit, got %d", got)
	}
	if got := normalizeBrowserLimit(browserMaxLimit + 1); got != browserMaxLimit {
		t.Fatalf("expected capped limit, got %d", got)
	}
}

func TestBrowserValueFieldsDedupesSymbolAndUnderlying(t *testing.T) {
	fields := browserValueFields(browserDatasetSpec{SymbolField: "symbol", UnderlyingField: "symbol"})
	if len(fields) != 1 || fields[0] != "symbol" {
		t.Fatalf("expected one deduped field, got %#v", fields)
	}
}

func TestNormalizeBrowserValueLimit(t *testing.T) {
	if got := normalizeBrowserValueLimit(0); got != browserValueDefaultLimit {
		t.Fatalf("expected default value limit, got %d", got)
	}
	if got := normalizeBrowserValueLimit(browserValueMaxLimit + 1); got != browserValueMaxLimit {
		t.Fatalf("expected capped value limit, got %d", got)
	}
}

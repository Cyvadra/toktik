package service

import (
	"context"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chrepo"
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

func TestDataBrowserPreviewPreservesNullAndEmptyString(t *testing.T) {
	conn := &fakeForexConn{rowSets: []driver.Rows{
		&fakeForexRows{data: [][]any{
			{"timestamp", "Nullable(DateTime)", uint64(1), "", "", "", ""},
			{"symbol", "Nullable(String)", uint64(2), "", "", "", ""},
		}},
		&fakeForexRows{data: [][]any{{nil, ""}}},
	}}
	svc := NewDataBrowserService(chrepo.NewRepo(conn))
	svc.datasets = map[string]browserDatasetSpec{
		"test": {
			Name:           "test",
			Relation:       "test_relation",
			DefaultColumns: []string{"timestamp", "symbol"},
		},
	}

	resp, err := svc.QueryDatasetPreview(context.Background(), dto.BrowserPreviewRequest{Dataset: "test"})
	if err != nil {
		t.Fatalf("QueryDatasetPreview returned error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected one preview row, got %d", len(resp.Data))
	}
	if got := resp.Data[0]["timestamp"]; got != nil {
		t.Fatalf("timestamp = %#v, want nil", got)
	}
	if got := resp.Data[0]["symbol"]; got != "" {
		t.Fatalf("symbol = %#v, want empty string", got)
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

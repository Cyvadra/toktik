package usmarket

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

func TestDownloadFlatFileRangeStopsOn404(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	var requested []string

	files, lastAvailable, err := downloadFlatFileRange(ctx, start, false, func(_ context.Context, date time.Time, _ bool) (string, error) {
		requested = append(requested, date.UTC().Format("2006-01-02"))
		switch date.UTC().Format("2006-01-02") {
		case "2026-04-07", "2026-04-08":
			return "/tmp/" + date.UTC().Format("2006-01-02") + ".csv.gz", nil
		default:
			return "", &polygonpkg.HTTPStatusError{URL: "http://example.test", StatusCode: http.StatusNotFound, Status: "404 Not Found"}
		}
	})
	if err != nil {
		t.Fatalf("downloadFlatFileRange failed: %v", err)
	}

	wantRequests := []string{"2026-04-07", "2026-04-08", "2026-04-09"}
	if !reflect.DeepEqual(requested, wantRequests) {
		t.Fatalf("unexpected requested dates: got=%v want=%v", requested, wantRequests)
	}
	wantFiles := []string{"/tmp/2026-04-07.csv.gz", "/tmp/2026-04-08.csv.gz"}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("unexpected files: got=%v want=%v", files, wantFiles)
	}
	if got := lastAvailable.Format("2006-01-02"); got != "2026-04-08" {
		t.Fatalf("unexpected last available date: %s", got)
	}
}

func TestDownloadFlatFileRangePropagatesNon404(t *testing.T) {
	ctx := context.Background()
	_, _, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, date time.Time, _ bool) (string, error) {
		return "", fmt.Errorf("boom on %s", date.Format("2006-01-02"))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || got == "boom" {
		t.Fatalf("expected wrapped error, got %q", got)
	}
}

func TestResolveFlatFileStartDate(t *testing.T) {
	latest := time.Date(2026, 4, 8, 13, 0, 0, 0, time.UTC)
	start, err := resolveFlatFileStartDate("stocks", latest, true)
	if err != nil {
		t.Fatalf("resolve with existing data failed: %v", err)
	}
	if got := start.Format("2006-01-02"); got != "2026-04-09" {
		t.Fatalf("unexpected next start date: %s", got)
	}

	if _, err := resolveFlatFileStartDate("options", time.Time{}, false); err == nil {
		t.Fatal("expected missing initial start date error")
	}
}

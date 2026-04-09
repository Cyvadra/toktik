package usmarket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

func TestDownloadFlatFileRangeSkipsMissingDatesThroughEndDate(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	var requested []string

	files, lastAvailable, err := downloadFlatFileRange(ctx, start, end, false, func(_ context.Context, date time.Time, _ bool) (string, error) {
		requested = append(requested, date.UTC().Format("2006-01-02"))
		switch date.UTC().Format("2006-01-02") {
		case "2026-04-07", "2026-04-08", "2026-04-10":
			return "/tmp/" + date.UTC().Format("2006-01-02") + ".csv.gz", nil
		default:
			return "", &polygonpkg.HTTPStatusError{URL: "http://example.test", StatusCode: http.StatusNotFound, Status: "404 Not Found"}
		}
	})
	if err != nil {
		t.Fatalf("downloadFlatFileRange failed: %v", err)
	}

	wantRequests := []string{"2026-04-07", "2026-04-08", "2026-04-09", "2026-04-10"}
	if !reflect.DeepEqual(requested, wantRequests) {
		t.Fatalf("unexpected requested dates: got=%v want=%v", requested, wantRequests)
	}
	wantFiles := []string{"/tmp/2026-04-07.csv.gz", "/tmp/2026-04-08.csv.gz", "/tmp/2026-04-10.csv.gz"}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("unexpected files: got=%v want=%v", files, wantFiles)
	}
	if got := lastAvailable.Format("2006-01-02"); got != "2026-04-10" {
		t.Fatalf("unexpected last available date: %s", got)
	}
}

func TestDownloadFlatFileRangePropagatesNon404(t *testing.T) {
	ctx := context.Background()
	_, _, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, date time.Time, _ bool) (string, error) {
		return "", fmt.Errorf("boom on %s", date.Format("2006-01-02"))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || got == "boom" {
		t.Fatalf("expected wrapped error, got %q", got)
	}
}

func TestDownloadFlatFileRangeHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, _ time.Time, _ bool) (string, error) {
		return "/tmp/ignored.csv.gz", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDownloadFlatFileRangeReturnsZeroLastDownloadedWhenAllDatesMissing(t *testing.T) {
	ctx := context.Background()
	files, lastDownloaded, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, _ time.Time, _ bool) (string, error) {
		return "", &polygonpkg.HTTPStatusError{URL: "http://example.test", StatusCode: http.StatusNotFound, Status: "404 Not Found"}
	})
	if err != nil {
		t.Fatalf("downloadFlatFileRange failed: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %v", files)
	}
	if !lastDownloaded.IsZero() {
		t.Fatalf("expected zero last downloaded date, got %s", lastDownloaded.Format("2006-01-02"))
	}
}

func TestResolveFlatFileStartDate(t *testing.T) {
	latest := time.Date(2026, 4, 8, 13, 0, 0, 0, time.UTC)
	start, err := resolveFlatFileStartDate("stocks", latest, true, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolve with existing data failed: %v", err)
	}
	if got := start.Format("2006-01-02"); got != "2026-04-09" {
		t.Fatalf("unexpected next start date: %s", got)
	}

	coldStart, err := resolveFlatFileStartDate("options", time.Time{}, false, time.Date(2023, 1, 1, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolve cold start failed: %v", err)
	}
	if got := coldStart.Format("2006-01-02"); got != "2023-01-01" {
		t.Fatalf("unexpected cold start date: %s", got)
	}
}

func TestResolveFlatFileEndDateUsesUTCYesterday(t *testing.T) {
	end := resolveFlatFileEndDate(func() time.Time {
		return time.Date(2026, 4, 10, 1, 30, 0, 0, time.FixedZone("CST", 8*3600))
	})
	if got := end.Format("2006-01-02"); got != "2026-04-08" {
		t.Fatalf("unexpected end date: %s", got)
	}
}

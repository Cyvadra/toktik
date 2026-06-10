package usmarket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

func TestDownloadFlatFileRangeSkipsMissingDatesThroughEndDate(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	var requested []string

	result, err := downloadFlatFileRange(ctx, start, end, false, func(_ context.Context, date time.Time, _ bool) (string, error) {
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
	if !reflect.DeepEqual(result.Files, wantFiles) {
		t.Fatalf("unexpected files: got=%v want=%v", result.Files, wantFiles)
	}
	if got := result.LastDownloaded.Format("2006-01-02"); got != "2026-04-10" {
		t.Fatalf("unexpected last available date: %s", got)
	}
	if got := formatFlatFileDateList(result.SkippedDates); got != "2026-04-09" {
		t.Fatalf("unexpected skipped dates: %s", got)
	}
}

func TestDownloadFlatFileRangePropagatesNon404(t *testing.T) {
	ctx := context.Background()
	_, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, date time.Time, _ bool) (string, error) {
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

	_, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, _ time.Time, _ bool) (string, error) {
		return "/tmp/ignored.csv.gz", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDownloadFlatFileRangeReturnsZeroLastDownloadedWhenAllDatesMissing(t *testing.T) {
	ctx := context.Background()
	result, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, _ time.Time, _ bool) (string, error) {
		return "", &polygonpkg.HTTPStatusError{URL: "http://example.test", StatusCode: http.StatusNotFound, Status: "404 Not Found"}
	})
	if err != nil {
		t.Fatalf("downloadFlatFileRange failed: %v", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("expected no files, got %v", result.Files)
	}
	if !result.LastDownloaded.IsZero() {
		t.Fatalf("expected zero last downloaded date, got %s", result.LastDownloaded.Format("2006-01-02"))
	}
	if got := len(result.SkippedDates); got != 2 {
		t.Fatalf("expected 2 skipped dates, got %d", got)
	}
}

func TestDownloadFlatFileDatesRequestsOnlyNormalizedUniqueDates(t *testing.T) {
	ctx := context.Background()
	var requested []string
	result, err := downloadFlatFileDates(ctx, []time.Time{
		time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 7, 9, 0, 0, 0, time.UTC),
	}, false, func(_ context.Context, date time.Time, _ bool) (string, error) {
		requested = append(requested, date.UTC().Format("2006-01-02"))
		return "/tmp/" + date.UTC().Format("2006-01-02") + ".csv.gz", nil
	})
	if err != nil {
		t.Fatalf("downloadFlatFileDates failed: %v", err)
	}
	wantRequests := []string{"2026-04-07", "2026-04-10"}
	if !reflect.DeepEqual(requested, wantRequests) {
		t.Fatalf("unexpected requested dates: got=%v want=%v", requested, wantRequests)
	}
	wantFiles := []string{"/tmp/2026-04-07.csv.gz", "/tmp/2026-04-10.csv.gz"}
	if !reflect.DeepEqual(result.Files, wantFiles) {
		t.Fatalf("unexpected files: got=%v want=%v", result.Files, wantFiles)
	}
	if got := result.LastDownloaded.Format("2006-01-02"); got != "2026-04-10" {
		t.Fatalf("unexpected last downloaded date: %s", got)
	}
}

func TestDownloadFlatFileRangeSkipsMissingLikePermissionErrors(t *testing.T) {
	ctx := context.Background()
	result, err := downloadFlatFileRange(ctx, time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC), false, func(_ context.Context, date time.Time, _ bool) (string, error) {
		if date.Format("2006-01-02") == "2026-04-24" {
			return "", fmt.Errorf("Insufficient permissions to access this path")
		}
		return "/tmp/" + date.Format("2006-01-02") + ".csv.gz", nil
	})
	if err != nil {
		t.Fatalf("downloadFlatFileRange failed: %v", err)
	}
	if got := formatFlatFileDateList(result.SkippedDates); got != "2026-04-24" {
		t.Fatalf("unexpected skipped dates: %s", got)
	}
}

func TestFormatFlatFileSyncSummarySeparatesTradingAndNonTradingSkips(t *testing.T) {
	result := FlatFileSyncResult{
		Stocks: FlatFileAssetResult{
			AssetClass:     "stocks",
			AttemptedDates: []time.Time{time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)},
			SkippedDates:   []time.Time{time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)},
			Files:          []string{"/tmp/2026-04-10.csv.gz"},
		},
	}
	lines := FormatFlatFileSyncSummary(result)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "polygon stocks flatfiles: scan=none..none latest_imported=none last_downloaded=none attempted_days=3 downloaded_days=1 skipped_days=2 skipped_ratio=66.7%") {
		t.Fatalf("missing stock summary in %q", joined)
	}
	if !strings.Contains(joined, "polygon stocks downloaded dates: 2026-04-10") {
		t.Fatalf("missing downloaded date list in %q", joined)
	}
	if !strings.Contains(joined, "polygon stocks skipped dates: 2026-04-11, 2026-04-13") {
		t.Fatalf("missing skipped date list in %q", joined)
	}
	if !strings.Contains(joined, "polygon flatfiles combined: attempted_days=3 skipped_days=2 skipped_ratio=66.7%") {
		t.Fatalf("missing combined summary in %q", joined)
	}
	if !strings.Contains(joined, "polygon flatfiles skipped classification: non_trading=1 trading_days=1") {
		t.Fatalf("missing classification summary in %q", joined)
	}
	if !strings.Contains(joined, "polygon flatfiles skipped trading dates: 2026-04-13") {
		t.Fatalf("missing trading date list in %q", joined)
	}
}

func TestResolveFlatFileStartDate(t *testing.T) {
	latest := time.Date(2026, 4, 8, 13, 0, 0, 0, time.UTC)
	start, err := resolveFlatFileStartDate("stocks", latest, true, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatalf("resolve with existing data failed: %v", err)
	}
	if got := start.Format("2006-01-02"); got != "2026-04-09" {
		t.Fatalf("unexpected next start date: %s", got)
	}

	coldStart, err := resolveFlatFileStartDate("options", time.Time{}, false, time.Date(2023, 1, 1, 15, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatalf("resolve cold start failed: %v", err)
	}
	if got := coldStart.Format("2006-01-02"); got != "2023-01-01" {
		t.Fatalf("unexpected cold start date: %s", got)
	}
}

func TestResolveFlatFileStartDateUsesOverrideRange(t *testing.T) {
	latest := time.Date(2026, 4, 8, 13, 0, 0, 0, time.UTC)
	start, err := resolveFlatFileStartDate(
		"stocks",
		latest,
		true,
		time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2022, 5, 1, 9, 30, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("resolve override start failed: %v", err)
	}
	if got := start.Format("2006-01-02"); got != "2022-05-01" {
		t.Fatalf("unexpected override start date: %s", got)
	}
}

func TestResolveFlatFileEndDateUsesUTCYesterday(t *testing.T) {
	end, err := resolveFlatFileEndDate(func() time.Time {
		return time.Date(2026, 4, 10, 1, 30, 0, 0, time.FixedZone("CST", 8*3600))
	}, time.Time{})
	if err != nil {
		t.Fatalf("resolve end date failed: %v", err)
	}
	if got := end.Format("2006-01-02"); got != "2026-04-08" {
		t.Fatalf("unexpected end date: %s", got)
	}
}

func TestResolveFlatFileEndDateUsesOverride(t *testing.T) {
	end, err := resolveFlatFileEndDate(nil, time.Date(2022, 12, 31, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolve override end date failed: %v", err)
	}
	if got := end.Format("2006-01-02"); got != "2022-12-31" {
		t.Fatalf("unexpected override end date: %s", got)
	}
}

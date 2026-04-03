package cryptooptions

import (
	"strings"
	"testing"
	"time"
)

func TestBuildChainBackfillInsertSQLUsesDirect1mPath(t *testing.T) {
	t.Parallel()

	sql := buildChainBackfillInsertSQL("crypto_options_chain_1m_agg", KlineInterval{Suffix: "1m", TimeFunc: "timestamp"}, time.Time{}, time.Time{}, "")
	if strings.Contains(sql, "min(timestamp)                    AS first_ts") {
		t.Fatalf("1m chain backfill should not use regrouped first_ts query: %s", sql)
	}
	if !strings.Contains(sql, "FROM crypto_options_bar_1m") {
		t.Fatalf("1m chain backfill should read directly from 1m source table: %s", sql)
	}
	if !strings.Contains(sql, "timestamp AS ts") {
		t.Fatalf("1m chain backfill should alias timestamp to ts: %s", sql)
	}
	if !strings.Contains(sql, "sumState(toUInt64(tick_count))   AS tick_count_state") {
		t.Fatalf("1m chain backfill should cast tick_count to UInt64: %s", sql)
	}
	if !strings.Contains(sql, "argMaxState(open_interest, timestamp) AS open_interest_state") {
		t.Fatalf("1m chain backfill should aggregate open interest directly on timestamp: %s", sql)
	}
}

func TestBuildChainBackfillInsertSQLUsesRegroupedPathForLargerIntervals(t *testing.T) {
	t.Parallel()

	sql := buildChainBackfillInsertSQL("crypto_options_chain_5m_agg", KlineInterval{Suffix: "5m", TimeFunc: "toStartOfFiveMinutes(timestamp)"}, time.Time{}, time.Time{}, "")
	if !strings.Contains(sql, "min(timestamp)                    AS first_ts") {
		t.Fatalf("5m chain backfill should compute first_ts in subquery: %s", sql)
	}
	if !strings.Contains(sql, "argMinState(delta, first_ts)          AS delta_state") {
		t.Fatalf("5m chain backfill should use first_ts in outer aggregation: %s", sql)
	}
	if !strings.Contains(sql, "argMaxState(open_interest, last_ts)   AS open_interest_state") {
		t.Fatalf("5m chain backfill should use last_ts for open interest: %s", sql)
	}
}

func TestSplitBackfillWindowsUsesInclusiveExclusiveChunks(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	windows := splitBackfillWindows(from, to, 24*time.Hour)
	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}
	if !windows[0].From.Equal(from) || !windows[0].To.Equal(from.Add(24*time.Hour)) {
		t.Fatalf("unexpected first window: %+v", windows[0])
	}
	if !windows[2].From.Equal(from.Add(48*time.Hour)) || !windows[2].To.Equal(to) {
		t.Fatalf("unexpected last window: %+v", windows[2])
	}
}

package cryptooptions

import (
	"strings"
	"testing"
)

func TestChainCacheDDLMaterializedViewUsesSourceTimestampAlias(t *testing.T) {
	t.Parallel()

	stmts := chainCacheDDL(KlineInterval{Suffix: "1m", TimeFunc: "timestamp"})
	if len(stmts) != 3 {
		t.Fatalf("expected 3 DDL statements, got %d", len(stmts))
	}

	mv := stmts[1]
	if !strings.Contains(mv, "min(timestamp)                    AS first_ts") {
		t.Fatalf("materialized view should expose first_ts alias: %s", mv)
	}
	if !strings.Contains(mv, "max(timestamp)                    AS last_ts") {
		t.Fatalf("materialized view should expose last_ts alias: %s", mv)
	}
	if strings.Contains(mv, "argMinState(delta, timestamp)") {
		t.Fatalf("materialized view should not reference timestamp directly in outer aggregate: %s", mv)
	}
	if !strings.Contains(mv, "argMinState(delta, first_ts)") {
		t.Fatalf("materialized view should use first_ts in argMinState: %s", mv)
	}
	if !strings.Contains(mv, "argMaxState(open_interest, last_ts)") {
		t.Fatalf("materialized view should use last_ts for open interest aggregate: %s", mv)
	}
}

func TestChainCacheDDLViewMergesBeforeGroupingIntoArrays(t *testing.T) {
	t.Parallel()

	stmts := chainCacheDDL(KlineInterval{Suffix: "1m", TimeFunc: "timestamp"})
	if len(stmts) != 3 {
		t.Fatalf("expected 3 DDL statements, got %d", len(stmts))
	}

	view := stmts[2]
	if strings.Contains(view, "groupArray(tuple(\n                symbol_id,\n                argMinMerge") {
		t.Fatalf("view should not nest aggregate merges inside groupArray: %s", view)
	}
	if !strings.Contains(view, "argMinMerge(delta_state)         AS delta") {
		t.Fatalf("view should merge states in inner subquery: %s", view)
	}
	if !strings.Contains(view, "groupArray(tuple(\n                symbol_id,\n                delta,") {
		t.Fatalf("view should build arrays from merged scalar values: %s", view)
	}
}

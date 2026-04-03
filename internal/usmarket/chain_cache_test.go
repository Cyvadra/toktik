package usmarket

import (
	"strings"
	"testing"
)

func TestChainCacheDDLViewMergesBeforeGroupingIntoArrays(t *testing.T) {
	t.Parallel()

	stmts := optionChainCacheDDL(KlineInterval{Suffix: "1m"})
	if len(stmts) != 4 {
		t.Fatalf("expected 4 DDL statements, got %d", len(stmts))
	}

	view := stmts[3]
	if strings.Contains(view, "groupArray(tuple(\n                symbol,\n                argMaxMerge") {
		t.Fatalf("view should not nest aggregate merges inside groupArray: %s", view)
	}
	if !strings.Contains(view, "argMaxMerge(option_type_state)        AS option_type") {
		t.Fatalf("view should merge states in inner subquery: %s", view)
	}
	if !strings.Contains(view, "groupArray(tuple(\n                symbol,\n                option_type,") {
		t.Fatalf("view should build arrays from merged scalar values: %s", view)
	}
}

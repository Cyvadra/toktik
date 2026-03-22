package cryptooptions

import (
	"strings"
	"testing"
)

func TestKlineBucketExprAnchorsToUTCEpoch(t *testing.T) {
	expr, err := klineBucketExpr("timestamp", "8h")
	if err != nil {
		t.Fatalf("klineBucketExpr returned error: %v", err)
	}

	want := "toDateTime(intDiv(toUnixTimestamp(timestamp), 28800) * 28800, 'UTC')"
	if expr != want {
		t.Fatalf("unexpected 8h bucket expression:\n got: %s\nwant: %s", expr, want)
	}
}

func TestQuerySpotAggregationSQLUsesUTCBucketExpression(t *testing.T) {
	query, err := QuerySpotAggregationSQL("8h")
	if err != nil {
		t.Fatalf("QuerySpotAggregationSQL returned error: %v", err)
	}

	want := "toDateTime(intDiv(toUnixTimestamp(timestamp), 28800) * 28800, 'UTC')"
	if count := strings.Count(query, want); count != 2 {
		t.Fatalf("expected UTC bucket expression twice, got %d occurrences in query:\n%s", count, query)
	}
	if strings.Contains(query, "toStartOfInterval(timestamp, INTERVAL 8 hour)") {
		t.Fatalf("query still uses timezone-sensitive 8h bucketing:\n%s", query)
	}
}

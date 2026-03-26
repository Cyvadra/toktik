package cryptooptions

import (
	"strings"
	"testing"
)

func TestBuildBarSubqueriesUseUTCTimeParsing(t *testing.T) {
	t.Parallel()

	optionSQL, err := BuildOptionBarSubquery("1m")
	if err != nil {
		t.Fatalf("BuildOptionBarSubquery: %v", err)
	}
	if !strings.Contains(optionSQL, "toDateTime({from:String}, 'UTC')") {
		t.Fatalf("option subquery missing UTC from-bound: %s", optionSQL)
	}
	if !strings.Contains(optionSQL, "toDateTime({to:String}, 'UTC')") {
		t.Fatalf("option subquery missing UTC to-bound: %s", optionSQL)
	}

	spotSQL, err := BuildSpotBarSubquery("1m")
	if err != nil {
		t.Fatalf("BuildSpotBarSubquery: %v", err)
	}
	if !strings.Contains(spotSQL, "toDateTime({from:String}, 'UTC')") {
		t.Fatalf("spot subquery missing UTC from-bound: %s", spotSQL)
	}
	if !strings.Contains(spotSQL, "toDateTime({to:String}, 'UTC')") {
		t.Fatalf("spot subquery missing UTC to-bound: %s", spotSQL)
	}
}

func TestQueryTimeAggregationSQLUsesUTCTimeParsing(t *testing.T) {
	t.Parallel()

	optionSQL, err := QueryTimeAggregationSQL("3h")
	if err != nil {
		t.Fatalf("QueryTimeAggregationSQL: %v", err)
	}
	if !strings.Contains(optionSQL, "toDateTime({from:String}, 'UTC')") {
		t.Fatalf("option aggregation missing UTC from-bound: %s", optionSQL)
	}
	if !strings.Contains(optionSQL, "toDateTime({to:String}, 'UTC')") {
		t.Fatalf("option aggregation missing UTC to-bound: %s", optionSQL)
	}

	spotSQL, err := QuerySpotAggregationSQL("3h")
	if err != nil {
		t.Fatalf("QuerySpotAggregationSQL: %v", err)
	}
	if !strings.Contains(spotSQL, "toDateTime({from:String}, 'UTC')") {
		t.Fatalf("spot aggregation missing UTC from-bound: %s", spotSQL)
	}
	if !strings.Contains(spotSQL, "toDateTime({to:String}, 'UTC')") {
		t.Fatalf("spot aggregation missing UTC to-bound: %s", spotSQL)
	}
}

func TestKlineDDLUsesUTCTimestamps(t *testing.T) {
	t.Parallel()

	optionDDL := optionKlineDDLWithPrefix("crypto_options", KlineInterval{Suffix: "3h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 3 hour)"})
	if !strings.Contains(optionDDL[0], "DateTime('UTC')") {
		t.Fatalf("option agg DDL missing UTC timestamp types: %s", optionDDL[0])
	}

	spotDDL := spotKlineDDLWithPrefix("crypto_spot", KlineInterval{Suffix: "3h", TimeFunc: "toStartOfInterval(timestamp, INTERVAL 3 hour)"})
	if !strings.Contains(spotDDL[0], "DateTime('UTC')") {
		t.Fatalf("spot agg DDL missing UTC timestamp types: %s", spotDDL[0])
	}
}

package chquery

import (
	"strings"
	"testing"
)

func TestFundamentalsAggregateQueriesUseNonConflictingAliases(t *testing.T) {
	tests := []struct {
		name              string
		query             string
		requiredSnippets  []string
		forbiddenSnippets []string
	}{
		{
			name:  "series as-of",
			query: FundamentalSeriesAsOfQuery(),
			requiredSnippets: []string{
				"AS latest_known_at",
				"AS latest_value",
				"AS latest_source",
				"AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')",
			},
			forbiddenSnippets: []string{
				"AS known_at",
			},
		},
		{
			name:  "snapshot",
			query: FundamentalSnapshotQuery(),
			requiredSnippets: []string{
				"AS latest_event_ts",
				"AS latest_known_at",
				"AS latest_value",
				"AS latest_source",
				"AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')",
			},
			forbiddenSnippets: []string{
				"AS event_ts",
				"AS known_at",
			},
		},
		{
			name:  "panel",
			query: FundamentalPanelQuery(),
			requiredSnippets: []string{
				"AS latest_event_ts",
				"AS latest_known_at",
				"AS latest_value",
				"AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')",
			},
			forbiddenSnippets: []string{
				"AS event_ts",
				"AS known_at",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, snippet := range tt.requiredSnippets {
				if !strings.Contains(tt.query, snippet) {
					t.Fatalf("expected query to contain %q, got %q", snippet, tt.query)
				}
			}
			for _, snippet := range tt.forbiddenSnippets {
				if strings.Contains(tt.query, snippet) {
					t.Fatalf("expected query to avoid %q to prevent alias/column collisions, got %q", snippet, tt.query)
				}
			}
		})
	}
}

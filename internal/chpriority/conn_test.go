package chpriority

import (
	"context"
	"strings"
	"testing"

	"github.com/Cyvadra/toktik/internal/requestpriority"
)

func TestWorkloadSettings(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		workload string
	}{
		{name: "interactive", ctx: requestpriority.WithPriority(context.Background(), requestpriority.Interactive), workload: "toktik_interactive"},
		{name: "default", ctx: context.Background(), workload: "default"},
		{name: "background", ctx: requestpriority.WithBackground(context.Background()), workload: "toktik_background"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := DefaultWorkloads().settings(requestpriority.FromContext(tt.ctx))
			if got := settings["workload"]; got != tt.workload {
				t.Fatalf("workload = %#v, want %q", got, tt.workload)
			}
			if got := settings["log_comment"]; got != "toktik:"+tt.name {
				t.Fatalf("log_comment = %#v", got)
			}
		})
	}
}

func TestDDLUsesConfiguredSchedulerLimits(t *testing.T) {
	ddl := DDL(SchedulerLimits{
		MaxConcurrentQueries: 20,
		MaxConcurrentThreads: 12,
		BackgroundQueries:    2,
		BackgroundThreads:    3,
	})
	for _, want := range []string{
		"max_concurrent_queries = 20 FOR toktik_query",
		"max_concurrent_threads = 12 FOR toktik_master_cpu",
		"CREATE WORKLOAD IF NOT EXISTS toktik_interactive",
		"CREATE WORKLOAD IF NOT EXISTS toktik_background",
		"max_concurrent_queries = 2 FOR toktik_query",
		"max_concurrent_threads = 3 FOR toktik_worker_cpu",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("DDL() does not contain %q:\n%s", want, ddl)
		}
	}
}

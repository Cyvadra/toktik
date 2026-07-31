// Package chpriority applies request scheduling settings to ClickHouse calls.
package chpriority

import (
	"context"
	"fmt"
	"strings"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/requestpriority"
)

type Workloads struct {
	Interactive string
	Default     string
	Background  string
}

func DefaultWorkloads() Workloads {
	return Workloads{
		Interactive: "toktik_interactive",
		Default:     "default",
		Background:  "toktik_background",
	}
}

// SchedulerLimits controls the shared ClickHouse workload pool. A zero value
// uses the defaults sized for the standard deployment.
type SchedulerLimits struct {
	MaxConcurrentQueries int
	MaxConcurrentThreads int
	BackgroundQueries    int
	BackgroundThreads    int
}

func DefaultSchedulerLimits() SchedulerLimits {
	return SchedulerLimits{
		MaxConcurrentQueries: 64,
		MaxConcurrentThreads: 64,
		BackgroundQueries:    4,
		BackgroundThreads:    8,
	}
}

func (l SchedulerLimits) normalized() SchedulerLimits {
	defaults := DefaultSchedulerLimits()
	if l.MaxConcurrentQueries <= 0 {
		l.MaxConcurrentQueries = defaults.MaxConcurrentQueries
	}
	if l.MaxConcurrentThreads <= 0 {
		l.MaxConcurrentThreads = defaults.MaxConcurrentThreads
	}
	if l.BackgroundQueries <= 0 {
		l.BackgroundQueries = defaults.BackgroundQueries
	}
	if l.BackgroundThreads <= 0 {
		l.BackgroundThreads = defaults.BackgroundThreads
	}
	return l
}

// DDL returns idempotent workload scheduler DDL for the standard Toktik
// workload names. ClickHouse identifiers are fixed package constants, while
// capacity is supplied by deployment configuration.
func DDL(limits SchedulerLimits) string {
	limits = limits.normalized()
	workloads := DefaultWorkloads()
	return fmt.Sprintf(`CREATE RESOURCE IF NOT EXISTS toktik_query (QUERY);
CREATE RESOURCE IF NOT EXISTS toktik_master_cpu (MASTER THREAD);
CREATE RESOURCE IF NOT EXISTS toktik_worker_cpu (WORKER THREAD);

CREATE WORKLOAD IF NOT EXISTS toktik_all SETTINGS
    max_concurrent_queries = %[1]d FOR toktik_query,
    max_concurrent_threads = %[2]d FOR toktik_master_cpu,
    max_concurrent_threads = %[2]d FOR toktik_worker_cpu;

CREATE WORKLOAD IF NOT EXISTS %[3]s IN toktik_all SETTINGS
    priority = 0,
    weight = 3;

CREATE WORKLOAD IF NOT EXISTS %[4]s IN toktik_all SETTINGS
    priority = -1,
    weight = 8;

CREATE WORKLOAD IF NOT EXISTS %[5]s IN toktik_all SETTINGS
    priority = 1,
    weight = 1,
    max_concurrent_queries = %[6]d FOR toktik_query,
    max_concurrent_threads = %[6]d FOR toktik_master_cpu,
    max_concurrent_threads = %[7]d FOR toktik_worker_cpu;`,
		limits.MaxConcurrentQueries,
		limits.MaxConcurrentThreads,
		workloads.Default,
		workloads.Interactive,
		workloads.Background,
		limits.BackgroundQueries,
		limits.BackgroundThreads,
	)
}

// Init creates the workload scheduler resources and workload classes.
func Init(ctx context.Context, conn driver.Conn, limits SchedulerLimits) error {
	for _, statement := range strings.Split(DDL(limits), ";") {
		if statement = strings.TrimSpace(statement); statement == "" {
			continue
		}
		if err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("initialize ClickHouse priority workload: %w", err)
		}
	}
	return nil
}

func (w Workloads) workload(priority requestpriority.Priority) string {
	switch priority {
	case requestpriority.Interactive:
		return w.Interactive
	case requestpriority.Background:
		return w.Background
	default:
		return w.Default
	}
}

func (w Workloads) settings(priority requestpriority.Priority) clickhouse.Settings {
	return clickhouse.Settings{
		"workload":    w.workload(priority),
		"log_comment": "toktik:" + string(priority),
	}
}

type Conn struct {
	driver.Conn
	workloads Workloads
}

func Wrap(conn driver.Conn, workloads Workloads) driver.Conn {
	if conn == nil {
		return nil
	}
	if workloads.Interactive == "" || workloads.Default == "" || workloads.Background == "" {
		workloads = DefaultWorkloads()
	}
	return &Conn{Conn: conn, workloads: workloads}
}

func (c *Conn) withPriority(ctx context.Context) context.Context {
	priority := requestpriority.FromContext(ctx)
	return clickhouse.Context(ctx, clickhouse.WithSettings(c.workloads.settings(priority)))
}

func (c *Conn) Select(ctx context.Context, dest any, query string, args ...any) error {
	return c.Conn.Select(c.withPriority(ctx), dest, query, args...)
}

func (c *Conn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.Conn.Query(c.withPriority(ctx), query, args...)
}

func (c *Conn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return c.Conn.QueryRow(c.withPriority(ctx), query, args...)
}

func (c *Conn) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	return c.Conn.PrepareBatch(c.withPriority(ctx), query, opts...)
}

func (c *Conn) Exec(ctx context.Context, query string, args ...any) error {
	return c.Conn.Exec(c.withPriority(ctx), query, args...)
}

func (c *Conn) AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error {
	return c.Conn.AsyncInsert(c.withPriority(ctx), query, wait, args...)
}

func (c *Conn) Ping(ctx context.Context) error {
	return c.Conn.Ping(c.withPriority(ctx))
}

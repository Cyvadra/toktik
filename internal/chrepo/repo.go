// Package chrepo provides a thin repository layer over ClickHouse.
// It encapsulates driver.Conn and exposes helper methods used by services.
package chrepo

import (
	"context"
	"fmt"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
)

// Repo is the base repository holding a shared ClickHouse connection.
type Repo struct {
	Conn driver.Conn
}

// NewRepo creates a Repo from an existing ClickHouse connection.
func NewRepo(conn driver.Conn) *Repo {
	return &Repo{Conn: conn}
}

// Ping checks ClickHouse connectivity.
func (r *Repo) Ping(ctx context.Context) error {
	return r.Conn.Ping(ctx)
}

// RelationExists returns true if a table or view exists in the current database.
func (r *Repo) RelationExists(ctx context.Context, relation string) (bool, error) {
	rows, err := r.Conn.Query(ctx, chquery.RelationExists, clickhouse.Named("relation", relation))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// RelationLastTimestamp returns the maximum value of timeField in the given relation.
func (r *Repo) RelationLastTimestamp(ctx context.Context, relation, timeField string) (time.Time, bool, error) {
	partRows, err := r.Conn.Query(ctx, chquery.RelationPartLastTimestamp, clickhouse.Named("relation", relation))
	if err == nil {
		defer partRows.Close()
		if partRows.Next() {
			var partLastTS time.Time
			if err := partRows.Scan(&partLastTS); err == nil {
				if !partLastTS.IsZero() && partLastTS.UTC().Unix() != 0 {
					return partLastTS.UTC(), true, nil
				}
			}
		}
	}

	query := chquery.RelationLastTimestamp(relation, timeField)
	rows, err := r.Conn.Query(ctx, query)
	if err != nil {
		return time.Time{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var lastTS time.Time
	if err := rows.Scan(&lastTS); err != nil {
		return time.Time{}, false, err
	}
	if lastTS.IsZero() || lastTS.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return lastTS.UTC(), true, nil
}

// RelationRowCount returns the total row count in the given relation.
func (r *Repo) RelationRowCount(ctx context.Context, relation string) (uint64, error) {
	rows, err := r.Conn.Query(ctx, chquery.RelationRowCount, clickhouse.Named("relation", relation))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// Query delegates to the underlying connection's Query method.
func (r *Repo) Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error) {
	return r.Conn.Query(ctx, query, args...)
}

// QueryRow executes a query that returns at most one row and scans it into dest.
func (r *Repo) QueryRow(ctx context.Context, query string, dest []interface{}, args ...interface{}) error {
	rows, err := r.Conn.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return fmt.Errorf("no rows returned")
	}
	return rows.Scan(dest...)
}

// Exec delegates to the underlying connection's Exec method.
func (r *Repo) Exec(ctx context.Context, query string, args ...interface{}) error {
	return r.Conn.Exec(ctx, query, args...)
}

// PrepareBatch delegates to the underlying connection's PrepareBatch method.
func (r *Repo) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	return r.Conn.PrepareBatch(ctx, query, opts...)
}

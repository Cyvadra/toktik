package polymarket

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

func InitSchema(ctx context.Context, conn driver.Conn, ddlPath string) error {
	data, err := os.ReadFile(ddlPath)
	if err != nil {
		return fmt.Errorf("read Polymarket DDL file %s: %w", ddlPath, err)
	}
	if err := rebuildEmptyLegacyEventTable(ctx, conn); err != nil {
		return err
	}
	for _, statement := range splitSQLStatements(string(data)) {
		if err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("exec Polymarket DDL: %w\nStatement: %s", err, statement)
		}
	}
	return nil
}

func rebuildEmptyLegacyEventTable(ctx context.Context, conn driver.Conn) error {
	var exists uint8
	var partitionKey string
	err := conn.QueryRow(ctx, `SELECT count(), any(partition_key)
		FROM system.tables
		WHERE database = currentDatabase() AND name = 'polymarket_l2_event'`,
	).Scan(&exists, &partitionKey)
	if err != nil {
		return fmt.Errorf("inspect Polymarket event table: %w", err)
	}
	if exists == 0 {
		return nil
	}
	if strings.TrimSpace(partitionKey) == "import_hour" {
		return nil
	}
	var rows uint64
	if err := conn.QueryRow(ctx, "SELECT count() FROM polymarket_l2_event").Scan(&rows); err != nil {
		return fmt.Errorf("count legacy Polymarket event rows: %w", err)
	}
	if rows != 0 {
		return fmt.Errorf("polymarket_l2_event uses legacy partition key %q with %d rows; migrate or clear it before enabling hourly archive replacements", partitionKey, rows)
	}
	if err := conn.Exec(ctx, "DROP TABLE polymarket_l2_event"); err != nil {
		return fmt.Errorf("drop empty legacy Polymarket event table: %w", err)
	}
	return nil
}

func splitSQLStatements(ddl string) []string {
	lines := strings.Split(ddl, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if index := strings.Index(line, "--"); index >= 0 {
			line = line[:index]
		}
		filtered = append(filtered, line)
	}
	parts := strings.Split(strings.Join(filtered, "\n"), ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

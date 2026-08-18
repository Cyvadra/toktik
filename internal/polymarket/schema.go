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
	for _, statement := range splitSQLStatements(string(data)) {
		if err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("exec Polymarket DDL: %w\nStatement: %s", err, statement)
		}
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

package usmarket

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const OptionWallTable = "us_options_option_wall_daily"

// InitOptionWallSchema creates the durable daily option-wall storage table.
// Rows are keyed by symbol + expiration + snapshot day and newer payloads replace older ones.
func InitOptionWallSchema(ctx context.Context, conn driver.Conn) error {
	if err := conn.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s
(
	snapshot_day Date,
	symbol LowCardinality(String),
	expiration Date,
	payload String,
	updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (symbol, expiration, snapshot_day)
SETTINGS index_granularity = 8192`, OptionWallTable)); err != nil {
		return fmt.Errorf("create option wall table: %w", err)
	}
	return nil
}

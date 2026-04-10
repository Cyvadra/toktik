package cryptooptions

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type SymbolIDMigrationOptions struct {
	DDLPath      string
	BackupSuffix string
	DryRun       bool
}

func MigrateCryptoOptionSymbolIDs(ctx context.Context, conn driver.Conn, opts SymbolIDMigrationOptions) error {
	ddlPath := strings.TrimSpace(opts.DDLPath)
	if ddlPath == "" {
		return fmt.Errorf("ddl path is required")
	}

	optionBaseExists, err := tableExists(ctx, conn, "crypto_options_bar_1m")
	if err != nil {
		return fmt.Errorf("check crypto_options_bar_1m: %w", err)
	}
	metaExists, err := tableExists(ctx, conn, "crypto_options_symbol_meta")
	if err != nil {
		return fmt.Errorf("check crypto_options_symbol_meta: %w", err)
	}
	if !optionBaseExists || !metaExists {
		return fmt.Errorf("expected crypto_options_bar_1m and crypto_options_symbol_meta to exist before migration")
	}

	backupSuffix := strings.TrimSpace(opts.BackupSuffix)
	if backupSuffix == "" {
		backupSuffix = time.Now().UTC().Format("20060102_150405")
	}
	backupSuffix = sanitizeMigrationSuffix(backupSuffix)

	legacyMeta := "crypto_options_symbol_meta_crc32_" + backupSuffix
	legacyBar := "crypto_options_bar_1m_crc32_" + backupSuffix
	legacyBarRows, err := approxTableRows(ctx, conn, "crypto_options_bar_1m")
	if err != nil {
		return fmt.Errorf("inspect crypto_options_bar_1m rows: %w", err)
	}
	legacyMetaRows, err := approxTableRows(ctx, conn, "crypto_options_symbol_meta")
	if err != nil {
		return fmt.Errorf("inspect crypto_options_symbol_meta rows: %w", err)
	}
	partitions, err := activePartitions(ctx, conn, "crypto_options_bar_1m")
	if err != nil {
		return fmt.Errorf("inspect crypto_options_bar_1m partitions: %w", err)
	}

	if exists, err := tableExists(ctx, conn, legacyMeta); err != nil {
		return fmt.Errorf("check backup table %s: %w", legacyMeta, err)
	} else if exists {
		return fmt.Errorf("backup table %s already exists", legacyMeta)
	}
	if exists, err := tableExists(ctx, conn, legacyBar); err != nil {
		return fmt.Errorf("check backup table %s: %w", legacyBar, err)
	} else if exists {
		return fmt.Errorf("backup table %s already exists", legacyBar)
	}

	log.Printf("[symbol-id-migrate] hash=%s backup_suffix=%s dry_run=%t legacy_meta_rows=%d legacy_bar_rows=%d partitions=%d", SymbolIDHashName, backupSuffix, opts.DryRun, legacyMetaRows, legacyBarRows, len(partitions))
	if opts.DryRun {
		log.Printf("[symbol-id-migrate] dry-run: would drop crypto option derived objects, rename current base tables to %s and %s, recreate UInt64 schema, backfill symbol metadata once, and backfill option bars partition-by-partition using %s(symbol)", legacyMeta, legacyBar, SymbolIDHashName)
		return nil
	}

	if err := dropChainCacheObjects(ctx, conn); err != nil {
		return fmt.Errorf("drop chain cache objects: %w", err)
	}
	if err := dropOptionKlineObjects(ctx, conn, "crypto_options"); err != nil {
		return fmt.Errorf("drop option kline objects: %w", err)
	}

	renameStmt := fmt.Sprintf("RENAME TABLE crypto_options_symbol_meta TO %s, crypto_options_bar_1m TO %s", legacyMeta, legacyBar)
	if err := execMigrationDDL(ctx, conn, renameStmt); err != nil {
		return fmt.Errorf("rename legacy crypto option tables: %w", err)
	}

	if err := InitSchema(ctx, conn, ddlPath); err != nil {
		return fmt.Errorf("recreate crypto option base schema: %w", err)
	}
	if err := InitKlineSchema(ctx, conn); err != nil {
		return fmt.Errorf("recreate option kline schema: %w", err)
	}
	if err := InitChainCacheSchema(ctx, conn); err != nil {
		return fmt.Errorf("recreate chain cache schema: %w", err)
	}

	metaInsert := fmt.Sprintf(`INSERT INTO crypto_options_symbol_meta
SELECT
    xxHash64(symbol) AS symbol_id,
    symbol,
    base_asset,
    option_type,
    strike_price,
    expiration,
    underlying_index
FROM %s FINAL`, legacyMeta)
	if err := execMigrationDDL(ctx, conn, metaInsert); err != nil {
		return fmt.Errorf("backfill crypto_options_symbol_meta: %w", err)
	}

	for _, partition := range partitions {
		barInsert := fmt.Sprintf(`INSERT INTO crypto_options_bar_1m
(
	timestamp,
	symbol_id,
	base_asset,
	mark_open,
	mark_high,
	mark_low,
	mark_close,
	last_open,
	last_high,
	last_low,
	last_close,
	bid_open,
	bid_high,
	bid_low,
	bid_close,
	ask_open,
	ask_high,
	ask_low,
	ask_close,
	mark_iv_open,
	mark_iv_close,
	bid_iv_open,
	ask_iv_open,
	delta,
	gamma,
	vega,
	theta,
	rho,
	open_interest,
	tick_count
)
SELECT
    b.timestamp,
    xxHash64(m.symbol) AS symbol_id,
    b.base_asset,
    b.mark_open,
    b.mark_high,
    b.mark_low,
    b.mark_close,
    b.last_open,
    b.last_high,
    b.last_low,
    b.last_close,
    b.bid_open,
    b.bid_high,
    b.bid_low,
    b.bid_close,
    b.ask_open,
    b.ask_high,
    b.ask_low,
    b.ask_close,
    b.mark_iv_open,
    b.mark_iv_close,
    b.bid_iv_open,
    b.ask_iv_open,
    b.delta,
    b.gamma,
    b.vega,
    b.theta,
    b.rho,
    b.open_interest,
    b.tick_count
FROM %s AS b
INNER JOIN
(
    SELECT
        symbol_id,
        anyLast(symbol) AS symbol
    FROM %s
    GROUP BY symbol_id
) AS m ON m.symbol_id = b.symbol_id
WHERE toYYYYMM(b.timestamp) = %s`, legacyBar, legacyMeta, partition)
		log.Printf("[symbol-id-migrate] backfill partition %s", partition)
		if err := execMigrationDDL(ctx, conn, barInsert); err != nil {
			return fmt.Errorf("backfill crypto_options_bar_1m partition %s: %w", partition, err)
		}
	}

	if err := verifyMigratedSymbolIDs(ctx, conn, legacyMetaRows, legacyBarRows); err != nil {
		return err
	}

	log.Printf("[symbol-id-migrate] migration completed; legacy tables retained as %s and %s", legacyMeta, legacyBar)
	return nil
}

func approxTableRows(ctx context.Context, conn driver.Conn, tableName string) (uint64, error) {
	rows, err := conn.Query(ctx, `SELECT sum(rows)
FROM system.parts
WHERE database = currentDatabase()
  AND table = {table_name:String}
  AND active`, clickhouse.Named("table_name", tableName))
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
	return count, rows.Err()
}

func activePartitions(ctx context.Context, conn driver.Conn, tableName string) ([]string, error) {
	rows, err := conn.Query(ctx, `SELECT partition
FROM system.parts
WHERE database = currentDatabase()
  AND table = {table_name:String}
  AND active
GROUP BY partition
ORDER BY partition`, clickhouse.Named("table_name", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	partitions := make([]string, 0, 64)
	for rows.Next() {
		var partition string
		if err := rows.Scan(&partition); err != nil {
			return nil, err
		}
		partitions = append(partitions, partition)
	}
	return partitions, rows.Err()
}

func verifyMigratedSymbolIDs(ctx context.Context, conn driver.Conn, wantMetaRows, wantBarRows uint64) error {
	gotMetaRows, err := approxTableRows(ctx, conn, "crypto_options_symbol_meta")
	if err != nil {
		return fmt.Errorf("verify crypto_options_symbol_meta rows: %w", err)
	}
	gotBarRows, err := approxTableRows(ctx, conn, "crypto_options_bar_1m")
	if err != nil {
		return fmt.Errorf("verify crypto_options_bar_1m rows: %w", err)
	}
	if gotMetaRows != wantMetaRows {
		return fmt.Errorf("verify crypto_options_symbol_meta rows: got %d want %d", gotMetaRows, wantMetaRows)
	}
	if gotBarRows != wantBarRows {
		return fmt.Errorf("verify crypto_options_bar_1m rows: got %d want %d", gotBarRows, wantBarRows)
	}

	collisionRows, err := countSymbolIDCollisions(ctx, conn, "crypto_options_symbol_meta")
	if err != nil {
		return fmt.Errorf("verify symbol ID collisions: %w", err)
	}
	if collisionRows != 0 {
		return fmt.Errorf("verify symbol ID collisions: found %d colliding IDs after migration", collisionRows)
	}

	log.Printf("[symbol-id-migrate] verified migrated rows: meta=%d bar_1m=%d collisions=%d", gotMetaRows, gotBarRows, collisionRows)
	return nil
}

func countSymbolIDCollisions(ctx context.Context, conn driver.Conn, tableName string) (uint64, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT count()
FROM
(
    SELECT symbol_id
    FROM %s
    GROUP BY symbol_id
    HAVING uniqExact(symbol) > 1
)`, tableName))
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
	return count, rows.Err()
}

func sanitizeMigrationSuffix(s string) string {
	var builder strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_':
			builder.WriteRune(r)
		case r == '-':
			builder.WriteRune('_')
		}
	}
	if builder.Len() == 0 {
		return time.Now().UTC().Format("20060102_150405")
	}
	return builder.String()
}

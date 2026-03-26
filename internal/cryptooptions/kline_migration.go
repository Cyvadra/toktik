package cryptooptions

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type KlineUTCMigrationOptions struct {
	Intervals    []string
	From         time.Time
	To           time.Time
	BaseAsset    string
	SkipBackfill bool
	DryRun       bool
}

func MigrateCryptoKlineSchemaToUTC(ctx context.Context, conn driver.Conn, opts KlineUTCMigrationOptions) error {
	intervals, err := normalizeBackfillIntervals(opts.Intervals)
	if err != nil {
		return err
	}

	optionBaseExists, err := tableExists(ctx, conn, "crypto_options_bar_1m")
	if err != nil {
		return fmt.Errorf("check crypto_options_bar_1m: %w", err)
	}
	spotBaseExists, err := tableExists(ctx, conn, "crypto_spot_bar_1m")
	if err != nil {
		return fmt.Errorf("check crypto_spot_bar_1m: %w", err)
	}
	if !optionBaseExists && !spotBaseExists {
		return fmt.Errorf("no crypto 1m base tables found; expected crypto_options_bar_1m and/or crypto_spot_bar_1m")
	}

	baseAsset := strings.ToUpper(strings.TrimSpace(opts.BaseAsset))
	log.Printf("[kline-migrate-utc] option_base=%t spot_base=%t intervals=%s from=%s to=%s base_asset=%q skip_backfill=%t dry_run=%t",
		optionBaseExists,
		spotBaseExists,
		strings.Join(intervals, ","),
		formatMigrationTime(opts.From),
		formatMigrationTime(opts.To),
		baseAsset,
		opts.SkipBackfill,
		opts.DryRun,
	)

	if opts.DryRun {
		log.Printf("[kline-migrate-utc] dry-run: would drop and recreate crypto K-line aggregates/views, alter 1m timestamp columns to DateTime('UTC'), and optionally backfill selected windows")
		return nil
	}

	if optionBaseExists {
		if err := dropOptionKlineObjects(ctx, conn, "crypto_options"); err != nil {
			return err
		}
		if err := alterTimestampColumnToUTC(ctx, conn, "crypto_options_bar_1m"); err != nil {
			return err
		}
		if err := initOptionKlineSchemaForPrefix(ctx, conn, "crypto_options"); err != nil {
			return err
		}
	}

	if spotBaseExists {
		if err := dropSpotKlineObjects(ctx, conn, "crypto_spot"); err != nil {
			return err
		}
		if err := alterTimestampColumnToUTC(ctx, conn, "crypto_spot_bar_1m"); err != nil {
			return err
		}
		if err := initSpotKlineSchemaForPrefix(ctx, conn, "crypto_spot"); err != nil {
			return err
		}
	}

	if opts.SkipBackfill {
		log.Printf("[kline-migrate-utc] migration completed without backfill")
		return nil
	}

	if err := backfillCryptoKlineWindows(ctx, conn, optionBaseExists, spotBaseExists, intervals, opts.From, opts.To, baseAsset); err != nil {
		return err
	}

	log.Printf("[kline-migrate-utc] migration completed")
	return nil
}

func formatMigrationTime(t time.Time) string {
	if t.IsZero() {
		return "<zero>"
	}
	return t.UTC().Format(time.RFC3339)
}

func tableExists(ctx context.Context, conn driver.Conn, tableName string) (bool, error) {
	rows, err := conn.Query(ctx, `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = {table_name:String}`,
		clickhouse.Named("table_name", tableName))
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

func dropOptionKlineObjects(ctx context.Context, conn driver.Conn, prefix string) error {
	for _, iv := range KlineIntervals {
		viewName := prefix + "_bar_" + iv.Suffix
		mvName := prefix + "_bar_" + iv.Suffix + "_mv"
		aggName := prefix + "_bar_" + iv.Suffix + "_agg"

		if err := execMigrationDDL(ctx, conn, fmt.Sprintf("DROP VIEW IF EXISTS %s", viewName)); err != nil {
			return fmt.Errorf("drop option view %s: %w", viewName, err)
		}
		if err := execMigrationDDL(ctx, conn, fmt.Sprintf("DROP TABLE IF EXISTS %s", mvName)); err != nil {
			return fmt.Errorf("drop option mv %s: %w", mvName, err)
		}
		if err := execMigrationDDL(ctx, conn, fmt.Sprintf("DROP TABLE IF EXISTS %s", aggName)); err != nil {
			return fmt.Errorf("drop option agg %s: %w", aggName, err)
		}
	}
	return nil
}

func dropSpotKlineObjects(ctx context.Context, conn driver.Conn, prefix string) error {
	for _, iv := range KlineIntervals {
		viewName := prefix + "_bar_" + iv.Suffix
		mvName := prefix + "_bar_" + iv.Suffix + "_mv"
		aggName := prefix + "_bar_" + iv.Suffix + "_agg"

		if err := execMigrationDDL(ctx, conn, fmt.Sprintf("DROP VIEW IF EXISTS %s", viewName)); err != nil {
			return fmt.Errorf("drop spot view %s: %w", viewName, err)
		}
		if err := execMigrationDDL(ctx, conn, fmt.Sprintf("DROP TABLE IF EXISTS %s", mvName)); err != nil {
			return fmt.Errorf("drop spot mv %s: %w", mvName, err)
		}
		if err := execMigrationDDL(ctx, conn, fmt.Sprintf("DROP TABLE IF EXISTS %s", aggName)); err != nil {
			return fmt.Errorf("drop spot agg %s: %w", aggName, err)
		}
	}
	return nil
}

func execMigrationDDL(ctx context.Context, conn driver.Conn, stmt string) error {
	log.Printf("[kline-migrate-utc] exec: %s", stmt)
	return conn.Exec(ctx, stmt)
}

func alterTimestampColumnToUTC(ctx context.Context, conn driver.Conn, tableName string) error {
	stmt := fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN timestamp DateTime('UTC')", tableName)
	if err := execMigrationDDL(ctx, conn, stmt); err != nil {
		return fmt.Errorf("alter %s timestamp to UTC: %w", tableName, err)
	}
	return nil
}

func initOptionKlineSchemaForPrefix(ctx context.Context, conn driver.Conn, prefix string) error {
	for _, iv := range KlineIntervals {
		for _, stmt := range optionKlineDDLWithPrefix(prefix, iv) {
			if err := execMigrationDDL(ctx, conn, stmt); err != nil {
				return fmt.Errorf("init option kline schema [%s/%s]: %w", prefix, iv.Suffix, err)
			}
		}
	}
	return nil
}

func initSpotKlineSchemaForPrefix(ctx context.Context, conn driver.Conn, prefix string) error {
	for _, iv := range KlineIntervals {
		for _, stmt := range spotKlineDDLWithPrefix(prefix, iv) {
			if err := execMigrationDDL(ctx, conn, stmt); err != nil {
				return fmt.Errorf("init spot kline schema [%s/%s]: %w", prefix, iv.Suffix, err)
			}
		}
	}
	return nil
}

func backfillCryptoKlineWindows(ctx context.Context, conn driver.Conn, optionBaseExists, spotBaseExists bool, intervals []string, from, to time.Time, baseAsset string) error {
	intervalToConfig := make(map[string]KlineInterval, len(KlineIntervals))
	for _, iv := range KlineIntervals {
		intervalToConfig[iv.Suffix] = iv
	}

	for _, interval := range intervals {
		if interval == "1m" {
			continue
		}
		iv, ok := intervalToConfig[interval]
		if !ok {
			return fmt.Errorf("interval %q is not precomputed", interval)
		}
		if optionBaseExists {
			if err := backfillOptionInterval(ctx, conn, iv, from, to, baseAsset, false); err != nil {
				return fmt.Errorf("backfill option interval %s: %w", interval, err)
			}
		}
		if spotBaseExists {
			if err := backfillSpotInterval(ctx, conn, iv, from, to, baseAsset, false); err != nil {
				return fmt.Errorf("backfill spot interval %s: %w", interval, err)
			}
		}
	}
	return nil
}

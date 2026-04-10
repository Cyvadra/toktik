package usmarket

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type VolumeSchemaMigrationOptions struct {
	DryRun bool
}

func MigrateUSVolumeSchema(ctx context.Context, conn driver.Conn, opts VolumeSchemaMigrationOptions) error {
	if opts.DryRun {
		log.Printf("[volume-migrate][us] dry-run: would modify base volume columns to Float64, recreate US option/stock kline tables and option chain caches, then rebuild all aggregates")
		return nil
	}

	if err := ensureMarketVolumeColumns(ctx, conn); err != nil {
		return fmt.Errorf("ensure US base volume columns: %w", err)
	}
	if err := dropUSOptionKlineObjects(ctx, conn); err != nil {
		return fmt.Errorf("drop US option kline objects: %w", err)
	}
	if err := dropUSStockKlineObjects(ctx, conn); err != nil {
		return fmt.Errorf("drop US stock kline objects: %w", err)
	}
	if err := dropUSOptionChainCacheObjects(ctx, conn); err != nil {
		return fmt.Errorf("drop US option chain cache objects: %w", err)
	}
	if err := InitOptionKlineSchema(ctx, conn); err != nil {
		return fmt.Errorf("init US option kline schema: %w", err)
	}
	if err := InitStockKlineSchema(ctx, conn); err != nil {
		return fmt.Errorf("init US stock kline schema: %w", err)
	}
	if err := InitOptionChainCacheSchema(ctx, conn); err != nil {
		return fmt.Errorf("init US option chain cache schema: %w", err)
	}
	if err := RebuildOptionKlineAggregates(ctx, conn); err != nil {
		return fmt.Errorf("rebuild US option kline aggregates: %w", err)
	}
	if err := RebuildStockKlineAggregates(ctx, conn); err != nil {
		return fmt.Errorf("rebuild US stock kline aggregates: %w", err)
	}
	if err := RebuildOptionChainCaches(ctx, conn); err != nil {
		return fmt.Errorf("rebuild US option chain caches: %w", err)
	}
	log.Printf("[volume-migrate][us] migration completed")
	return nil
}

func RebuildStockKlineAggregates(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		agg := "us_stocks_bar_" + iv.Suffix + "_agg"
		if err := conn.Exec(ctx, `TRUNCATE TABLE `+agg); err != nil {
			return fmt.Errorf("truncate stock kline aggregate [%s]: %w", iv.Suffix, err)
		}
		if err := conn.Exec(ctx, stockKlineRebuildSQL(iv)); err != nil {
			return fmt.Errorf("rebuild stock kline aggregate [%s]: %w", iv.Suffix, err)
		}
		log.Printf("[us-stocks-kline] rebuilt %s interval", iv.Suffix)
	}
	return nil
}

func stockKlineRebuildSQL(iv KlineInterval) string {
	return fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol,
    argMinState(open, timestamp)       AS open_state,
    maxState(high)                     AS high_state,
    minState(low)                      AS low_state,
    argMaxState(close, timestamp)      AS close_state,
    sumState(volume)                   AS volume_state,
    sumState(transactions)             AS transactions_state
FROM us_stocks_bar_1m
WHERE is_regular_session = 1
GROUP BY ts, symbol`, "us_stocks_bar_"+iv.Suffix+"_agg", klineTimeFunc(iv))
}

func dropUSOptionKlineObjects(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		view := "us_options_bar_" + iv.Suffix
		mv := view + "_mv"
		agg := view + "_agg"
		if err := conn.Exec(ctx, "DROP VIEW IF EXISTS "+view); err != nil {
			return err
		}
		if err := conn.Exec(ctx, dropTableIfExistsDDL(mv)); err != nil {
			return err
		}
		if err := conn.Exec(ctx, dropTableIfExistsDDL(agg)); err != nil {
			return err
		}
	}
	return nil
}

func dropUSStockKlineObjects(ctx context.Context, conn driver.Conn) error {
	for _, iv := range KlineIntervals {
		view := "us_stocks_bar_" + iv.Suffix
		mv := view + "_mv"
		agg := view + "_agg"
		if err := conn.Exec(ctx, "DROP VIEW IF EXISTS "+view); err != nil {
			return err
		}
		if err := conn.Exec(ctx, dropTableIfExistsDDL(mv)); err != nil {
			return err
		}
		if err := conn.Exec(ctx, dropTableIfExistsDDL(agg)); err != nil {
			return err
		}
	}
	return nil
}

func dropUSOptionChainCacheObjects(ctx context.Context, conn driver.Conn) error {
	for _, iv := range DefaultChainCacheIntervals {
		view := "us_options_chain_" + iv.Suffix
		mv := view + "_mv"
		agg := view + "_agg"
		if err := conn.Exec(ctx, "DROP VIEW IF EXISTS "+view); err != nil {
			return err
		}
		if err := conn.Exec(ctx, dropTableIfExistsDDL(mv)); err != nil {
			return err
		}
		if err := conn.Exec(ctx, dropTableIfExistsDDL(agg)); err != nil {
			return err
		}
	}
	return nil
}

func dropTableIfExistsDDL(tableName string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s SETTINGS max_table_size_to_drop=0, max_partition_size_to_drop=0", tableName)
}

package cryptooptions

import (
	"context"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type VolumeSchemaMigrationOptions struct {
	DryRun bool
}

func MigrateCryptoVolumeSchema(ctx context.Context, conn driver.Conn, opts VolumeSchemaMigrationOptions) error {
	if opts.DryRun {
		log.Printf("[volume-migrate][crypto] dry-run: would ensure base volume columns, recreate crypto option/spot kline tables plus chain caches, and backfill all derived intervals")
		return nil
	}

	if err := ensureMarketVolumeColumns(ctx, conn); err != nil {
		return fmt.Errorf("ensure crypto volume columns: %w", err)
	}
	if err := dropChainCacheObjects(ctx, conn); err != nil {
		return fmt.Errorf("drop crypto chain cache objects: %w", err)
	}
	if err := dropOptionKlineObjects(ctx, conn, "crypto_options"); err != nil {
		return fmt.Errorf("drop crypto option kline objects: %w", err)
	}
	if err := dropSpotKlineObjects(ctx, conn, "crypto_spot"); err != nil {
		return fmt.Errorf("drop crypto spot kline objects: %w", err)
	}
	if err := InitKlineSchema(ctx, conn); err != nil {
		return fmt.Errorf("init crypto option kline schema: %w", err)
	}
	if err := InitSpotKlineSchema(ctx, conn); err != nil {
		return fmt.Errorf("init crypto spot kline schema: %w", err)
	}
	if err := InitChainCacheSchema(ctx, conn); err != nil {
		return fmt.Errorf("init crypto chain cache schema: %w", err)
	}
	if err := BackfillKlineWindows(ctx, conn, KlineBackfillOptions{Intervals: DefaultKlineWindows, Replace: true}); err != nil {
		return fmt.Errorf("backfill crypto derived volume schema: %w", err)
	}
	log.Printf("[volume-migrate][crypto] migration completed")
	return nil
}

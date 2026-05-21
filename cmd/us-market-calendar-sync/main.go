package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/calendarrepo"
	"github.com/Cyvadra/toktik/internal/chrepo"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/pkg/fmp"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "us-market-calendar-sync: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("us-market-calendar-sync", flag.ContinueOnError)
	target := fs.String("target", "all", "Sync target: all, economic, or watchlist")
	clickhouseDSN := fs.String("clickhouse-dsn", "", "ClickHouse DSN override for watchlist resolution")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	runtimeCfg, err := config.LoadRuntime()
	if err != nil {
		return fmt.Errorf("load runtime config: %w", err)
	}
	fmpAPIKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		return fmt.Errorf("read FMP api key: %w", err)
	}
	mysqlDSN, err := runtimeCfg.MySQLDSN()
	if err != nil {
		return fmt.Errorf("build mysql dsn: %w", err)
	}
	ctx := context.Background()
	cacheStore, err := cache.NewStore(ctx, runtimeCfg)
	if err != nil {
		cacheStore = cache.NewMemoryStore()
	}
	defer cacheStore.Close()

	calendarSvc, closeMySQL, err := newFinanceCalendarService(mysqlDSN, fmpAPIKey, runtimeCfg.FMP.CacheDir, cacheStore)
	if err != nil {
		return err
	}
	defer closeMySQL()

	switch strings.ToLower(strings.TrimSpace(*target)) {
	case "all":
		if err := syncEconomic(ctx, calendarSvc); err != nil {
			return err
		}
		return syncWatchlist(ctx, firstNonEmpty(*clickhouseDSN, runtimeCfg.ClickHouse.DSN), fmpAPIKey, cacheStore, calendarSvc)
	case "economic":
		return syncEconomic(ctx, calendarSvc)
	case "watchlist":
		return syncWatchlist(ctx, firstNonEmpty(*clickhouseDSN, runtimeCfg.ClickHouse.DSN), fmpAPIKey, cacheStore, calendarSvc)
	default:
		return fmt.Errorf("unsupported --target %q", *target)
	}
}

func syncEconomic(ctx context.Context, calendarSvc *service.FinanceCalendarService) error {
	rows, err := calendarSvc.SyncEconomicCalendar(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("economic calendar synced: rows=%d\n", rows)
	return nil
}

func syncWatchlist(ctx context.Context, clickhouseDSN, fmpAPIKey string, cacheStore cache.Store, calendarSvc *service.FinanceCalendarService) error {
	conn, err := appCli.ConnectClickHouse(ctx, clickhouseDSN, nil)
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}
	defer conn.Close()
	companyProfileProvider := service.NewCachedFMPUSStockCompanyProfileProvider(fmpAPIKey, cacheStore)
	screener := service.NewScreenerService(chrepo.NewRepo(conn), cacheStore).WithCompanyProfileProvider(companyProfileProvider)
	symbols, err := service.ResolveObservedUSStockPool(ctx, screener)
	if err != nil {
		return err
	}
	rows, err := calendarSvc.SyncStockCalendar(ctx, symbols)
	if err != nil {
		return err
	}
	fmt.Printf("observed stock calendar synced: symbols=%d rows=%d\n", len(symbols), rows)
	return nil
}

func newFinanceCalendarService(mysqlDSN, apiKey, fmpCacheDir string, cacheStore cache.Store) (*service.FinanceCalendarService, func(), error) {
	options := []fmp.Option{}
	if strings.TrimSpace(fmpCacheDir) != "" {
		options = append(options, fmp.WithCacheDir(strings.TrimSpace(fmpCacheDir)))
	}
	gormDB, err := gorm.Open(mysql.Open(mysqlDSN), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("connect mysql: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("mysql sql db: %w", err)
	}
	repo := calendarrepo.New(gormDB)
	if err := repo.AutoMigrate(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("migrate finance calendar tables: %w", err)
	}
	closeFn := func() {
		_ = sqlDB.Close()
	}
	return service.NewFinanceCalendarService(repo, fmp.New(apiKey, options...), cacheStore), closeFn, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

package usmarket

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

type FlatFileDownloader interface {
	DownloadStockMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error)
	DownloadOptionMinuteAggregates(ctx context.Context, date time.Time, force bool) (string, error)
}

type FlatFileSyncConfig struct {
	Downloader    FlatFileDownloader
	Conn          driver.Conn
	Import        ImportConfig
	Sessions      SessionMap
	ForceDownload bool
}

type FlatFileAssetResult struct {
	AssetClass      string
	LastImported    time.Time
	HasImportedData bool
	StartDate       time.Time
	LastAvailable   time.Time
	Files           []string
}

type FlatFileSyncResult struct {
	Stocks  FlatFileAssetResult
	Options FlatFileAssetResult
	Import  ImportResult
}

func SyncPolygonFlatFiles(ctx context.Context, cfg FlatFileSyncConfig) (FlatFileSyncResult, error) {
	if cfg.Downloader == nil {
		return FlatFileSyncResult{}, fmt.Errorf("flatfile downloader is required")
	}
	if cfg.Conn == nil {
		return FlatFileSyncResult{}, fmt.Errorf("clickhouse connection is required")
	}

	stocks, err := syncFlatFileAsset(ctx, flatFileAssetConfig{
		assetClass:     "stocks",
		forceDownload:  cfg.ForceDownload,
		loadLatestDate: LatestStockMarketDate,
		download:       cfg.Downloader.DownloadStockMinuteAggregates,
	}, cfg.Conn)
	if err != nil {
		return FlatFileSyncResult{}, err
	}

	options, err := syncFlatFileAsset(ctx, flatFileAssetConfig{
		assetClass:     "options",
		forceDownload:  cfg.ForceDownload,
		loadLatestDate: LatestOptionMarketDate,
		download:       cfg.Downloader.DownloadOptionMinuteAggregates,
	}, cfg.Conn)
	if err != nil {
		return FlatFileSyncResult{}, err
	}

	importResult, err := ImportFiles(ctx, cfg.Import, stocks.Files, options.Files, cfg.Sessions)
	if err != nil {
		return FlatFileSyncResult{Stocks: stocks, Options: options, Import: importResult}, err
	}

	return FlatFileSyncResult{Stocks: stocks, Options: options, Import: importResult}, nil
}

type flatFileAssetConfig struct {
	assetClass     string
	forceDownload  bool
	loadLatestDate func(context.Context, driver.Conn) (time.Time, bool, error)
	download       func(context.Context, time.Time, bool) (string, error)
}

func syncFlatFileAsset(ctx context.Context, cfg flatFileAssetConfig, conn driver.Conn) (FlatFileAssetResult, error) {
	latest, hasData, err := cfg.loadLatestDate(ctx, conn)
	if err != nil {
		return FlatFileAssetResult{}, fmt.Errorf("load latest %s market date: %w", cfg.assetClass, err)
	}

	startDate, err := resolveFlatFileStartDate(cfg.assetClass, latest, hasData)
	if err != nil {
		return FlatFileAssetResult{}, err
	}

	result := FlatFileAssetResult{
		AssetClass:      cfg.assetClass,
		LastImported:    latest,
		HasImportedData: hasData,
		StartDate:       startDate,
	}

	if startDate.IsZero() {
		return result, nil
	}

	files, lastAvailable, err := downloadFlatFileRange(ctx, startDate, cfg.forceDownload, cfg.download)
	if err != nil {
		return FlatFileAssetResult{}, fmt.Errorf("download %s flatfiles: %w", cfg.assetClass, err)
	}
	result.Files = files
	result.LastAvailable = lastAvailable

	if len(files) == 0 {
		log.Printf("No new %s flatfiles available from %s onward", cfg.assetClass, startDate.Format("2006-01-02"))
	} else {
		log.Printf("Downloaded %d %s flatfiles from %s through %s", len(files), cfg.assetClass, startDate.Format("2006-01-02"), lastAvailable.Format("2006-01-02"))
	}

	return result, nil
}

func resolveFlatFileStartDate(assetClass string, latest time.Time, hasData bool) (time.Time, error) {
	if hasData {
		return normalizeUTCDay(latest).Add(24 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("no existing %s data found; sync start date must be inferred from the database", assetClass)
}

func downloadFlatFileRange(ctx context.Context, startDate time.Time, force bool, download func(context.Context, time.Time, bool) (string, error)) ([]string, time.Time, error) {
	if startDate.IsZero() {
		return nil, time.Time{}, nil
	}

	var (
		files         []string
		lastAvailable time.Time
	)

	for current := normalizeUTCDay(startDate); ; current = current.Add(24 * time.Hour) {
		path, err := download(ctx, current, force)
		if err != nil {
			if polygonpkg.IsHTTPStatus(err, http.StatusNotFound) {
				break
			}
			return nil, time.Time{}, fmt.Errorf("download %s: %w", current.Format("2006-01-02"), err)
		}
		files = append(files, path)
		lastAvailable = current
	}

	sort.Strings(files)
	return files, lastAvailable, nil
}

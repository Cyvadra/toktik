package usmarket

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
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
	SkipStocks    bool
	SkipOptions   bool
	DryRun        bool
	Now           func() time.Time
	ColdStartDate time.Time
	StartDate     time.Time
	EndDate       time.Time
	SpecificDates []time.Time
}

type FlatFileAssetResult struct {
	AssetClass      string
	LastImported    time.Time
	HasImportedData bool
	StartDate       time.Time
	LastDownloaded  time.Time
	ScanEnd         time.Time
	Files           []string
	AttemptedDates  []time.Time
	SkippedDates    []time.Time
}

type FlatFileSyncResult struct {
	Stocks  FlatFileAssetResult
	Options FlatFileAssetResult
	Import  ImportResult
}

type flatFileDownloadResult struct {
	Files          []string
	LastDownloaded time.Time
	AttemptedDates []time.Time
	SkippedDates   []time.Time
}

func SyncPolygonFlatFiles(ctx context.Context, cfg FlatFileSyncConfig) (FlatFileSyncResult, error) {
	if cfg.Downloader == nil {
		return FlatFileSyncResult{}, fmt.Errorf("flatfile downloader is required")
	}
	if cfg.Conn == nil {
		return FlatFileSyncResult{}, fmt.Errorf("clickhouse connection is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ColdStartDate.IsZero() {
		cfg.ColdStartDate = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if cfg.SkipStocks && cfg.SkipOptions {
		return FlatFileSyncResult{}, fmt.Errorf("at least one Polygon flatfile asset class must be enabled")
	}

	stocks := FlatFileAssetResult{AssetClass: "stocks"}
	if !cfg.SkipStocks {
		var err error
		stocks, err = syncFlatFileAsset(ctx, flatFileAssetConfig{
			assetClass:     "stocks",
			forceDownload:  cfg.ForceDownload,
			now:            cfg.Now,
			coldStartDate:  cfg.ColdStartDate,
			overrideStart:  cfg.StartDate,
			overrideEnd:    cfg.EndDate,
			specificDates:  cfg.SpecificDates,
			loadLatestDate: LatestStockMarketDate,
			download:       cfg.Downloader.DownloadStockMinuteAggregates,
		}, cfg.Conn)
		if err != nil {
			return FlatFileSyncResult{}, err
		}
	}

	options := FlatFileAssetResult{AssetClass: "options"}
	if !cfg.SkipOptions {
		var err error
		options, err = syncFlatFileAsset(ctx, flatFileAssetConfig{
			assetClass:     "options",
			forceDownload:  cfg.ForceDownload,
			now:            cfg.Now,
			coldStartDate:  cfg.ColdStartDate,
			overrideStart:  cfg.StartDate,
			overrideEnd:    cfg.EndDate,
			specificDates:  cfg.SpecificDates,
			loadLatestDate: LatestOptionMarketDate,
			download:       cfg.Downloader.DownloadOptionMinuteAggregates,
		}, cfg.Conn)
		if err != nil {
			return FlatFileSyncResult{}, err
		}
	}

	if cfg.DryRun {
		log.Printf("[DRYRUN] Polygon flatfile import skipped: would import %d stock files and %d option files", len(stocks.Files), len(options.Files))
		return FlatFileSyncResult{Stocks: stocks, Options: options, Import: ImportResult{SkippedFiles: int64(len(stocks.Files) + len(options.Files))}}, nil
	}

	// Keep replace semantics at the per-file import layer so reruns overwrite by
	// market date instead of being short-circuited by a previous ledger success.
	importResult, err := ImportFiles(ctx, cfg.Import, stocks.Files, options.Files, cfg.Sessions)
	if err != nil {
		return FlatFileSyncResult{Stocks: stocks, Options: options, Import: importResult}, err
	}

	return FlatFileSyncResult{Stocks: stocks, Options: options, Import: importResult}, nil
}

func collectMarketDatesFromPaths(paths []string) ([]time.Time, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	dates := make([]time.Time, 0, len(paths))
	for _, path := range paths {
		marketDate, err := ExtractDateFromFilename(path)
		if err != nil {
			return nil, err
		}
		dates = append(dates, marketDate)
	}
	return normalizeUniqueUTCDates(dates), nil
}

type flatFileAssetConfig struct {
	assetClass     string
	forceDownload  bool
	now            func() time.Time
	coldStartDate  time.Time
	overrideStart  time.Time
	overrideEnd    time.Time
	specificDates  []time.Time
	loadLatestDate func(context.Context, driver.Conn) (time.Time, bool, error)
	download       func(context.Context, time.Time, bool) (string, error)
}

func syncFlatFileAsset(ctx context.Context, cfg flatFileAssetConfig, conn driver.Conn) (FlatFileAssetResult, error) {
	latest, hasData, err := cfg.loadLatestDate(ctx, conn)
	if err != nil {
		return FlatFileAssetResult{}, fmt.Errorf("load latest %s market date: %w", cfg.assetClass, err)
	}

	startDate, err := resolveFlatFileStartDate(cfg.assetClass, latest, hasData, cfg.coldStartDate, cfg.overrideStart)
	if err != nil {
		return FlatFileAssetResult{}, err
	}

	result := FlatFileAssetResult{
		AssetClass:      cfg.assetClass,
		LastImported:    latest,
		HasImportedData: hasData,
		StartDate:       startDate,
	}

	explicitDates := normalizeUniqueUTCDates(cfg.specificDates)
	if len(explicitDates) > 0 {
		result.StartDate = explicitDates[0]
		result.ScanEnd = explicitDates[len(explicitDates)-1]
		downloadResult, err := downloadFlatFileDates(ctx, explicitDates, cfg.forceDownload, cfg.download)
		if err != nil {
			return FlatFileAssetResult{}, fmt.Errorf("download %s flatfiles: %w", cfg.assetClass, err)
		}
		result.Files = downloadResult.Files
		result.LastDownloaded = downloadResult.LastDownloaded
		result.AttemptedDates = downloadResult.AttemptedDates
		result.SkippedDates = downloadResult.SkippedDates

		if len(downloadResult.Files) == 0 {
			log.Printf("No requested %s flatfiles were available for %d explicit dates", cfg.assetClass, len(explicitDates))
		} else {
			log.Printf("Downloaded %d %s flatfiles for %d explicit dates from %s through %s", len(downloadResult.Files), cfg.assetClass, len(explicitDates), explicitDates[0].Format("2006-01-02"), explicitDates[len(explicitDates)-1].Format("2006-01-02"))
		}
		return result, nil
	}

	if startDate.IsZero() {
		return result, nil
	}

	endDate, err := resolveFlatFileEndDate(cfg.now, cfg.overrideEnd)
	if err != nil {
		return FlatFileAssetResult{}, err
	}
	result.ScanEnd = endDate
	if startDate.After(endDate) {
		log.Printf("No new %s flatfiles available from %s onward", cfg.assetClass, startDate.Format("2006-01-02"))
		return result, nil
	}

	downloadResult, err := downloadFlatFileRange(ctx, startDate, endDate, cfg.forceDownload, cfg.download)
	if err != nil {
		return FlatFileAssetResult{}, fmt.Errorf("download %s flatfiles: %w", cfg.assetClass, err)
	}
	result.Files = downloadResult.Files
	result.LastDownloaded = downloadResult.LastDownloaded
	result.AttemptedDates = downloadResult.AttemptedDates
	result.SkippedDates = downloadResult.SkippedDates

	if len(downloadResult.Files) == 0 {
		log.Printf("No new %s flatfiles available from %s onward", cfg.assetClass, startDate.Format("2006-01-02"))
	} else {
		log.Printf("Downloaded %d %s flatfiles from %s through %s (scan end %s)", len(downloadResult.Files), cfg.assetClass, startDate.Format("2006-01-02"), downloadResult.LastDownloaded.Format("2006-01-02"), endDate.Format("2006-01-02"))
	}

	return result, nil
}

func resolveFlatFileStartDate(assetClass string, latest time.Time, hasData bool, coldStartDate, overrideStartDate time.Time) (time.Time, error) {
	if !overrideStartDate.IsZero() {
		return normalizeUTCDay(overrideStartDate), nil
	}
	if hasData {
		return normalizeUTCDay(latest).Add(24 * time.Hour), nil
	}
	if coldStartDate.IsZero() {
		return time.Time{}, fmt.Errorf("no existing %s data found and cold start date is not configured", assetClass)
	}
	return normalizeUTCDay(coldStartDate), nil
}

func resolveFlatFileEndDate(now func() time.Time, overrideEndDate time.Time) (time.Time, error) {
	if !overrideEndDate.IsZero() {
		return normalizeUTCDay(overrideEndDate), nil
	}
	if now == nil {
		now = time.Now
	}
	return normalizeUTCDay(now().UTC()).Add(-24 * time.Hour), nil
}

func downloadFlatFileRange(ctx context.Context, startDate, endDate time.Time, force bool, download func(context.Context, time.Time, bool) (string, error)) (flatFileDownloadResult, error) {
	if startDate.IsZero() {
		return flatFileDownloadResult{}, nil
	}
	if endDate.IsZero() {
		return flatFileDownloadResult{}, nil
	}
	startDate = normalizeUTCDay(startDate)
	endDate = normalizeUTCDay(endDate)
	if startDate.After(endDate) {
		return flatFileDownloadResult{LastDownloaded: endDate}, nil
	}

	result := flatFileDownloadResult{}

	for current := startDate; !current.After(endDate); current = current.Add(24 * time.Hour) {
		if err := ctx.Err(); err != nil {
			return flatFileDownloadResult{}, err
		}
		result.AttemptedDates = append(result.AttemptedDates, current)
		path, err := download(ctx, current, force)
		if err != nil {
			if isSkippableFlatFileDownloadError(err) {
				result.SkippedDates = append(result.SkippedDates, current)
				log.Printf("Skipping flatfile date %s: %v", current.Format("2006-01-02"), err)
				continue
			}
			return flatFileDownloadResult{}, fmt.Errorf("download %s: %w", current.Format("2006-01-02"), err)
		}
		result.Files = append(result.Files, path)
		result.LastDownloaded = current
	}

	sort.Strings(result.Files)
	return result, nil
}

func downloadFlatFileDates(ctx context.Context, dates []time.Time, force bool, download func(context.Context, time.Time, bool) (string, error)) (flatFileDownloadResult, error) {
	normalized := normalizeUniqueUTCDates(dates)
	if len(normalized) == 0 {
		return flatFileDownloadResult{}, nil
	}

	result := flatFileDownloadResult{AttemptedDates: append([]time.Time(nil), normalized...)}
	for _, current := range normalized {
		if err := ctx.Err(); err != nil {
			return flatFileDownloadResult{}, err
		}
		path, err := download(ctx, current, force)
		if err != nil {
			if isSkippableFlatFileDownloadError(err) {
				result.SkippedDates = append(result.SkippedDates, current)
				log.Printf("Skipping flatfile date %s: %v", current.Format("2006-01-02"), err)
				continue
			}
			return flatFileDownloadResult{}, fmt.Errorf("download %s: %w", current.Format("2006-01-02"), err)
		}
		result.Files = append(result.Files, path)
		result.LastDownloaded = current
	}

	sort.Strings(result.Files)
	return result, nil
}

func isSkippableFlatFileDownloadError(err error) bool {
	if err == nil {
		return false
	}
	if polygonpkg.IsHTTPStatus(err, http.StatusNotFound) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}
	markers := []string{
		"insufficient permissions to access this path",
		"not found",
		"404",
		"nosuchkey",
		"does not exist",
		"unable to stat source",
		"the specified key does not exist",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func FormatFlatFileSyncSummary(result FlatFileSyncResult) []string {
	assets := activeFlatFileAssetResults(result)
	if len(assets) == 0 {
		return nil
	}

	lines := make([]string, 0, len(assets)+3)
	for _, asset := range assets {
		if len(asset.AttemptedDates) == 0 {
			continue
		}
		lines = append(lines, formatFlatFileAssetSummaryLine(asset))
	}

	combinedAttempted := uniqueSortedDates(flattenFlatFileDates(assets, func(asset FlatFileAssetResult) []time.Time { return asset.AttemptedDates }))
	combinedSkipped := uniqueSortedDates(flattenFlatFileDates(assets, func(asset FlatFileAssetResult) []time.Time { return asset.SkippedDates }))
	if len(combinedAttempted) == 0 {
		return lines
	}

	ratio := flatFileSkipRatio(len(combinedSkipped), len(combinedAttempted))
	nonTrading, trading := classifySkippedMarketDates(combinedSkipped)
	lines = append(lines, fmt.Sprintf("polygon flatfiles combined: attempted_days=%d skipped_days=%d skipped_ratio=%.1f%%", len(combinedAttempted), len(combinedSkipped), ratio))
	lines = append(lines, fmt.Sprintf("polygon flatfiles skipped classification: non_trading=%d trading_days=%d", len(nonTrading), len(trading)))
	if len(trading) > 0 {
		lines = append(lines, "polygon flatfiles skipped trading dates: "+formatFlatFileDateList(trading))
	}
	return lines
}

func activeFlatFileAssetResults(result FlatFileSyncResult) []FlatFileAssetResult {
	assets := make([]FlatFileAssetResult, 0, 2)
	for _, asset := range []FlatFileAssetResult{result.Stocks, result.Options} {
		if len(asset.AttemptedDates) == 0 && len(asset.Files) == 0 && len(asset.SkippedDates) == 0 && asset.ScanEnd.IsZero() && asset.StartDate.IsZero() {
			continue
		}
		assets = append(assets, asset)
	}
	return assets
}

func formatFlatFileAssetSummaryLine(asset FlatFileAssetResult) string {
	attempted := len(asset.AttemptedDates)
	skipped := len(asset.SkippedDates)
	downloaded := len(asset.Files)
	return fmt.Sprintf("polygon %s flatfiles: attempted_days=%d downloaded_days=%d skipped_days=%d skipped_ratio=%.1f%%", asset.AssetClass, attempted, downloaded, skipped, flatFileSkipRatio(skipped, attempted))
}

func flattenFlatFileDates(assets []FlatFileAssetResult, pick func(FlatFileAssetResult) []time.Time) []time.Time {
	var dates []time.Time
	for _, asset := range assets {
		dates = append(dates, pick(asset)...)
	}
	return dates
}

func uniqueSortedDates(values []time.Time) []time.Time {
	return normalizeUniqueUTCDates(values)
}

func flatFileSkipRatio(skipped, attempted int) float64 {
	if attempted <= 0 {
		return 0
	}
	return float64(skipped) * 100 / float64(attempted)
}

func classifySkippedMarketDates(dates []time.Time) ([]time.Time, []time.Time) {
	normalized := normalizeUniqueUTCDates(dates)
	if len(normalized) == 0 {
		return nil, nil
	}
	startYear := normalized[0].Year()
	endYear := normalized[len(normalized)-1].Year()
	sessionMap := make(SessionMap)
	for _, session := range GenerateSessionCalendar(startYear, endYear) {
		sessionMap[session.MarketDate.Format("2006-01-02")] = session
	}
	nonTrading := make([]time.Time, 0)
	trading := make([]time.Time, 0)
	for _, date := range normalized {
		session := sessionMap.Lookup(date)
		if session == nil || session.IsHoliday {
			nonTrading = append(nonTrading, date)
			continue
		}
		trading = append(trading, date)
	}
	return nonTrading, trading
}

func formatFlatFileDateList(dates []time.Time) string {
	parts := make([]string, 0, len(dates))
	for _, date := range normalizeUniqueUTCDates(dates) {
		parts = append(parts, date.Format("2006-01-02"))
	}
	return strings.Join(parts, ", ")
}

func normalizeUniqueUTCDates(values []time.Time) []time.Time {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]time.Time, len(values))
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		normalized := normalizeUTCDay(value)
		seen[normalized.Format("2006-01-02")] = normalized
	}
	out := make([]time.Time, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

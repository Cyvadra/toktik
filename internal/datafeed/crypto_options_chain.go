package datafeed

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// CryptoOptionsChainProvider implements backtest.OptionsChainProvider by
// pre-loading all option bars for a base asset from ClickHouse, then
// serving per-timestamp lookups during bar replay.
type CryptoOptionsChainProvider struct {
	// byTimestamp maps truncated timestamps to the contracts available at that time.
	byTimestamp map[int64][]backtest.OptionContract
	// resolution is the bar duration used for timestamp bucketing.
	resolution time.Duration
}

const cryptoDailyChainInterval = "1d"
const cryptoOptionsBarChunkWindow = 31 * 24 * time.Hour

// symbolMetaRecord holds the immutable metadata fields for a single option contract.
type symbolMetaRecord struct {
	symbol     string
	optionType string
	strike     float32
	expiration time.Time
}

// loadSymbolMeta fetches option contract metadata for baseAsset using GROUP BY
// + anyLast() instead of a FINAL join. FINAL forces ClickHouse to synchronously
// merge all table parts before returning, which is very expensive for large
// tables. GROUP BY + anyLast() achieves the same deduplication semantics (last
// written value wins) without triggering a full merge.
func loadSymbolMeta(ctx context.Context, conn driver.Conn, baseAsset string) (map[uint32]symbolMetaRecord, error) {
	rows, err := conn.Query(ctx, `
SELECT
    symbol_id,
    anyLast(symbol)       AS symbol,
    anyLast(option_type)  AS option_type,
    anyLast(strike_price) AS strike_price,
    anyLast(expiration)   AS expiration
FROM crypto_options_symbol_meta
WHERE base_asset = {base_asset:String}
GROUP BY symbol_id`,
		clickhouse.Named("base_asset", baseAsset),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meta := make(map[uint32]symbolMetaRecord)
	for rows.Next() {
		var id uint32
		var r symbolMetaRecord
		if err := rows.Scan(&id, &r.symbol, &r.optionType, &r.strike, &r.expiration); err != nil {
			return nil, fmt.Errorf("scan symbol meta: %w", err)
		}
		meta[id] = r
	}
	return meta, rows.Err()
}

// NewCryptoOptionsChainProvider loads all option data for the given base asset
// and interval from ClickHouse. The data is indexed by timestamp for O(1)
// lookups during replay.
func NewCryptoOptionsChainProvider(ctx context.Context, conn driver.Conn, baseAsset, interval string, from, to time.Time) (*CryptoOptionsChainProvider, error) {
	resolution, err := parseInterval(interval)
	if err != nil {
		return nil, err
	}

	cacheInterval := resolveCryptoChainCacheInterval(interval)
	cacheResolution := resolution
	if cacheInterval != "" {
		cacheResolution, err = parseInterval(cacheInterval)
		if err != nil {
			return nil, fmt.Errorf("parse chain cache interval %s: %w", cacheInterval, err)
		}
	}

	// Pre-load symbol metadata separately so the main query can avoid the
	// expensive LEFT JOIN … FINAL pattern (see loadSymbolMeta).
	metaMap, err := loadSymbolMeta(ctx, conn, baseAsset)
	if err != nil {
		return nil, fmt.Errorf("load symbol metadata for %s: %w", baseAsset, err)
	}

	fromParam := backtestTimeParam(from)
	toParam := backtestTimeParam(to)

	underlyingCloseExprBars := "toFloat32(0)"
	underlyingCloseExprCache := "toFloat32(0)"
	joinClauseBars := ""
	joinClauseCache := ""

	spotSourceSQL, degraded, err := buildSpotSourceSQLWithFallback(ctx, conn, interval, baseAsset, from, to)
	if err != nil {
		return nil, fmt.Errorf("resolve spot source for options chain: %w", err)
	}
	if spotSourceSQL != "" {
		joinClauseBars = fmt.Sprintf("LEFT JOIN (%s) AS u\n    ON u.symbol = b.base_asset AND u.timestamp = b.timestamp", spotSourceSQL)
		underlyingCloseExprBars = "ifNull(u.close, toFloat32(0))"
		if degraded {
			log.Printf("[compat] options chain for %s/%s is using slower spot aggregation fallback", baseAsset, interval)
		}
	} else if legacyExpr, ok, err := legacyUnderlyingCloseExpr(ctx, conn, interval); err != nil {
		return nil, fmt.Errorf("resolve legacy underlying source for options chain: %w", err)
	} else if ok {
		underlyingCloseExprBars = legacyExpr
	} else {
		log.Printf("[compat] no spot source or legacy underlying columns found for %s/%s; using zero underlying prices in options chain", baseAsset, interval)
	}

	if cacheInterval != "" {
		spotSourceSQLCache, degradedCache, err := buildSpotSourceSQLWithFallback(ctx, conn, cacheInterval, baseAsset, from, to)
		if err != nil {
			return nil, fmt.Errorf("resolve cache spot source for options chain: %w", err)
		}
		if spotSourceSQLCache != "" {
			joinClauseCache = fmt.Sprintf("LEFT JOIN (%s) AS u\n    ON u.symbol = c.base_asset AND u.timestamp = c.timestamp", spotSourceSQLCache)
			underlyingCloseExprCache = "ifNull(u.close, toFloat32(0))"
			if degradedCache {
				log.Printf("[compat] options chain cache for %s/%s is using slower spot aggregation fallback", baseAsset, cacheInterval)
			}
		} else if legacyExpr, ok, err := legacyUnderlyingCloseExpr(ctx, conn, cacheInterval); err != nil {
			return nil, fmt.Errorf("resolve cache legacy underlying source for options chain: %w", err)
		} else if ok {
			underlyingCloseExprCache = legacyExpr
		}
	}

	numTimestamps := int(to.Sub(from)/resolution) + 1
	if numTimestamps < 64 {
		numTimestamps = 64
	}
	if chainView, ok := cryptooptions.ChainPrecomputedIntervals[cacheInterval]; ok {
		exists, err := tableExists(ctx, conn, chainView)
		if err != nil {
			return nil, fmt.Errorf("check chain cache table %s: %w", chainView, err)
		}
		if exists {
			if !shouldUseCachedChainSnapshots(resolution, cacheResolution) {
				symbolIDs, err := loadCandidateSymbolIDsFromCache(ctx, conn, chainView, baseAsset, fromParam, toParam)
				if err != nil {
					return nil, err
				}
				if len(symbolIDs) > 0 {
					byTimestamp, missingMetaCount, err := loadOptionsChainFromBarsForSymbols(ctx, conn, baseAsset, interval, from, to, underlyingCloseExprBars, joinClauseBars, resolution, metaMap, numTimestamps, symbolIDs)
					if err != nil {
						return nil, err
					}
					if missingMetaCount > 0 {
						log.Printf("[chain] %d bars skipped due to missing symbol metadata for %s", missingMetaCount, baseAsset)
					}
					log.Printf("[chain] using %s cache to discover %d candidate contracts, then loading %s bar snapshots for %s", cacheInterval, len(symbolIDs), interval, baseAsset)
					return &CryptoOptionsChainProvider{
						byTimestamp: byTimestamp,
						resolution:  resolution,
					}, nil
				}
				log.Printf("[chain] cache table %s has no candidate symbols for %s/%s [%s,%s), falling back to unrestricted bar scan",
					chainView, baseAsset, cacheInterval, fromParam, toParam)
			}
			byTimestamp, rowCount, missingMetaCount, err := loadOptionsChainFromCache(ctx, conn, chainView, baseAsset, from, to, fromParam, toParam, underlyingCloseExprCache, joinClauseCache, resolution, cacheResolution, metaMap, numTimestamps)
			if err != nil {
				return nil, err
			}
			if missingMetaCount > 0 {
				log.Printf("[chain] %d cached contracts skipped due to missing symbol metadata for %s", missingMetaCount, baseAsset)
			}
			if rowCount > 0 {
				return &CryptoOptionsChainProvider{
					byTimestamp: byTimestamp,
					resolution:  resolution,
				}, nil
			}
			log.Printf("[chain] cache table %s has no rows for %s/%s [%s,%s), falling back to bar scan",
				chainView, baseAsset, cacheInterval, fromParam, toParam)
		}
	}

	byTimestamp, missingMetaCount, err := loadOptionsChainFromBars(ctx, conn, baseAsset, interval, from, to, underlyingCloseExprBars, joinClauseBars, resolution, metaMap, numTimestamps)
	if err != nil {
		return nil, err
	}
	if missingMetaCount > 0 {
		log.Printf("[chain] %d bars skipped due to missing symbol metadata for %s", missingMetaCount, baseAsset)
	}

	return &CryptoOptionsChainProvider{
		byTimestamp: byTimestamp,
		resolution:  resolution,
	}, nil
}

func shouldUseCachedChainSnapshots(resolution, cacheResolution time.Duration) bool {
	if cacheResolution <= 0 {
		return true
	}
	return resolution >= cacheResolution
}

func loadCandidateSymbolIDsFromCache(ctx context.Context, conn driver.Conn, chainView, baseAsset, fromParam, toParam string) ([]uint32, error) {
	query := fmt.Sprintf(`SELECT
    c.symbol_ids
FROM %s AS c
WHERE c.base_asset = {base_asset:String}
    AND c.timestamp >= toDateTime({from:String}, 'UTC')
    AND c.timestamp < toDateTime({to:String}, 'UTC')`, chainView)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("from", fromParam),
		clickhouse.Named("to", toParam),
	)
	if err != nil {
		return nil, fmt.Errorf("load candidate symbols from chain cache for %s: %w", baseAsset, err)
	}
	defer rows.Close()

	seen := make(map[uint32]struct{})
	for rows.Next() {
		var symbolIDs []uint32
		if err := rows.Scan(&symbolIDs); err != nil {
			return nil, fmt.Errorf("scan candidate symbol row: %w", err)
		}
		for _, symbolID := range symbolIDs {
			seen[symbolID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidate symbol rows for %s: %w", baseAsset, err)
	}

	if len(seen) == 0 {
		return nil, nil
	}
	ids := make([]uint32, 0, len(seen))
	for symbolID := range seen {
		ids = append(ids, symbolID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func loadOptionsChainFromCache(
	ctx context.Context,
	conn driver.Conn,
	chainView, baseAsset string,
	from, to time.Time,
	fromParam, toParam, underlyingCloseExpr, joinClause string,
	resolution, cacheResolution time.Duration,
	metaMap map[uint32]symbolMetaRecord,
	numTimestamps int,
) (map[int64][]backtest.OptionContract, uint64, uint64, error) {
	query := fmt.Sprintf(`SELECT
    c.timestamp,
    c.symbol_ids,
    c.deltas,
    c.gammas,
    c.vegas,
    c.thetas,
    c.rhos,
    c.bid_prices,
    c.ask_prices,
    c.mark_prices,
    c.mark_ivs,
    c.volumes,
    c.open_interests,
    %s AS underlying_close
FROM %s AS c
%s
WHERE c.base_asset = {base_asset:String}
    AND c.timestamp >= toDateTime({from:String}, 'UTC')
    AND c.timestamp < toDateTime({to:String}, 'UTC')`,
		underlyingCloseExpr,
		chainView,
		joinClause,
	)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", fromParam),
		clickhouse.Named("to", toParam),
	)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("load options chain cache for %s: %w", baseAsset, err)
	}
	defer rows.Close()

	byTimestamp := make(map[int64][]backtest.OptionContract, numTimestamps)
	var rowCount uint64
	var missingMetaCount uint64

	for rows.Next() {
		var (
			ts              time.Time
			symbolIDs       []uint32
			deltas          []float32
			gammas          []float32
			vegas           []float32
			thetas          []float32
			rhos            []float32
			bidPrices       []float32
			askPrices       []float32
			markPrices      []float32
			markIVs         []float32
			volumes         []uint64
			openInterests   []float32
			underlyingClose float32
		)

		if err := rows.Scan(
			&ts,
			&symbolIDs,
			&deltas,
			&gammas,
			&vegas,
			&thetas,
			&rhos,
			&bidPrices,
			&askPrices,
			&markPrices,
			&markIVs,
			&volumes,
			&openInterests,
			&underlyingClose,
		); err != nil {
			return nil, 0, 0, fmt.Errorf("scan cached chain row: %w", err)
		}

		seriesLen := len(symbolIDs)
		if seriesLen == 0 || len(deltas) != seriesLen || len(gammas) != seriesLen || len(vegas) != seriesLen ||
			len(thetas) != seriesLen || len(rhos) != seriesLen || len(bidPrices) != seriesLen || len(askPrices) != seriesLen ||
			len(markPrices) != seriesLen || len(markIVs) != seriesLen || len(volumes) != seriesLen || len(openInterests) != seriesLen {
			return nil, 0, 0, fmt.Errorf("invalid cached chain row at %s: array lengths mismatch", ts.UTC().Format(time.RFC3339))
		}

		contracts := make([]backtest.OptionContract, 0, seriesLen)
		for i := 0; i < seriesLen; i++ {
			meta, ok := metaMap[symbolIDs[i]]
			if !ok {
				missingMetaCount++
				continue
			}
			contracts = append(contracts, buildOptionContract(meta, deltas[i], gammas[i], vegas[i], thetas[i], rhos[i], bidPrices[i], askPrices[i], markPrices[i], markIVs[i], underlyingClose, volumes[i], openInterests[i]))
		}
		expandCachedChainContracts(byTimestamp, contracts, ts, from, to, resolution, cacheResolution)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("iterate cached chain rows for %s: %w", baseAsset, err)
	}

	return byTimestamp, rowCount, missingMetaCount, nil
}

func resolveCryptoChainCacheInterval(requestedInterval string) string {
	if _, ok := cryptooptions.ChainPrecomputedIntervals[requestedInterval]; ok {
		return requestedInterval
	}
	if _, ok := cryptooptions.ChainPrecomputedIntervals[cryptoDailyChainInterval]; ok {
		return cryptoDailyChainInterval
	}
	return ""
}

func expandCachedChainContracts(byTimestamp map[int64][]backtest.OptionContract, contracts []backtest.OptionContract, ts, from, to time.Time, resolution, cacheResolution time.Duration) {
	if len(contracts) == 0 {
		return
	}

	cacheTS := ts.UTC()
	if cacheResolution <= 0 || resolution >= cacheResolution {
		byTimestamp[cacheTS.Truncate(resolution).Unix()] = contracts
		return
	}

	windowStart := cacheTS.Truncate(cacheResolution)
	windowEnd := windowStart.Add(cacheResolution)
	if !from.IsZero() && windowStart.Before(from.UTC()) {
		windowStart = from.UTC().Truncate(resolution)
	}
	if !to.IsZero() && windowEnd.After(to.UTC()) {
		windowEnd = to.UTC()
	}

	for bucket := windowStart; bucket.Before(windowEnd); bucket = bucket.Add(resolution) {
		byTimestamp[bucket.UTC().Unix()] = contracts
	}
}

func loadOptionsChainFromBars(
	ctx context.Context,
	conn driver.Conn,
	baseAsset, interval string,
	from, to time.Time,
	underlyingCloseExpr, joinClause string,
	resolution time.Duration,
	metaMap map[uint32]symbolMetaRecord,
	numTimestamps int,
) (map[int64][]backtest.OptionContract, uint64, error) {
	return loadOptionsChainFromBarsForSymbols(ctx, conn, baseAsset, interval, from, to, underlyingCloseExpr, joinClause, resolution, metaMap, numTimestamps, nil)
}

func loadOptionsChainFromBarsForSymbols(
	ctx context.Context,
	conn driver.Conn,
	baseAsset, interval string,
	from, to time.Time,
	underlyingCloseExpr, joinClause string,
	resolution time.Duration,
	metaMap map[uint32]symbolMetaRecord,
	numTimestamps int,
	symbolIDs []uint32,
) (map[int64][]backtest.OptionContract, uint64, error) {
	optionTableName := resolveOptionTableName(interval)
	symbolFilter := ""
	if len(symbolIDs) > 0 {
		symbolFilter = "\n    AND has({symbol_ids:Array(UInt32)}, b.symbol_id)"
	}
	query := fmt.Sprintf(`SELECT
    b.timestamp,
    b.symbol_id,
    b.delta,
    b.gamma,
    b.vega,
    b.theta,
    b.rho,
    b.bid_close,
    b.ask_close,
    b.mark_close,
    b.mark_iv_close,
    %s AS underlying_close,
    toUInt64(b.tick_count) AS tick_count,
    b.open_interest
FROM %s AS b
%s
WHERE b.base_asset = {base_asset:String}
    AND b.timestamp >= toDateTime({from:String}, 'UTC')
    AND b.timestamp < toDateTime({to:String}, 'UTC')%s`,
		underlyingCloseExpr,
		optionTableName,
		joinClause,
		symbolFilter,
	)

	byTimestamp := make(map[int64][]backtest.OptionContract, numTimestamps)
	var missingMetaCount uint64
	for chunkStart := from.UTC(); chunkStart.Before(to.UTC()); {
		chunkEnd := minTime(chunkStart.Add(cryptoOptionsBarChunkWindow), to.UTC())

		chunkMissing, err := loadOptionsChainChunk(ctx, conn, query, baseAsset, chunkStart, chunkEnd, symbolIDs, resolution, metaMap, byTimestamp)
		missingMetaCount += chunkMissing
		if err != nil {
			return nil, 0, err
		}
		chunkStart = chunkEnd
	}

	return byTimestamp, missingMetaCount, nil
}

func loadOptionsChainChunk(
	ctx context.Context,
	conn driver.Conn,
	query, baseAsset string,
	chunkStart, chunkEnd time.Time,
	symbolIDs []uint32,
	resolution time.Duration,
	metaMap map[uint32]symbolMetaRecord,
	byTimestamp map[int64][]backtest.OptionContract,
) (uint64, error) {
	queryArgs := []any{
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", backtestTimeParam(chunkStart)),
		clickhouse.Named("to", backtestTimeParam(chunkEnd)),
	}
	if len(symbolIDs) > 0 {
		queryArgs = append(queryArgs, clickhouse.Named("symbol_ids", symbolIDs))
	}

	rows, err := conn.Query(ctx, query, queryArgs...)
	if err != nil {
		return 0, fmt.Errorf("load options chain for %s [%s,%s): %w", baseAsset, backtestTimeParam(chunkStart), backtestTimeParam(chunkEnd), err)
	}
	defer rows.Close()

	var missingMetaCount uint64
	for rows.Next() {
		var (
			ts              time.Time
			symbolID        uint32
			delta           float32
			gamma           float32
			vega            float32
			theta           float32
			rho             float32
			bidClose        float32
			askClose        float32
			markClose       float32
			markIVClose     float32
			underlyingClose float32
			tickCount       uint64
			openInterest    float32
		)

		if err := rows.Scan(
			&ts, &symbolID,
			&delta, &gamma, &vega, &theta, &rho,
			&bidClose, &askClose, &markClose, &markIVClose,
			&underlyingClose, &tickCount, &openInterest,
		); err != nil {
			return 0, fmt.Errorf("scan chain row: %w", err)
		}

		meta, ok := metaMap[symbolID]
		if !ok {
			missingMetaCount++
			continue
		}

		key := ts.UTC().Truncate(resolution).Unix()
		byTimestamp[key] = append(byTimestamp[key], buildOptionContract(meta, delta, gamma, vega, theta, rho, bidClose, askClose, markClose, markIVClose, underlyingClose, tickCount, openInterest))
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate chain rows for %s [%s,%s): %w", baseAsset, backtestTimeParam(chunkStart), backtestTimeParam(chunkEnd), err)
	}

	return missingMetaCount, nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func buildOptionContract(meta symbolMetaRecord, delta, gamma, vega, theta, rho, bidClose, askClose, markClose, markIVClose, underlyingClose float32, tickCount uint64, openInterest float32) backtest.OptionContract {
	ot := backtest.Call
	if meta.optionType == "put" {
		ot = backtest.Put
	}

	return backtest.OptionContract{
		Symbol:          meta.symbol,
		Ref:             backtest.SecurityRef{Market: "crypto-options", Symbol: meta.symbol},
		Type:            ot,
		StrikePrice:     float64(meta.strike),
		Expiration:      meta.expiration,
		Delta:           float64(delta),
		Gamma:           float64(gamma),
		Vega:            float64(vega),
		Theta:           float64(theta),
		Rho:             float64(rho),
		BidPrice:        float64(bidClose),
		AskPrice:        float64(askClose),
		MarkPrice:       float64(markClose),
		IV:              float64(markIVClose),
		UnderlyingPrice: float64(underlyingClose),
		Volume:          float64(tickCount),
		OpenInterest:    float64(openInterest),
	}
}

// AvailableContracts returns all option contracts at the given time.
func (p *CryptoOptionsChainProvider) AvailableContracts(t time.Time) []backtest.OptionContract {
	key := t.UTC().Truncate(p.resolution).Unix()
	return p.byTimestamp[key]
}

// parseInterval converts an interval string to a time.Duration.
func parseInterval(interval string) (time.Duration, error) {
	mapping := map[string]time.Duration{
		"1m":  time.Minute,
		"5m":  5 * time.Minute,
		"15m": 15 * time.Minute,
		"30m": 30 * time.Minute,
		"1h":  time.Hour,
		"2h":  2 * time.Hour,
		"3h":  3 * time.Hour,
		"4h":  4 * time.Hour,
		"6h":  6 * time.Hour,
		"8h":  8 * time.Hour,
		"12h": 12 * time.Hour,
		"1d":  24 * time.Hour,
	}
	if d, ok := mapping[interval]; ok {
		return d, nil
	}
	return 0, fmt.Errorf("unsupported interval for chain provider: %q", interval)
}

// SortContractsBySpread sorts contracts by bid-ask spread ratio (best first),
// breaking ties by volume.
func SortContractsBySpread(contracts []backtest.OptionContract) {
	sort.Slice(contracts, func(i, j int) bool {
		ri, rj := contracts[i].SpreadRatio(), contracts[j].SpreadRatio()
		if ri != rj {
			return ri < rj
		}
		return contracts[i].Volume > contracts[j].Volume
	})
}

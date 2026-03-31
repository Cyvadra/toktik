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

	// Pre-load symbol metadata separately so the main query can avoid the
	// expensive LEFT JOIN … FINAL pattern (see loadSymbolMeta).
	metaMap, err := loadSymbolMeta(ctx, conn, baseAsset)
	if err != nil {
		return nil, fmt.Errorf("load symbol metadata for %s: %w", baseAsset, err)
	}

	fromParam := backtestTimeParam(from)
	toParam := backtestTimeParam(to)

	optionTableName := resolveOptionTableName(interval)
	underlyingCloseExpr := "toFloat32(0)"
	joinClause := ""

	spotSourceSQL, degraded, err := buildSpotSourceSQLWithFallback(ctx, conn, interval, baseAsset, from, to)
	if err != nil {
		return nil, fmt.Errorf("resolve spot source for options chain: %w", err)
	}
	if spotSourceSQL != "" {
		joinClause = fmt.Sprintf("LEFT JOIN (%s) AS u\n    ON u.symbol = b.base_asset AND u.timestamp = b.timestamp", spotSourceSQL)
		underlyingCloseExpr = "ifNull(u.close, toFloat32(0))"
		if degraded {
			log.Printf("[compat] options chain for %s/%s is using slower spot aggregation fallback", baseAsset, interval)
		}
	} else if legacyExpr, ok, err := legacyUnderlyingCloseExpr(ctx, conn, interval); err != nil {
		return nil, fmt.Errorf("resolve legacy underlying source for options chain: %w", err)
	} else if ok {
		underlyingCloseExpr = legacyExpr
	} else {
		log.Printf("[compat] no spot source or legacy underlying columns found for %s/%s; using zero underlying prices in options chain", baseAsset, interval)
	}

	// Meta columns and the FINAL join are removed; metadata is looked up from
	// metaMap in Go. ORDER BY is removed because results go into an unordered
	// map. bid_open, ask_open, and mark_open are dropped as they are unused.
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
    b.tick_count,
    b.open_interest
FROM %s AS b
%s
WHERE b.base_asset = {base_asset:String}
    AND b.timestamp >= toDateTime({from:String}, 'UTC')
    AND b.timestamp < toDateTime({to:String}, 'UTC')`,
		underlyingCloseExpr,
		optionTableName,
		joinClause,
	)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", fromParam),
		clickhouse.Named("to", toParam),
	)
	if err != nil {
		return nil, fmt.Errorf("load options chain for %s: %w", baseAsset, err)
	}
	defer rows.Close()

	numTimestamps := int(to.Sub(from)/resolution) + 1
	if numTimestamps < 64 {
		numTimestamps = 64
	}
	byTimestamp := make(map[int64][]backtest.OptionContract, numTimestamps)
	missingMetaCount := 0

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
			return nil, fmt.Errorf("scan chain row: %w", err)
		}

		meta, ok := metaMap[symbolID]
		if !ok {
			missingMetaCount++
			continue
		}

		ot := backtest.Call
		if meta.optionType == "put" {
			ot = backtest.Put
		}

		contract := backtest.OptionContract{
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

		key := ts.UTC().Truncate(resolution).Unix()
		byTimestamp[key] = append(byTimestamp[key], contract)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain rows for %s: %w", baseAsset, err)
	}
	if missingMetaCount > 0 {
		log.Printf("[chain] %d bars skipped due to missing symbol metadata for %s", missingMetaCount, baseAsset)
	}

	return &CryptoOptionsChainProvider{
		byTimestamp: byTimestamp,
		resolution:  resolution,
	}, nil
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

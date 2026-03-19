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

// NewCryptoOptionsChainProvider loads all option data for the given base asset
// and interval from ClickHouse. The data is indexed by timestamp for O(1)
// lookups during replay.
func NewCryptoOptionsChainProvider(ctx context.Context, conn driver.Conn, baseAsset, interval string, from, to time.Time) (*CryptoOptionsChainProvider, error) {
	resolution, err := parseInterval(interval)
	if err != nil {
		return nil, err
	}

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

	query := fmt.Sprintf(`SELECT
    b.timestamp,
    b.symbol_id,
    m.symbol,
    m.option_type,
    m.strike_price,
    m.expiration,
    b.delta,
    b.gamma,
    b.vega,
    b.theta,
    b.rho,
    b.bid_open,
    b.bid_close,
    b.ask_open,
    b.ask_close,
    b.mark_open,
    b.mark_close,
    b.mark_iv_close,
		%s AS underlying_close,
    b.tick_count,
    b.open_interest
FROM %s AS b
LEFT JOIN crypto_options_symbol_meta FINAL AS m
    ON b.symbol_id = m.symbol_id
%s
WHERE b.base_asset = {base_asset:String}
  AND b.timestamp >= {from:DateTime}
  AND b.timestamp < {to:DateTime}
ORDER BY b.timestamp`,
		underlyingCloseExpr,
		optionTableName,
		joinClause,
	)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
	)
	if err != nil {
		return nil, fmt.Errorf("load options chain for %s: %w", baseAsset, err)
	}
	defer rows.Close()

	byTimestamp := make(map[int64][]backtest.OptionContract)
	missingMetaCount := 0

	for rows.Next() {
		var (
			ts              time.Time
			symbolID        uint32
			symbol          string
			optionType      string
			strikePrice     float32
			expiration      time.Time
			delta           float32
			gamma           float32
			vega            float32
			theta           float32
			rho             float32
			bidOpen         float32
			bidClose        float32
			askOpen         float32
			askClose        float32
			markOpen        float32
			markClose       float32
			markIVClose     float32
			underlyingClose float32
			tickCount       uint64
			openInterest    float32
		)

		if err := rows.Scan(
			&ts, &symbolID, &symbol, &optionType, &strikePrice, &expiration,
			&delta, &gamma, &vega, &theta, &rho,
			&bidOpen, &bidClose, &askOpen, &askClose,
			&markOpen, &markClose, &markIVClose,
			&underlyingClose, &tickCount, &openInterest,
		); err != nil {
			return nil, fmt.Errorf("scan chain row: %w", err)
		}

		// Skip rows where symbol metadata is missing (LEFT JOIN produced defaults).
		if symbol == "" {
			missingMetaCount++
			continue
		}

		ot := backtest.Call
		if optionType == "put" {
			ot = backtest.Put
		}

		contract := backtest.OptionContract{
			Symbol:          symbol,
			Ref:             backtest.SecurityRef{Market: "crypto-options", Symbol: symbol},
			Type:            ot,
			StrikePrice:     float64(strikePrice),
			Expiration:      expiration,
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

		key := ts.Truncate(resolution).Unix()
		byTimestamp[key] = append(byTimestamp[key], contract)
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
	key := t.Truncate(p.resolution).Unix()
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

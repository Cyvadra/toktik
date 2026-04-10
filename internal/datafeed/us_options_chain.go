package datafeed

import (
	"context"
	"fmt"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

var usOptionPrecomputedIntervals = map[string]string{
	"5m":  "us_options_bar_5m",
	"15m": "us_options_bar_15m",
	"30m": "us_options_bar_30m",
	"1h":  "us_options_bar_1h",
	"2h":  "us_options_bar_2h",
	"4h":  "us_options_bar_4h",
	"1d":  "us_options_bar_1d",
}

// USOptionsChainProvider implements backtest.OptionsChainProvider for US options.
// It loads chain snapshots for an underlying symbol into memory before replay.
type USOptionsChainProvider struct {
	byTimestamp map[int64][]backtest.OptionContract
	resolution  time.Duration
}

// NewUSOptionsChainProvider loads all option contracts for the requested
// underlying and time range. It first attempts chain-cache views, and falls
// back to option bar tables if cache rows are unavailable.
func NewUSOptionsChainProvider(ctx context.Context, conn driver.Conn, underlying, interval string, from, to time.Time) (*USOptionsChainProvider, error) {
	resolution, err := parseUSInterval(interval)
	if err != nil {
		return nil, err
	}

	fromParam := backtestTimeParam(from)
	toParam := backtestTimeParam(to)
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	if underlying == "" {
		return nil, fmt.Errorf("empty underlying symbol")
	}

	numTimestamps := int(to.Sub(from)/resolution) + 1
	if numTimestamps < 64 {
		numTimestamps = 64
	}

	if chainView, ok := usmarket.ChainPrecomputedIntervals[interval]; ok {
		exists, err := tableExists(ctx, conn, chainView)
		if err != nil {
			return nil, fmt.Errorf("check US chain cache table %s: %w", chainView, err)
		}
		if exists {
			byTimestamp, rowCount, err := loadUSOptionsChainFromCache(ctx, conn, chainView, underlying, fromParam, toParam, resolution, numTimestamps)
			if err != nil {
				return nil, err
			}
			if rowCount > 0 {
				return &USOptionsChainProvider{
					byTimestamp: byTimestamp,
					resolution:  resolution,
				}, nil
			}
		}
	}

	byTimestamp, err := loadUSOptionsChainFromBars(ctx, conn, underlying, interval, fromParam, toParam, resolution, numTimestamps)
	if err != nil {
		return nil, err
	}
	return &USOptionsChainProvider{
		byTimestamp: byTimestamp,
		resolution:  resolution,
	}, nil
}

func (p *USOptionsChainProvider) AvailableContracts(t time.Time) []backtest.OptionContract {
	key := t.UTC().Truncate(p.resolution).Unix()
	return p.byTimestamp[key]
}

func parseUSInterval(interval string) (time.Duration, error) {
	switch interval {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported US options interval %q (supported: 1m,5m,15m,30m,1h,2h,4h,1d)", interval)
	}
}

func loadUSOptionsChainFromCache(
	ctx context.Context,
	conn driver.Conn,
	chainView, underlying, fromParam, toParam string,
	resolution time.Duration,
	numTimestamps int,
) (map[int64][]backtest.OptionContract, uint64, error) {
	query := fmt.Sprintf(`SELECT
    timestamp,
    symbols,
    option_types,
    expirations,
    strikes,
    close_prices,
    underlying_closes,
    implied_volatilities,
    deltas,
    gammas,
    vegas,
    thetas,
    rhos,
    volumes,
    transactions
FROM %s
WHERE underlying = {underlying:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')`, chainView)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("from", fromParam),
		clickhouse.Named("to", toParam),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("load US options chain cache for %s: %w", underlying, err)
	}
	defer rows.Close()

	byTimestamp := make(map[int64][]backtest.OptionContract, numTimestamps)
	var rowCount uint64

	for rows.Next() {
		var (
			ts          time.Time
			symbols     []string
			types       []string
			expiries    []time.Time
			strikes     []float64
			closes      []float32
			underCloses []float32
			ivs         []float32
			deltas      []float32
			gammas      []float32
			vegas       []float32
			thetas      []float32
			rhos        []float32
			volumes     []float64
			txs         []uint64
		)
		if err := rows.Scan(
			&ts, &symbols, &types, &expiries, &strikes, &closes, &underCloses, &ivs,
			&deltas, &gammas, &vegas, &thetas, &rhos, &volumes, &txs,
		); err != nil {
			return nil, 0, fmt.Errorf("scan US cached chain row: %w", err)
		}

		n := len(symbols)
		if n == 0 || len(types) != n || len(expiries) != n || len(strikes) != n || len(closes) != n ||
			len(underCloses) != n || len(ivs) != n || len(deltas) != n || len(gammas) != n ||
			len(vegas) != n || len(thetas) != n || len(rhos) != n || len(volumes) != n || len(txs) != n {
			return nil, 0, fmt.Errorf("invalid US cached chain row at %s: array lengths mismatch", ts.UTC().Format(time.RFC3339))
		}

		contracts := make([]backtest.OptionContract, 0, n)
		for i := 0; i < n; i++ {
			contracts = append(contracts, buildUSOptionContract(
				symbols[i], types[i], expiries[i], strikes[i], closes[i], underCloses[i], ivs[i],
				deltas[i], gammas[i], vegas[i], thetas[i], rhos[i], volumes[i], txs[i],
			))
		}
		key := ts.UTC().Truncate(resolution).Unix()
		byTimestamp[key] = contracts
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate US cached chain rows for %s: %w", underlying, err)
	}
	return byTimestamp, rowCount, nil
}

func loadUSOptionsChainFromBars(
	ctx context.Context,
	conn driver.Conn,
	underlying, interval, fromParam, toParam string,
	resolution time.Duration,
	numTimestamps int,
) (map[int64][]backtest.OptionContract, error) {
	tableName, err := resolveUSOptionTableName(interval)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    option_type,
    expiration,
    strike,
    close,
    underlying_close,
    implied_volatility,
    delta,
    gamma,
    vega,
    theta,
    rho,
    volume,
    transactions
FROM %s
WHERE underlying = {underlying:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')`, tableName)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("from", fromParam),
		clickhouse.Named("to", toParam),
	)
	if err != nil {
		return nil, fmt.Errorf("load US options chain for %s: %w", underlying, err)
	}
	defer rows.Close()

	byTimestamp := make(map[int64][]backtest.OptionContract, numTimestamps)
	for rows.Next() {
		var (
			ts              time.Time
			symbol          string
			optionType      string
			expiration      time.Time
			strike          float64
			close           float32
			underlyingClose float32
			iv              float32
			delta           float32
			gamma           float32
			vega            float32
			theta           float32
			rho             float32
			volume          float64
			transactions    uint64
		)
		if err := rows.Scan(
			&ts, &symbol, &optionType, &expiration, &strike, &close, &underlyingClose, &iv,
			&delta, &gamma, &vega, &theta, &rho, &volume, &transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US chain row: %w", err)
		}

		key := ts.UTC().Truncate(resolution).Unix()
		byTimestamp[key] = append(byTimestamp[key], buildUSOptionContract(
			symbol, optionType, expiration, strike, close, underlyingClose, iv,
			delta, gamma, vega, theta, rho, volume, transactions,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US chain rows for %s: %w", underlying, err)
	}
	return byTimestamp, nil
}

func resolveUSOptionTableName(interval string) (string, error) {
	if interval == "1m" {
		return "us_options_bar_1m", nil
	}
	if table, ok := usOptionPrecomputedIntervals[interval]; ok {
		return table, nil
	}
	return "", fmt.Errorf("unsupported US options interval %q (supported: 1m,5m,15m,30m,1h,2h,4h,1d)", interval)
}

func buildUSOptionContract(
	symbol, optionType string,
	expiration time.Time,
	strike float64,
	close, underlyingClose, iv, delta, gamma, vega, theta, rho float32,
	volume float64,
	transactions uint64,
) backtest.OptionContract {
	ot := backtest.Call
	if strings.EqualFold(optionType, "P") {
		ot = backtest.Put
	}
	mark := float64(close)
	return backtest.OptionContract{
		Symbol:          symbol,
		Ref:             backtest.SecurityRef{Market: "us-stock-options", Symbol: symbol},
		Type:            ot,
		StrikePrice:     strike,
		Expiration:      expiration,
		Delta:           float64(delta),
		Gamma:           float64(gamma),
		Vega:            float64(vega),
		Theta:           float64(theta),
		Rho:             float64(rho),
		BidPrice:        mark,
		AskPrice:        mark,
		MarkPrice:       mark,
		IV:              float64(iv),
		UnderlyingPrice: float64(underlyingClose),
		Volume:          volume,
		OpenInterest:    0,
	}
}

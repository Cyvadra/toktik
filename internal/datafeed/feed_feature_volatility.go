package datafeed

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
)

const featureVolatilitySnapshotDailyTable = "feature_volatility_snapshot_daily"

var featureVolatilityFields = []string{
	"iv",
	"current_iv",
	"hv10",
	"hv20",
	"hv30",
	"iv_percentile",
	"iv_rank",
	"price_observations",
	"iv_observations",
}

// FeatureVolatilityFactorFeed exposes precomputed volatility features to DSL
// strategies through request.factor("volatility", "1d", field).
type FeatureVolatilityFactorFeed struct {
	conn         driver.Conn
	lookbackDays uint16
	market       string
}

func NewFeatureVolatilityFactorFeed(conn driver.Conn) *FeatureVolatilityFactorFeed {
	return &FeatureVolatilityFactorFeed{conn: conn, lookbackDays: 252, market: "us-options"}
}

func (f *FeatureVolatilityFactorFeed) WithLookbackDays(days int) *FeatureVolatilityFactorFeed {
	if days > 0 {
		f.lookbackDays = uint16(days)
	}
	return f
}

func (f *FeatureVolatilityFactorFeed) WithMarket(market string) *FeatureVolatilityFactorFeed {
	if market = strings.TrimSpace(market); market != "" {
		f.market = market
	}
	return f
}

func (f *FeatureVolatilityFactorFeed) Load(ctx context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	if strings.TrimSpace(req.Interval) != "1d" {
		return nil, fmt.Errorf("feature volatility factor supports interval 1d, got %q", req.Interval)
	}
	underlying := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if underlying == "" {
		underlying = strings.ToUpper(strings.TrimSpace(req.PrimarySymbol))
	}
	if underlying == "" {
		return nil, fmt.Errorf("feature volatility factor requires a symbol or primary symbol")
	}
	market := strings.TrimSpace(req.Market)
	if market == "" {
		market = strings.TrimSpace(req.PrimaryMarket)
	}
	market = volatilityFeatureMarket(market, f.market)

	rows, err := f.conn.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	price_observations,
	iv_observations,
	hv10,
	hv20,
	hv30,
	current_iv,
	iv_percentile,
	iv_rank
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND lookback_days = {lookback_days:UInt16}
  AND as_of_date >= toDate({from:String})
  AND as_of_date < toDate({to:String})
ORDER BY as_of_date ASC`, featureVolatilitySnapshotDailyTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", f.lookbackDays),
		clickhouse.Named("from", req.From.UTC().Format("2006-01-02")),
		clickhouse.Named("to", req.To.UTC().Format("2006-01-02")),
	)
	if err != nil {
		return nil, fmt.Errorf("query feature volatility %s/%s: %w", market, underlying, err)
	}
	defer rows.Close()

	timestamps := make([]time.Time, 0)
	priceObservations := make([]float64, 0)
	ivObservations := make([]float64, 0)
	hv10 := make([]float64, 0)
	hv20 := make([]float64, 0)
	hv30 := make([]float64, 0)
	currentIV := make([]float64, 0)
	ivPercentile := make([]float64, 0)
	ivRank := make([]float64, 0)

	for rows.Next() {
		var asOf time.Time
		var priceObs, ivObs uint32
		var hv10Value, hv20Value, hv30Value, currentIVValue, ivPercentileValue, ivRankValue *float64
		if err := rows.Scan(&asOf, &priceObs, &ivObs, &hv10Value, &hv20Value, &hv30Value, &currentIVValue, &ivPercentileValue, &ivRankValue); err != nil {
			return nil, fmt.Errorf("scan feature volatility row: %w", err)
		}
		timestamps = append(timestamps, asOf.UTC())
		priceObservations = append(priceObservations, float64(priceObs))
		ivObservations = append(ivObservations, float64(ivObs))
		hv10 = append(hv10, nullableFloat64(hv10Value))
		hv20 = append(hv20, nullableFloat64(hv20Value))
		hv30 = append(hv30, nullableFloat64(hv30Value))
		currentIV = append(currentIV, nullableFloat64(currentIVValue))
		ivPercentile = append(ivPercentile, nullableFloat64(ivPercentileValue))
		ivRank = append(ivRank, nullableFloat64(ivRankValue))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature volatility rows: %w", err)
	}

	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)
	ds.AddColumn("price_observations", priceObservations)
	ds.AddColumn("iv_observations", ivObservations)
	ds.AddColumn("hv10", hv10)
	ds.AddColumn("hv20", hv20)
	ds.AddColumn("hv30", hv30)
	ds.AddColumn("current_iv", currentIV)
	ds.AddColumn("iv", currentIV)
	ds.AddColumn("iv_percentile", ivPercentile)
	ds.AddColumn("iv_rank", ivRank)
	return ds, nil
}

func (f *FeatureVolatilityFactorFeed) Fields() []string {
	return append([]string(nil), featureVolatilityFields...)
}

func volatilityFeatureMarket(market, fallback string) string {
	switch strings.TrimSpace(market) {
	case "us", "us-stocks", "us-stock", "us-underlying", "stocks":
		return "us-options"
	case "crypto", "crypto-spot", "crypto-underlying", "crypto-options":
		return "crypto-options"
	case "":
		return fallback
	default:
		return market
	}
}

func nullableFloat64(value *float64) float64 {
	if value == nil {
		return math.NaN()
	}
	return *value
}

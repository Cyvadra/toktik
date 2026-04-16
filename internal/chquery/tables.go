package chquery

// ----- Base (1m) table names -----

const (
	CryptoOptionsBar1m      = "crypto_options_bar_1m"
	CryptoSpotBar1m         = "crypto_spot_bar_1m"
	CryptoOptionsSymbolMeta = "crypto_options_symbol_meta"
	USOptionsBar1m          = "us_options_bar_1m"
	USOptionsChain1d        = "us_options_chain_1d"
	USStocksBar1m           = "us_stocks_bar_1m"
	USEquitySessions        = "us_equity_sessions"
)

// ----- Feature store table names -----

const (
	FeatureVolatilitySnapshotDaily    = "feature_volatility_snapshot_daily"
	FeatureTermStructureSnapshotDaily = "feature_term_structure_snapshot_daily"
	FeatureSkewSnapshotDaily          = "feature_skew_snapshot_daily"
	FeatureLiquiditySnapshotDaily     = "feature_liquidity_snapshot_daily"
	FeatureDailyPanelDaily            = "feature_daily_panel_daily"
)

// ----- Interval → table maps -----

// CryptoOptionsIntervals maps interval suffixes to precomputed option bar view names.
var CryptoOptionsIntervals = map[string]string{
	"5m":  "crypto_options_bar_5m",
	"15m": "crypto_options_bar_15m",
	"30m": "crypto_options_bar_30m",
	"1h":  "crypto_options_bar_1h",
	"2h":  "crypto_options_bar_2h",
	"3h":  "crypto_options_bar_3h",
	"4h":  "crypto_options_bar_4h",
	"6h":  "crypto_options_bar_6h",
	"8h":  "crypto_options_bar_8h",
	"12h": "crypto_options_bar_12h",
	"1d":  "crypto_options_bar_1d",
}

// CryptoSpotIntervals maps interval suffixes to precomputed spot bar view names.
var CryptoSpotIntervals = map[string]string{
	"1m":  CryptoSpotBar1m,
	"5m":  "crypto_spot_bar_5m",
	"15m": "crypto_spot_bar_15m",
	"30m": "crypto_spot_bar_30m",
	"1h":  "crypto_spot_bar_1h",
	"2h":  "crypto_spot_bar_2h",
	"3h":  "crypto_spot_bar_3h",
	"4h":  "crypto_spot_bar_4h",
	"6h":  "crypto_spot_bar_6h",
	"8h":  "crypto_spot_bar_8h",
	"12h": "crypto_spot_bar_12h",
	"1d":  "crypto_spot_bar_1d",
}

// USOptionIntervals maps interval suffixes to precomputed US option bar table names.
var USOptionIntervals = map[string]string{
	"1m":  USOptionsBar1m,
	"5m":  "us_options_bar_5m",
	"15m": "us_options_bar_15m",
	"30m": "us_options_bar_30m",
	"1h":  "us_options_bar_1h",
	"2h":  "us_options_bar_2h",
	"4h":  "us_options_bar_4h",
	"1d":  "us_options_bar_1d",
}

// USStockIntervals maps interval suffixes to precomputed US stock bar table names.
var USStockIntervals = map[string]string{
	"1m":  USStocksBar1m,
	"5m":  "us_stocks_bar_5m",
	"15m": "us_stocks_bar_15m",
	"30m": "us_stocks_bar_30m",
	"1h":  "us_stocks_bar_1h",
	"2h":  "us_stocks_bar_2h",
	"4h":  "us_stocks_bar_4h",
	"1d":  "us_stocks_bar_1d",
}

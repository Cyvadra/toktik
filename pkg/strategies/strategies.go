package strategies

import (
	"encoding/json"

	"github.com/Cyvadra/toktik/internal/backtest"
	_ "github.com/Cyvadra/toktik/pkg/strategies/buy_flash_low"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
	_ "github.com/Cyvadra/toktik/pkg/strategies/delta_filter"
	_ "github.com/Cyvadra/toktik/pkg/strategies/ema_atr_spot"
	_ "github.com/Cyvadra/toktik/pkg/strategies/golden_cross"
	_ "github.com/Cyvadra/toktik/pkg/strategies/ma_deviation_forum_short_put"
	_ "github.com/Cyvadra/toktik/pkg/strategies/ma_deviation_spread"
	_ "github.com/Cyvadra/toktik/pkg/strategies/sf31_long"
	_ "github.com/Cyvadra/toktik/pkg/strategies/sf31_short"
	_ "github.com/Cyvadra/toktik/pkg/strategies/turtle_trend_simp"
)

// Re-export catalog types for existing callers.
type Config = catalog.Config
type TradeDirection = catalog.TradeDirection

const (
	DirectionBoth      = catalog.DirectionBoth
	DirectionLongOnly  = catalog.DirectionLongOnly
	DirectionShortOnly = catalog.DirectionShortOnly
)

func DefaultConfig() Config {
	return catalog.DefaultConfig()
}

func ConfigFromJSON(raw json.RawMessage) (Config, error) {
	return catalog.ConfigFromJSON(raw)
}

func ParseOptionPriceMode(value string) (backtest.OptionPriceMode, error) {
	return catalog.ParseOptionPriceMode(value)
}

func Build(strategyName string, params json.RawMessage) (backtest.Strategy, error) {
	return catalog.Build(strategyName, params)
}

func Resolve(request string, cfg Config) ([]backtest.Strategy, error) {
	return catalog.Resolve(request, cfg)
}

func Available() []string {
	return catalog.Available()
}

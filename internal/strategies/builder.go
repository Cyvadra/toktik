package strategies

import (
	"encoding/json"
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// Build returns a configured backtest strategy from the strategy name and
// optional JSON parameters blob.
func Build(strategyName string, params json.RawMessage) (backtest.Strategy, error) {
	cfg, err := ConfigFromJSON(params)
	if err != nil {
		return nil, err
	}
	built, err := Resolve(strategyName, cfg)
	if err != nil {
		return nil, err
	}
	if len(built) != 1 {
		return nil, fmt.Errorf("strategy %q resolves to %d strategies; API Build expects exactly one", strategyName, len(built))
	}
	return built[0], nil
}

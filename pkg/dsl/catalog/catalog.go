// Package dslcatalog registers DSL-based strategies with the strategy catalog.
// Import this package (with _) to enable DSL strategy loading.
package dslcatalog

import (
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// RegisterDSL registers a DSL script as a named strategy in the catalog.
func RegisterDSL(name, source string) error {
	return catalog.TryRegister(catalog.Registration{
		Name:   name,
		Groups: []string{"dsl"},
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			ds := bridge.NewWithOptions(source, bridge.Options{SignalSource: cfg.SignalSource})
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", name, errs)
			}
			return ds, nil
		},
	})
}

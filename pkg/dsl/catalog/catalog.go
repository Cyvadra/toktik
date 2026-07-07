// Package dslcatalog registers DSL-based strategies with the strategy catalog.
// Import this package (with _) to enable DSL strategy loading.
package dslcatalog

import (
	"fmt"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/dsl/configmap"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
	dslscripts "github.com/Cyvadra/toktik/pkg/dsl/scripts"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// RegisterDSL registers a DSL script as a named strategy in the catalog.
func RegisterDSL(name, source string) error {
	return RegisterDSLWithMetadata(catalog.Registration{
		Name:   name,
		Groups: []string{"dsl"},
	}, source)
}

// RegisterDSLWithMetadata registers a DSL script with explicit catalog metadata.
func RegisterDSLWithMetadata(reg catalog.Registration, source string) error {
	if _, errs := parser.Parse(source); len(errs) > 0 {
		return fmt.Errorf("DSL parse errors in %q: %v", reg.Name, errs)
	}
	return catalog.TryRegister(catalog.Registration{
		Name:    reg.Name,
		Aliases: append([]string(nil), reg.Aliases...),
		Groups:  append([]string(nil), reg.Groups...),
		Profile: reg.Profile,
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			opts := bridge.Options{
				SignalSource:     cfg.SignalSource,
				UniverseProvider: cfg.UniverseProvider,
			}
			opts.Config = configmap.FromStrategyConfig(cfg, nil)
			ds := bridge.NewWithOptions(source, opts)
			if errs := ds.ParseErrors(); len(errs) > 0 {
				return nil, fmt.Errorf("DSL parse errors in %q: %v", reg.Name, errs)
			}
			return ds, nil
		},
	})
}

func mustRegisterDSLFile(name, fileName string) {
	if err := registerDSLFile(name, fileName); err != nil {
		panic(err)
	}
}

func mustRegisterDSLFileWithMetadata(reg catalog.Registration, fileName string) {
	if err := registerDSLFileWithMetadata(reg, fileName); err != nil {
		panic(err)
	}
}

func registerDSLFile(name, fileName string) error {
	return registerDSLFileWithMetadata(catalog.Registration{
		Name:   name,
		Groups: []string{"dsl"},
	}, fileName)
}

func registerDSLFileWithMetadata(reg catalog.Registration, fileName string) error {
	source, err := dslscripts.ReadStrategy(fileName)
	if err != nil {
		return err
	}
	return RegisterDSLWithMetadata(reg, source)
}

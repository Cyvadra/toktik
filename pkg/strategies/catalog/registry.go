package catalog

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// Registration defines how a strategy registers itself in the global registry.
type Registration struct {
	Name    string
	Aliases []string
	Groups  []string
	Factory func(Config) (backtest.Strategy, error)
}

type strategySpec struct {
	name    string
	aliases []string
	groups  map[string]struct{}
	factory func(Config) (backtest.Strategy, error)
}

var (
	registryMu        sync.RWMutex
	orderedStrategies []string
	strategiesByName  = make(map[string]*strategySpec)
	aliasToName       = make(map[string]string)
)

func init() {
	// Keep historical CLI compatibility: "both" means all spread strategies.
	aliasToName["both"] = "@group:spread"
	aliasToName["all"] = "@group:all"
}

// Register registers one strategy implementation.
// It is intended to be called from strategy files in init().
func Register(reg Registration) {
	name := normalize(reg.Name)
	if name == "" {
		panic("strategies.Register: empty strategy name")
	}
	if reg.Factory == nil {
		panic(fmt.Sprintf("strategies.Register: nil factory for %q", reg.Name))
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := strategiesByName[name]; exists {
		panic(fmt.Sprintf("strategies.Register: duplicate strategy name %q", name))
	}

	groups := make(map[string]struct{}, len(reg.Groups)+1)
	groups["all"] = struct{}{}
	for _, group := range reg.Groups {
		groupName := normalize(group)
		if groupName == "" {
			continue
		}
		groups[groupName] = struct{}{}
	}

	spec := &strategySpec{
		name:    name,
		aliases: nil,
		groups:  groups,
		factory: reg.Factory,
	}
	strategiesByName[name] = spec
	orderedStrategies = append(orderedStrategies, name)

	registerAliasLocked(name, name)
	for _, alias := range reg.Aliases {
		aliasName := normalize(alias)
		if aliasName == "" {
			continue
		}
		registerAliasLocked(aliasName, name)
		spec.aliases = append(spec.aliases, aliasName)
	}
}

// Resolve returns one or more strategy instances based on a strategy name,
// alias, group name, or comma-separated list.
func Resolve(request string, cfg Config) ([]backtest.Strategy, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	parts := splitRequest(request)
	if len(parts) == 0 {
		parts = []string{defaultStrategyName}
	}

	out := make([]backtest.Strategy, 0, len(parts))
	for _, part := range parts {
		resolved, err := resolveOneLocked(part, cfg)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved...)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no strategy resolved from %q", request)
	}
	return out, nil
}

// Available returns all canonical strategy names.
func Available() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := append([]string(nil), orderedStrategies...)
	sort.Strings(names)
	return names
}

func resolveOneLocked(name string, cfg Config) ([]backtest.Strategy, error) {
	key := normalize(name)
	if key == "" {
		return nil, fmt.Errorf("empty strategy name")
	}

	mapped, ok := aliasToName[key]
	if !ok {
		return nil, fmt.Errorf("unknown strategy %q; available: %s", name, strings.Join(Available(), ", "))
	}

	if strings.HasPrefix(mapped, "@group:") {
		groupName := strings.TrimPrefix(mapped, "@group:")
		return buildGroupLocked(groupName, cfg)
	}

	spec, exists := strategiesByName[mapped]
	if !exists {
		return nil, fmt.Errorf("strategy %q is registered as alias but implementation is missing", mapped)
	}
	built, err := spec.factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("build strategy %q: %w", mapped, err)
	}
	return []backtest.Strategy{built}, nil
}

func buildGroupLocked(groupName string, cfg Config) ([]backtest.Strategy, error) {
	groupName = normalize(groupName)
	if groupName == "" {
		return nil, fmt.Errorf("empty group name")
	}

	out := make([]backtest.Strategy, 0)
	for _, name := range orderedStrategies {
		spec := strategiesByName[name]
		if spec == nil {
			continue
		}
		if _, ok := spec.groups[groupName]; !ok {
			continue
		}
		built, err := spec.factory(cfg)
		if err != nil {
			return nil, fmt.Errorf("build strategy %q: %w", name, err)
		}
		out = append(out, built)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no strategy found for group %q", groupName)
	}
	return out, nil
}

func registerAliasLocked(alias, mapped string) {
	if current, exists := aliasToName[alias]; exists && current != mapped {
		panic(fmt.Sprintf("strategies.Register: alias %q already maps to %q", alias, current))
	}
	aliasToName[alias] = mapped
}

func splitRequest(request string) []string {
	parts := strings.Split(request, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

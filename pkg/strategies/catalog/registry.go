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
	Profile StrategyProfile
	Factory func(Config) (backtest.Strategy, error)
}

type strategySpec struct {
	name    string
	aliases []string
	groups  map[string]struct{}
	profile StrategyProfile
	factory func(Config) (backtest.Strategy, error)
}

// ResolvedStrategy keeps the built strategy alongside its registration profile.
type ResolvedStrategy struct {
	CanonicalName string
	Strategy      backtest.Strategy
	Profile       StrategyProfile
	Runtime       StrategyRuntimeProfile
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
// It is intended to be called from strategy files in init() and panics on
// errors (empty name, nil factory, duplicate registration). Use TryRegister
// for an error-returning alternative.
func Register(reg Registration) {
	if err := TryRegister(reg); err != nil {
		panic(err.Error())
	}
}

// TryRegister is like Register but returns an error instead of panicking.
// This is useful for dynamic or late registration where a panic is undesirable.
func TryRegister(reg Registration) error {
	name := normalize(reg.Name)
	if name == "" {
		return fmt.Errorf("strategies.Register: empty strategy name")
	}
	if reg.Factory == nil {
		return fmt.Errorf("strategies.Register: nil factory for %q", reg.Name)
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := strategiesByName[name]; exists {
		return fmt.Errorf("strategies.Register: duplicate strategy name %q", name)
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
		profile: reg.Profile.Normalized(),
		factory: reg.Factory,
	}
	strategiesByName[name] = spec
	orderedStrategies = append(orderedStrategies, name)

	if err := tryRegisterAliasLocked(name, name); err != nil {
		return err
	}
	for _, alias := range reg.Aliases {
		aliasName := normalize(alias)
		if aliasName == "" {
			continue
		}
		if err := tryRegisterAliasLocked(aliasName, name); err != nil {
			return err
		}
		spec.aliases = append(spec.aliases, aliasName)
	}
	return nil
}

// Resolve returns one or more strategy instances based on a strategy name,
// alias, group name, or comma-separated list.
func Resolve(request string, cfg Config) ([]backtest.Strategy, error) {
	detailed, err := ResolveDetailed(request, cfg, "")
	if err != nil {
		return nil, err
	}
	out := make([]backtest.Strategy, 0, len(detailed))
	for _, item := range detailed {
		out = append(out, item.Strategy)
	}
	return out, nil
}

// ResolveDetailed returns strategy instances together with their profile data.
func ResolveDetailed(request string, cfg Config, baseAsset string) ([]ResolvedStrategy, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	parts := splitRequest(request)
	if len(parts) == 0 {
		parts = []string{defaultStrategyName}
	}

	out := make([]ResolvedStrategy, 0, len(parts))
	for _, part := range parts {
		resolved, err := resolveOneLocked(part, cfg, baseAsset)
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

// RegistrationInfo is a read-only snapshot of a registered strategy's metadata.
type RegistrationInfo struct {
	Name    string
	Aliases []string
	Groups  []string
	Profile StrategyProfile
}

// ListRegistrations returns metadata for all registered strategies.
func ListRegistrations() []RegistrationInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()

	out := make([]RegistrationInfo, 0, len(orderedStrategies))
	for _, name := range orderedStrategies {
		spec := strategiesByName[name]
		if spec == nil {
			continue
		}
		groups := make([]string, 0, len(spec.groups))
		for g := range spec.groups {
			groups = append(groups, g)
		}
		sort.Strings(groups)
		out = append(out, RegistrationInfo{
			Name:    spec.name,
			Aliases: append([]string(nil), spec.aliases...),
			Groups:  groups,
			Profile: spec.profile,
		})
	}
	return out
}

func resolveOneLocked(name string, cfg Config, baseAsset string) ([]ResolvedStrategy, error) {
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
		return buildGroupLocked(groupName, cfg, baseAsset)
	}

	spec, exists := strategiesByName[mapped]
	if !exists {
		return nil, fmt.Errorf("strategy %q is registered as alias but implementation is missing", mapped)
	}
	built, err := spec.factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("build strategy %q: %w", mapped, err)
	}
	runtime := buildRuntimeProfile(mapped, spec.profile, baseAsset)
	runtime.DisplayName = built.Name()
	return []ResolvedStrategy{{
		CanonicalName: mapped,
		Strategy:      built,
		Profile:       spec.profile,
		Runtime:       runtime,
	}}, nil
}

func buildGroupLocked(groupName string, cfg Config, baseAsset string) ([]ResolvedStrategy, error) {
	groupName = normalize(groupName)
	if groupName == "" {
		return nil, fmt.Errorf("empty group name")
	}

	out := make([]ResolvedStrategy, 0)
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
		runtime := buildRuntimeProfile(name, spec.profile, baseAsset)
		runtime.DisplayName = built.Name()
		out = append(out, ResolvedStrategy{
			CanonicalName: name,
			Strategy:      built,
			Profile:       spec.profile,
			Runtime:       runtime,
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no strategy found for group %q", groupName)
	}
	return out, nil
}

func tryRegisterAliasLocked(alias, mapped string) error {
	if current, exists := aliasToName[alias]; exists && current != mapped {
		return fmt.Errorf("strategies.Register: alias %q already maps to %q", alias, current)
	}
	aliasToName[alias] = mapped
	return nil
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

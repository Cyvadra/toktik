package service

import (
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

func validateMarketStrategyCompatibility(market marketSpec, items []strategies.ResolvedStrategy) error {
	if market.name != marketForex {
		return nil
	}
	for _, item := range items {
		if !item.Profile.UsesOptions {
			continue
		}
		name := strings.TrimSpace(item.CanonicalName)
		if name == "" {
			name = "selected strategy"
		}
		return dto.NewValidationError("market=forex currently supports spot strategies only; %s requires option-contract data", name)
	}
	return nil
}

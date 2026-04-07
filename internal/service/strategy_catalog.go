package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// StrategyCatalogService exposes the strategy registry as a read-only catalog.
type StrategyCatalogService struct{}

func NewStrategyCatalogService() *StrategyCatalogService {
	return &StrategyCatalogService{}
}

func (s *StrategyCatalogService) ListStrategies(_ context.Context, req dto.StrategyCatalogListRequest) (*dto.StrategyCatalogResponse, error) {
	entries := catalog.ListRegistrations()

	result := make([]dto.StrategyCatalogEntry, 0, len(entries))
	for _, e := range entries {
		groups := make([]string, 0, len(e.Groups))
		for _, g := range e.Groups {
			if g != "all" {
				groups = append(groups, g)
			}
		}
		sort.Strings(groups)

		if req.Group != "" {
			found := false
			for _, g := range e.Groups {
				if strings.EqualFold(g, req.Group) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		result = append(result, dto.StrategyCatalogEntry{
			Name:         e.Name,
			Aliases:      e.Aliases,
			Groups:       groups,
			UsesOptions:  e.Profile.UsesOptions,
			RegularTrade: string(e.Profile.RegularTrade),
			ProfileLabel: e.Profile.Label(),
		})
	}

	return &dto.StrategyCatalogResponse{Data: result}, nil
}

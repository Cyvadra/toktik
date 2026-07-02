package dto

import (
	"encoding/json"
	"strings"
	"time"
)

type UniverseSourceType string

const (
	UniverseSourceTurnoverIntersectionUnion UniverseSourceType = "turnover_intersection_union"
	UniverseSourcePresetSymbols             UniverseSourceType = "preset_symbols"
	UniverseSourceProviderHoldings          UniverseSourceType = "provider_holdings"
)

type UniverseMember struct {
	UniverseCode string     `json:"universe_code"`
	Market       string     `json:"market"`
	Symbol       string     `json:"symbol"`
	ValidFrom    time.Time  `json:"valid_from"`
	ValidTo      time.Time  `json:"valid_to"`
	Score        *float64   `json:"score,omitempty"`
	Rank         *uint32    `json:"rank,omitempty"`
	SourceRunID  string     `json:"source_run_id,omitempty"`
	Metadata     string     `json:"metadata,omitempty"`
	Source       string     `json:"source,omitempty"`
	IngestedAt   *time.Time `json:"ingested_at,omitempty"`
}

type UniverseMembersRequest struct {
	Market string    `form:"market" json:"market"`
	Code   string    `form:"code" json:"code"`
	AsOf   time.Time `form:"-" json:"as_of"`
	From   time.Time `form:"-" json:"from"`
	To     time.Time `form:"-" json:"to"`
	Limit  int       `form:"limit" json:"limit"`
}

type UniverseMembersResponse struct {
	Market string           `json:"market"`
	Code   string           `json:"code"`
	AsOf   *time.Time       `json:"as_of,omitempty"`
	From   time.Time        `json:"from,omitempty"`
	To     time.Time        `json:"to,omitempty"`
	Data   []UniverseMember `json:"data"`
}

type UniverseRebuildRequest struct {
	Market       string             `json:"market"`
	Code         string             `json:"code"`
	SourceType   UniverseSourceType `json:"source_type"`
	AsOf         time.Time          `json:"as_of"`
	From         time.Time          `json:"from"`
	To           time.Time          `json:"to"`
	Symbols      []string           `json:"symbols,omitempty"`
	Members      []UniverseMember   `json:"members,omitempty"`
	LookbackDays []int              `json:"lookback_days,omitempty"`
	Limit        int                `json:"limit,omitempty"`
	NonETFOnly   *bool              `json:"non_etf_only,omitempty"`
	DryRun       bool               `json:"dry_run,omitempty"`
}

func (r *UniverseRebuildRequest) UnmarshalJSON(data []byte) error {
	type rawRequest struct {
		Market       string             `json:"market"`
		Code         string             `json:"code"`
		SourceType   UniverseSourceType `json:"source_type"`
		AsOf         string             `json:"as_of"`
		From         string             `json:"from"`
		To           string             `json:"to"`
		Symbols      []string           `json:"symbols,omitempty"`
		Members      []UniverseMember   `json:"members,omitempty"`
		LookbackDays []int              `json:"lookback_days,omitempty"`
		Limit        int                `json:"limit,omitempty"`
		NonETFOnly   *bool              `json:"non_etf_only,omitempty"`
		DryRun       bool               `json:"dry_run,omitempty"`
	}
	var raw rawRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	asOf, err := ParseUniverseDate(raw.AsOf)
	if err != nil {
		return err
	}
	from, err := ParseUniverseDate(raw.From)
	if err != nil {
		return err
	}
	to, err := ParseUniverseDate(raw.To)
	if err != nil {
		return err
	}
	*r = UniverseRebuildRequest{Market: raw.Market, Code: raw.Code, SourceType: raw.SourceType, AsOf: asOf, From: from, To: to, Symbols: raw.Symbols, Members: raw.Members, LookbackDays: raw.LookbackDays, Limit: raw.Limit, NonETFOnly: raw.NonETFOnly, DryRun: raw.DryRun}
	return nil
}

func ParseUniverseDate(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

type UniverseRebuildResponse struct {
	Market       string             `json:"market"`
	Code         string             `json:"code"`
	SourceType   UniverseSourceType `json:"source_type"`
	AsOf         time.Time          `json:"as_of"`
	From         time.Time          `json:"from,omitempty"`
	To           time.Time          `json:"to,omitempty"`
	RunID        string             `json:"run_id"`
	DryRun       bool               `json:"dry_run"`
	MemberCount  int                `json:"member_count"`
	LookbackDays []int              `json:"lookback_days,omitempty"`
	Data         []UniverseMember   `json:"data,omitempty"`
}

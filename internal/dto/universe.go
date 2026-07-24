package dto

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// UniverseSourceType identifies how a named universe obtains its members.
type UniverseSourceType string

const (
	UniverseSourceTurnoverIntersectionUnion UniverseSourceType = "turnover_intersection_union"
	UniverseSourcePresetSymbols             UniverseSourceType = "preset_symbols"
	UniverseSourceProviderHoldings          UniverseSourceType = "provider_holdings"
)

// UniverseMember is a symbol's membership in a named universe over [valid_from, valid_to).
type UniverseMember struct {
	UniverseCode string     `json:"universe_code"`           // Named universe code.
	Market       string     `json:"market"`                  // Market containing the symbol.
	Symbol       string     `json:"symbol"`                  // Symbol, normalized to uppercase for static sources.
	ValidFrom    time.Time  `json:"valid_from"`              // Inclusive membership start.
	ValidTo      time.Time  `json:"valid_to"`                // Exclusive membership end.
	Score        *float64   `json:"score,omitempty"`         // Optional source score, such as combined turnover.
	Rank         *uint32    `json:"rank,omitempty"`          // Optional source rank; static members without a rank are ranked by symbol.
	SourceRunID  string     `json:"source_run_id,omitempty"` // Deterministic identifier for the rebuild result.
	Metadata     string     `json:"metadata,omitempty"`      // Optional caller-supplied JSON or text metadata.
	Source       string     `json:"source,omitempty"`        // Source type that created the membership.
	IngestedAt   *time.Time `json:"ingested_at,omitempty"`   // Storage ingestion timestamp.
}

// UniverseMembersRequest selects point-in-time or interval membership records.
type UniverseMembersRequest struct {
	Market string    `form:"market" json:"market"` // Market; defaults to us-stocks.
	Code   string    `form:"code" json:"code"`     // Named universe code.
	AsOf   time.Time `form:"-" json:"as_of"`       // Point-in-time membership date.
	From   time.Time `form:"-" json:"from"`        // Inclusive interval query start.
	To     time.Time `form:"-" json:"to"`          // Exclusive interval query end.
	Limit  int       `form:"limit" json:"limit"`   // Maximum records returned.
}

// UniverseMembersResponse contains point-in-time or interval membership records.
type UniverseMembersResponse struct {
	Market string           `json:"market"`          // Resolved market.
	Code   string           `json:"code"`            // Resolved universe code.
	AsOf   *time.Time       `json:"as_of,omitempty"` // Point-in-time date for an as_of query.
	From   time.Time        `json:"from,omitempty"`  // Inclusive interval query start.
	To     time.Time        `json:"to,omitempty"`    // Exclusive interval query end.
	Data   []UniverseMember `json:"data"`            // Matching membership records.
}

// UniverseRebuildRequest configures a named-universe rebuild. Its date range
// is owned by the server and derived from configured history plus reference data.
type UniverseRebuildRequest struct {
	Market       string             `json:"market"`                  // Market; defaults to us-stocks.
	Code         string             `json:"code"`                    // Required named universe code.
	SourceType   UniverseSourceType `json:"source_type"`             // turnover_intersection_union (default), preset_symbols, or provider_holdings.
	ForceRefresh bool               `json:"force_refresh"`           // Full rebuild when true; defaults to true for JSON requests.
	Symbols      []string           `json:"symbols,omitempty"`       // Static symbols required by preset_symbols and provider_holdings when members is empty.
	Members      []UniverseMember   `json:"members,omitempty"`       // Static members required by preset_symbols and provider_holdings when symbols is empty.
	LookbackDays []int              `json:"lookback_days,omitempty"` // Turnover lookbacks in trading days; values outside 7..252 are ignored and default is 7,20,60,120.
	Limit        int                `json:"limit,omitempty"`         // Members retained per turnover lookback; default is 60.
	NonETFOnly   *bool              `json:"non_etf_only,omitempty"`  // Turnover source ETF exclusion; default is true.
	DryRun       bool               `json:"dry_run,omitempty"`       // Calculate without persisting membership or run metadata.
}

func (r *UniverseRebuildRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, field := range []string{"as_of", "from", "to"} {
		if _, ok := fields[field]; ok {
			return fmt.Errorf("%s is not supported for universe rebuilds", field)
		}
	}
	type rawRequest struct {
		Market       string             `json:"market"`
		Code         string             `json:"code"`
		SourceType   UniverseSourceType `json:"source_type"`
		ForceRefresh *bool              `json:"force_refresh"`
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
	forceRefresh := true
	if raw.ForceRefresh != nil {
		forceRefresh = *raw.ForceRefresh
	}
	*r = UniverseRebuildRequest{Market: raw.Market, Code: raw.Code, SourceType: raw.SourceType, ForceRefresh: forceRefresh, Symbols: raw.Symbols, Members: raw.Members, LookbackDays: raw.LookbackDays, Limit: raw.Limit, NonETFOnly: raw.NonETFOnly, DryRun: raw.DryRun}
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

// UniverseRebuildResponse describes the calculated or persisted rebuild result.
type UniverseRebuildResponse struct {
	Market       string             `json:"market"`                  // Resolved market.
	Code         string             `json:"code"`                    // Resolved universe code.
	SourceType   UniverseSourceType `json:"source_type"`             // Source used to build members.
	AsOf         time.Time          `json:"as_of"`                   // Rebuild start date retained for compatibility.
	From         time.Time          `json:"from,omitempty"`          // Inclusive rebuild start.
	To           time.Time          `json:"to,omitempty"`            // Exclusive rebuild end.
	RunID        string             `json:"run_id"`                  // Deterministic identifier for this result.
	DryRun       bool               `json:"dry_run"`                 // Whether the result was calculated without persistence.
	MemberCount  int                `json:"member_count"`            // Number of compressed membership intervals returned.
	LookbackDays []int              `json:"lookback_days,omitempty"` // Effective turnover lookbacks.
	Data         []UniverseMember   `json:"data,omitempty"`          // Calculated membership intervals.
}

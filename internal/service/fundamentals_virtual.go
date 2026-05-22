package service

import (
	"context"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	virtualFundamentalFactorPE             = "pe"
	virtualFundamentalFactorPE10Live       = "pe10_live"
	virtualFundamentalSnapshotLookbackDays = 45
)

type macroSeriesProvider interface {
	QuerySeries(ctx context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error)
}

type virtualFundamentalsProvider struct {
	macro macroSeriesProvider
}

type virtualFundamentalMacroTarget struct {
	Dataset         string
	ReferenceMarket string
	ReferenceSymbol string
	MacroFactor     string
}

type fundamentalFactorSelection struct {
	base            []string
	includePE       bool
	includePE10Live bool
}

func newVirtualFundamentalsProvider(macro macroSeriesProvider) *virtualFundamentalsProvider {
	return &virtualFundamentalsProvider{macro: macro}
}

func (p *virtualFundamentalsProvider) appendCatalogEntries(entries []dto.FundamentalFactorCatalogEntry, market string) []dto.FundamentalFactorCatalogEntry {
	if market != "" && !strings.EqualFold(market, "us-stocks") {
		return entries
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Market, "us-stocks") && strings.EqualFold(entry.FactorCode, virtualFundamentalFactorPE10Live) {
			return entries
		}
	}
	return append(entries, dto.FundamentalFactorCatalogEntry{
		Market:             "us-stocks",
		FactorCode:         virtualFundamentalFactorPE10Live,
		DisplayName:        "Index Shiller PE Live",
		Description:        "Alias-aware daily index valuation series backed by monthly Shiller-style PE anchors for SPY/SPX.",
		ValueType:          "ratio",
		PreferredFrequency: "1d",
		FillPolicy:         fundamentalFillForwardFill,
		PointInTime:        true,
		Source:             macroVirtualFactorSource,
		Active:             true,
	})
}

func (p *virtualFundamentalsProvider) querySeries(ctx context.Context, req dto.FundamentalSeriesRequest, market, symbol, factor, mode string) ([]dto.FundamentalSeriesPoint, string, bool, error) {
	target, ok := resolveVirtualFundamentalMacroTarget(market, symbol, factor)
	if !ok {
		return nil, "", false, nil
	}
	interval := macroIntervalEvent
	fillPolicy := ""
	from := req.From
	to := req.To
	if mode != fundamentalSeriesModeEvent {
		interval = "1d"
		fillPolicy = fundamentalFillForwardFill
		from, to = normalizeVirtualFundamentalDailyRange(req.From, req.To)
	}
	resp, err := p.macro.QuerySeries(ctx, dto.MacroSeriesRequest{
		Dataset:         target.Dataset,
		Factors:         []string{target.MacroFactor},
		From:            from,
		To:              to,
		AsOf:            req.AsOf,
		Interval:        interval,
		ReferenceMarket: target.ReferenceMarket,
		ReferenceSymbol: target.ReferenceSymbol,
		Limit:           maxMacroSeriesLimit,
	})
	if err != nil {
		return nil, "", true, err
	}
	out := make([]dto.FundamentalSeriesPoint, 0, len(resp.Data))
	for _, point := range resp.Data {
		eventTS := point.EventTS
		if mode != fundamentalSeriesModeEvent {
			eventTS = point.Timestamp
		}
		out = append(out, dto.FundamentalSeriesPoint{
			EventTS: eventTS,
			KnownAt: point.KnownAt,
			Value:   point.Value,
			Source:  point.Source,
			Filled:  point.Filled,
		})
	}
	return out, fillPolicy, true, nil
}

func (p *virtualFundamentalsProvider) querySnapshot(ctx context.Context, market, symbol, factor string, asOf time.Time) (*dto.FundamentalSnapshotEntry, bool, error) {
	target, ok := resolveVirtualFundamentalMacroTarget(market, symbol, factor)
	if !ok {
		return nil, false, nil
	}
	resp, err := p.querySnapshotSeries(ctx, factor, target, asOf)
	if err != nil {
		return nil, true, err
	}
	if len(resp.Data) == 0 {
		return nil, true, nil
	}
	point := resp.Data[len(resp.Data)-1]
	return &dto.FundamentalSnapshotEntry{
		Factor:  factor,
		EventTS: point.Timestamp,
		KnownAt: point.KnownAt,
		Value:   point.Value,
		Source:  point.Source,
	}, true, nil
}

func (p *virtualFundamentalsProvider) queryPanelRows(ctx context.Context, market string, symbols []string, factor string, asOf time.Time) ([]dto.FundamentalPanelRow, error) {
	out := make([]dto.FundamentalPanelRow, 0, len(symbols))
	for _, symbol := range symbols {
		entry, handled, err := p.querySnapshot(ctx, market, symbol, factor, asOf)
		if err != nil {
			return nil, err
		}
		if !handled || entry == nil {
			continue
		}
		out = append(out, dto.FundamentalPanelRow{
			Symbol:  symbol,
			Factor:  entry.Factor,
			EventTS: entry.EventTS,
			KnownAt: entry.KnownAt,
			Value:   entry.Value,
		})
	}
	return out, nil
}

func (p *virtualFundamentalsProvider) querySnapshotSeries(ctx context.Context, factor string, target virtualFundamentalMacroTarget, asOf time.Time) (*dto.MacroSeriesResponse, error) {
	from := asOf.AddDate(0, 0, -virtualFundamentalSnapshotLookbackDays)
	to := asOf.AddDate(0, 0, 1)
	return p.macro.QuerySeries(ctx, dto.MacroSeriesRequest{
		Dataset:         target.Dataset,
		Factors:         []string{target.MacroFactor},
		From:            from.UTC().Format(time.RFC3339Nano),
		To:              to.UTC().Format(time.RFC3339Nano),
		AsOf:            asOf.UTC().Format(time.RFC3339Nano),
		Interval:        "1d",
		ReferenceMarket: target.ReferenceMarket,
		ReferenceSymbol: target.ReferenceSymbol,
		Limit:           maxMacroSeriesLimit,
	})
}

func resolveVirtualFundamentalMacroTarget(market, symbol, factor string) (virtualFundamentalMacroTarget, bool) {
	if !strings.EqualFold(market, "us-stocks") {
		return virtualFundamentalMacroTarget{}, false
	}
	switch strings.ToLower(strings.TrimSpace(factor)) {
	case virtualFundamentalFactorPE, virtualFundamentalFactorPE10Live:
	default:
		return virtualFundamentalMacroTarget{}, false
	}
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "SPY", "SPX":
		return virtualFundamentalMacroTarget{
			Dataset:         macroDatasetFMPSP500Shiller,
			ReferenceMarket: defaultMacroReferenceMarket,
			ReferenceSymbol: "SPY",
			MacroFactor:     factor,
		}, true
	case "QQQ", "NDX":
		if !strings.EqualFold(strings.TrimSpace(factor), virtualFundamentalFactorPE) {
			return virtualFundamentalMacroTarget{}, false
		}
		return virtualFundamentalMacroTarget{
			Dataset:         macroDatasetFMPNDXShiller,
			ReferenceMarket: defaultMacroReferenceMarket,
			ReferenceSymbol: "QQQ",
			MacroFactor:     virtualFundamentalFactorPE,
		}, true
	default:
		return virtualFundamentalMacroTarget{}, false
	}
}

func splitFundamentalFactorSelection(factors []string) fundamentalFactorSelection {
	selection := fundamentalFactorSelection{base: make([]string, 0, len(factors))}
	for _, factor := range factors {
		switch factor {
		case virtualFundamentalFactorPE:
			selection.base = append(selection.base, factor)
			selection.includePE = true
		case virtualFundamentalFactorPE10Live:
			selection.includePE10Live = true
		default:
			selection.base = append(selection.base, factor)
		}
	}
	if len(selection.base) == 0 {
		selection.base = nil
	}
	return selection
}

func normalizeVirtualFundamentalDailyRange(rawFrom, rawTo string) (string, string) {
	if isDateOnlyTimeInput(rawTo) {
		return rawFrom, rawTo
	}
	to, err := parseFundamentalTime(rawTo)
	if err != nil {
		return rawFrom, rawTo
	}
	dayEnd := time.Date(to.UTC().Year(), to.UTC().Month(), to.UTC().Day()+1, 0, 0, 0, 0, time.UTC)
	return rawFrom, dayEnd.Format(time.RFC3339Nano)
}

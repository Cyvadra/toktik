package report

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func buildMarketMixView(result *backtest.Result, unit string) (marketMixView, []securityMixRowView) {
	if result == nil {
		return marketMixView{Description: "无交易标的记录。"}, nil
	}
	type aggregate struct {
		trades   int
		notional float64
		netCash  float64
	}
	markets := make(map[string]struct{})
	securities := make(map[string]*aggregate)
	regularTrades := 0
	optionLegs := 0

	addMarket := func(market string) {
		market = strings.TrimSpace(market)
		if market != "" {
			markets[market] = struct{}{}
		}
	}
	securityKey := func(market, symbol, interval string) string {
		parts := []string{strings.TrimSpace(market), strings.TrimSpace(symbol)}
		if strings.TrimSpace(interval) != "" {
			parts = append(parts, strings.TrimSpace(interval))
		}
		return strings.Join(parts, " / ")
	}

	for _, trade := range result.Trades {
		regularTrades++
		addMarket(trade.Security.Market)
		key := securityKey(trade.Security.Market, trade.Security.Symbol, trade.Security.Interval)
		if key == " / " || strings.TrimSpace(key) == "" {
			key = "regular"
		}
		agg := securities[key]
		if agg == nil {
			agg = &aggregate{}
			securities[key] = agg
		}
		agg.trades++
		agg.notional += math.Abs(trade.Qty * trade.FillPrice)
		agg.netCash += trade.NetAmount()
	}
	for _, spread := range result.SpreadPositions {
		for _, leg := range spread.Legs {
			optionLegs++
			key := strings.TrimSpace(leg.Symbol)
			if key == "" {
				key = fmt.Sprintf("option spread #%d", spread.ID)
			}
			agg := securities[key]
			if agg == nil {
				agg = &aggregate{}
				securities[key] = agg
			}
			agg.trades++
			agg.notional += math.Abs(leg.Qty * leg.EntryPrice)
			if leg.Closed {
				agg.netCash += leg.RealizedPnL
			}
		}
	}

	keys := make([]string, 0, len(securities))
	for key := range securities {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := securities[keys[i]]
		right := securities[keys[j]]
		if left.trades != right.trades {
			return left.trades > right.trades
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 8 {
		keys = keys[:8]
	}
	rows := make([]securityMixRowView, 0, len(keys))
	for _, key := range keys {
		agg := securities[key]
		rows = append(rows, securityMixRowView{
			Security: key,
			Trades:   integer(agg.trades),
			Notional: amount4(agg.notional, unit),
			NetCash:  signedAmount(agg.netCash, unit),
		})
	}

	view := marketMixView{
		SecurityCount:       integer(len(securities)),
		MarketCount:         integer(len(markets)),
		OptionLegCount:      integer(optionLegs),
		RegularTradeCount:   integer(regularTrades),
		HasOptions:          optionLegs > 0,
		HasRegularTrades:    regularTrades > 0,
		HasMixedInstruments: optionLegs > 0 && regularTrades > 0,
	}
	switch {
	case view.HasMixedInstruments:
		view.Description = "本报告包含普通股票/现货成交与期权价差腿，价格图仅代表被声明的参考品种，收益与风险以组合权益为准。"
	case view.HasOptions:
		view.Description = "本报告主要由期权合约或价差生命周期构成，价格图用于显示所选底层参考品种。"
	case view.HasRegularTrades:
		view.Description = "本报告由普通标的成交构成；多品种策略的价格图仅显示所选参考品种。"
	default:
		view.Description = "本次运行未记录成交或期权腿，图表仅展示可用的行情与权益序列。"
	}
	return view, rows
}

func buildPortfolioAttributionView(result *backtest.Result, unit string) portfolioAttributionView {
	view := portfolioAttributionView{
		Description: "组合归因按报告已记录事件派生：普通成交展示成交净现金流，期权展示价差已实现盈亏；账户权益仍是最终组合口径。",
	}
	if result == nil {
		return view
	}
	type aggregate struct {
		family   string
		events   int
		notional float64
		pnl      float64
		details  map[string]struct{}
	}
	newAgg := func(family string) *aggregate { return &aggregate{family: family, details: map[string]struct{}{}} }
	underlyings := map[string]*aggregate{}
	regularNotional := 0.0
	regularNetCash := 0.0
	optionNotional := 0.0
	optionRealizedPnL := 0.0
	optionLegs := 0
	closedSpreads := 0
	openSpreads := 0
	groups := map[int]struct{}{}

	for _, trade := range result.Trades {
		notional := math.Abs(trade.Qty * trade.FillPrice)
		netCash := trade.NetAmount()
		regularNotional += notional
		regularNetCash += netCash
		name := normalizeAttributionName(trade.Security.Symbol, "regular")
		agg := underlyings[name]
		if agg == nil {
			agg = newAgg("普通成交")
			underlyings[name] = agg
		}
		agg.events++
		agg.notional += notional
		agg.pnl += netCash
		if market := strings.TrimSpace(trade.Security.Market); market != "" {
			agg.details[market] = struct{}{}
		}
	}
	for _, spread := range result.SpreadPositions {
		if strings.EqualFold(strings.TrimSpace(spread.Status), "closed") {
			closedSpreads++
		} else {
			openSpreads++
		}
		if spread.GroupID != 0 {
			groups[spread.GroupID] = struct{}{}
		}
		underlying := spreadUnderlying(spread)
		if underlying == "" {
			underlying = fmt.Sprintf("spread #%d", spread.ID)
		}
		agg := underlyings[underlying]
		if agg == nil {
			agg = newAgg("期权价差")
			underlyings[underlying] = agg
		} else if agg.family != "期权价差" {
			agg.family = "混合"
		}
		agg.pnl += spread.RealizedPnL
		optionRealizedPnL += spread.RealizedPnL
		for _, leg := range spread.Legs {
			optionLegs++
			agg.events++
			notional := math.Abs(leg.Qty * leg.EntryPrice)
			agg.notional += notional
			optionNotional += notional
			if tag := strings.TrimSpace(spread.Tag); tag != "" {
				agg.details[tag] = struct{}{}
			}
		}
	}

	if len(result.Trades) > 0 {
		view.InstrumentRows = append(view.InstrumentRows, portfolioAttributionRowView{
			Name:     "普通股票/现货成交",
			Family:   "Regular",
			Events:   integer(len(result.Trades)),
			Notional: amount4(regularNotional, unit),
			PnL:      signedAmount(regularNetCash, unit),
			Details:  fmt.Sprintf("%s 个成交标的", integer(countRegularSecurities(result.Trades))),
			PnLClass: signedClass(regularNetCash),
		})
	}
	if len(result.SpreadPositions) > 0 {
		details := []string{fmt.Sprintf("%s 条期权腿", integer(optionLegs)), fmt.Sprintf("%s 已平仓", integer(closedSpreads)), fmt.Sprintf("%s 未平仓", integer(openSpreads))}
		if len(groups) > 0 {
			details = append(details, fmt.Sprintf("%s 个订单组", integer(len(groups))))
		}
		view.InstrumentRows = append(view.InstrumentRows, portfolioAttributionRowView{
			Name:     "期权价差/合约生命周期",
			Family:   "Options",
			Events:   integer(len(result.SpreadPositions)),
			Notional: amount4(optionNotional, unit),
			PnL:      signedAmount(optionRealizedPnL, unit),
			Details:  strings.Join(details, " · "),
			PnLClass: signedClass(optionRealizedPnL),
		})
	}

	keys := make([]string, 0, len(underlyings))
	for key := range underlyings {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := underlyings[keys[i]]
		right := underlyings[keys[j]]
		if math.Abs(left.pnl) != math.Abs(right.pnl) {
			return math.Abs(left.pnl) > math.Abs(right.pnl)
		}
		if left.events != right.events {
			return left.events > right.events
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 10 {
		keys = keys[:10]
	}
	for _, key := range keys {
		agg := underlyings[key]
		view.UnderlyingRows = append(view.UnderlyingRows, portfolioAttributionRowView{
			Name:     key,
			Family:   agg.family,
			Events:   integer(agg.events),
			Notional: amount4(agg.notional, unit),
			PnL:      signedAmount(agg.pnl, unit),
			Details:  joinTopDetails(agg.details, 3),
			PnLClass: signedClass(agg.pnl),
		})
	}
	view.RegularNetCash = signedAmount(regularNetCash, unit)
	view.OptionRealizedPnL = signedAmount(optionRealizedPnL, unit)
	return view
}

func buildPortfolioAttributionStats(result *backtest.Result) portfolioAttributionStats {
	if result == nil {
		return portfolioAttributionStats{}
	}
	securities := map[string]struct{}{}
	underlyings := map[string]struct{}{}
	stats := portfolioAttributionStats{RegularFills: len(result.Trades), OptionSpreads: len(result.SpreadPositions), HasRegular: len(result.Trades) > 0, HasOptions: len(result.SpreadPositions) > 0}
	for _, trade := range result.Trades {
		key := normalizeAttributionName(strings.Join([]string{trade.Security.Market, trade.Security.Symbol, trade.Security.Interval}, "/"), "regular")
		securities[key] = struct{}{}
		underlyings[normalizeAttributionName(trade.Security.Symbol, "regular")] = struct{}{}
		stats.RegularNetCash += trade.NetAmount()
	}
	for _, spread := range result.SpreadPositions {
		underlying := spreadUnderlying(spread)
		if underlying != "" {
			underlyings[underlying] = struct{}{}
		}
		stats.OptionRealizedPnL += spread.RealizedPnL
		for _, leg := range spread.Legs {
			stats.OptionLegs++
			securities[normalizeAttributionName(leg.Symbol, fmt.Sprintf("spread-%d", spread.ID))] = struct{}{}
		}
	}
	stats.SecurityCount = len(securities)
	stats.UnderlyingCount = len(underlyings)
	return stats
}

func countRegularSecurities(trades []backtest.Trade) int {
	seen := map[string]struct{}{}
	for _, trade := range trades {
		key := strings.Join([]string{trade.Security.Market, trade.Security.Symbol, trade.Security.Interval}, "/")
		seen[normalizeAttributionName(key, "regular")] = struct{}{}
	}
	return len(seen)
}

func spreadUnderlying(spread backtest.SpreadPositionReport) string {
	for _, leg := range spread.Legs {
		if underlying := optionUnderlyingFromSymbol(leg.Symbol); underlying != "" {
			return underlying
		}
	}
	return normalizeAttributionName(stripExecDeltaTagSuffix(spread.Tag), "")
}

func optionUnderlyingFromSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return ""
	}
	for index := 0; index+6 < len(symbol); index++ {
		chunk := symbol[index : index+6]
		if isDigits(chunk) && index+6 < len(symbol) {
			right := symbol[index+6]
			if right == 'C' || right == 'P' {
				return strings.TrimRight(symbol[:index], " _-")
			}
		}
	}
	if cut := strings.IndexAny(symbol, "-_ "); cut > 0 {
		return symbol[:cut]
	}
	return symbol
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizeAttributionName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func joinTopDetails(details map[string]struct{}, limit int) string {
	if len(details) == 0 {
		return "-"
	}
	values := make([]string, 0, len(details))
	for value := range details {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	if limit > 0 && len(values) > limit {
		values = append(values[:limit], fmt.Sprintf("+%d", len(values)-limit))
	}
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, " · ")
}

func signedClass(value float64) string {
	switch {
	case value > 0:
		return "text-up"
	case value < 0:
		return "text-down"
	default:
		return "text-slate-300"
	}
}

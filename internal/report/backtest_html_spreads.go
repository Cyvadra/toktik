package report

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func buildSpreadRows(spreads []backtest.SpreadPositionReport, unit string, metricResolver spreadMetricResolver, priceResolver underlyingPriceResolver) []spreadRowView {
	rows := make([]spreadRowView, 0, len(spreads)*2)
	for _, spread := range spreads {
		displayTag := stripExecDeltaTagSuffix(spread.Tag)
		displayCloseNote := stripExecDeltaTagSuffix(spread.CloseNote)
		openMetrics := metricResolver.valuesAt(spread.OpenTime)
		openUnderlyingPrice := priceResolver.valueAt(spread.OpenTime)
		legs := make([]spreadLegRowView, 0, len(spread.Legs))
		for _, leg := range spread.Legs {
			expiryOpenDays := leg.Expiration.Sub(leg.EntryTime).Hours() / 24
			entryDelta := leg.Delta
			if leg.EntryDelta != nil {
				entryDelta = *leg.EntryDelta
			}
			closeTimeLabel := "平仓时间"
			legView := spreadLegRowView{
				Symbol:         leg.Symbol,
				Side:           translateSide(leg.Side),
				Type:           translateOptionType(string(leg.Type)),
				StrikePrice:    currency(leg.StrikePrice),
				Expiration:     formatDate(leg.Expiration),
				OpenSelect:     expiryOpenDelta(expiryOpenDays, entryDelta),
				Qty:            decimal(leg.Qty),
				EntryPrice:     amount4(leg.EntryPrice, unit),
				EntryAmount:    amount4(leg.Qty*leg.EntryPrice, unit),
				EntryTime:      formatDateTime(leg.EntryTime),
				ClosePrice:     nullableAmount4(leg.ClosePrice, leg.Closed, unit),
				CloseTimeLabel: closeTimeLabel,
				CloseTime:      "-",
				CloseReason:    fallbackText(strings.TrimSpace(leg.CloseReason), "-"),
				RealizedPnL:    signedAmount(leg.RealizedPnL, unit),
				SideClass:      sideClass(leg.Side),
			}
			if leg.CloseTriggerTime != nil {
				legView.CloseTimeLabel = "平仓触发时间"
				legView.CloseTime = formatDateTime(*leg.CloseTriggerTime)
			} else if leg.CloseTime != nil {
				legView.CloseTime = formatDateTime(*leg.CloseTime)
			}
			legs = append(legs, legView)
		}

		openAnchor := fmt.Sprintf("spread-%d-open", spread.ID)
		closeAnchor := fmt.Sprintf("spread-%d-close", spread.ID)

		openRow := spreadRowView{
			ID:              spread.ID,
			Tag:             displayTag,
			GroupID:         spread.GroupID,
			AnchorID:        openAnchor,
			EventType:       "OPEN",
			EventClass:      "bg-sky-500/15 text-sky-200 ring-sky-400/40",
			EventTime:       formatDateTime(spread.OpenTime),
			HeaderTimeLabel: "下单",
			HeaderTime:      formatDateTime(spread.OpenTime),
			UnderlyingPrice: openUnderlyingPrice,
			EventUnix:       spread.OpenTime.Unix(),
			WindowStartUnix: spread.OpenTime.Unix(),
			WindowEndUnix:   spread.OpenTime.Unix(),
			Status:          "已开仓",
			eventUnix:       spread.OpenTime.Unix(),
			OpenTime:        formatDateTime(spread.OpenTime),
			CloseTime:       "-",
			DaysHeld:        "-",
			RealizedPnL:     "-",
			StatusClass:     statusClass("open"),
			ReportMetrics:   openMetrics,
			Legs:            legs,
		}
		if spread.CloseTime != nil {
			openRow.WindowEndUnix = spread.CloseTime.Unix()
			openRow.RelatedLink = closeAnchor
			openRow.RelatedText = "跳转到平仓"
		}
		rows = append(rows, openRow)

		if spread.CloseTime != nil {
			closeTag := displayTag
			closeEventTime := spread.CloseTime
			closeHeaderLabel := "平仓时间"
			closeHeaderTime := formatDateTime(*spread.CloseTime)
			if spread.CloseTriggerTime != nil {
				closeEventTime = spread.CloseTriggerTime
				closeHeaderLabel = "平仓触发"
				closeHeaderTime = formatDateTime(*spread.CloseTriggerTime)
			}
			if strings.TrimSpace(displayCloseNote) != "" {
				closeTag = displayCloseNote
			}
			closeMetrics := metricResolver.valuesAt(*closeEventTime)
			closeUnderlyingPrice := priceResolver.valueAt(*closeEventTime)
			rows = append(rows, spreadRowView{
				ID:              spread.ID,
				Tag:             closeTag,
				GroupID:         spread.GroupID,
				AnchorID:        closeAnchor,
				EventType:       "CLOSE",
				EventClass:      "bg-rose-500/15 text-rose-200 ring-rose-400/40",
				EventTime:       formatDateTime(*closeEventTime),
				HeaderTimeLabel: closeHeaderLabel,
				HeaderTime:      closeHeaderTime,
				UnderlyingPrice: closeUnderlyingPrice,
				RelatedLink:     openAnchor,
				RelatedText:     "跳转到开仓",
				EventUnix:       closeEventTime.Unix(),
				WindowStartUnix: spread.OpenTime.Unix(),
				WindowEndUnix:   closeEventTime.Unix(),
				Status:          translateSpreadStatus(spread.Status),
				eventUnix:       closeEventTime.Unix(),
				OpenTime:        formatDateTime(spread.OpenTime),
				CloseTime:       closeHeaderTime,
				DaysHeld:        fmt.Sprintf("%.2f 天", spread.DaysHeld),
				RealizedPnL:     signedAmount(spread.RealizedPnL, unit),
				StatusClass:     statusClass(spread.Status),
				ReportMetrics:   closeMetrics,
				Legs:            legs,
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].eventUnix != rows[j].eventUnix {
			return rows[i].eventUnix < rows[j].eventUnix
		}
		if rows[i].ID != rows[j].ID {
			return rows[i].ID < rows[j].ID
		}
		if rows[i].EventType != rows[j].EventType {
			return rows[i].EventType == "OPEN"
		}
		return rows[i].Tag < rows[j].Tag
	})

	return rows
}

func buildSpreadGroupViews(groups []backtest.SpreadGroupReport, spreads []backtest.SpreadPositionReport, unit string, metricResolver spreadMetricResolver, priceResolver underlyingPriceResolver) ([]spreadGroupView, []spreadRowView) {
	if len(spreads) == 0 {
		return nil, nil
	}

	spreadMap := make(map[int]backtest.SpreadPositionReport, len(spreads))
	groupSpreads := make(map[int][]backtest.SpreadPositionReport)
	ungrouped := make([]backtest.SpreadPositionReport, 0, len(spreads))
	for _, spread := range spreads {
		spreadMap[spread.ID] = spread
		if spread.GroupID > 0 {
			groupSpreads[spread.GroupID] = append(groupSpreads[spread.GroupID], spread)
			continue
		}
		ungrouped = append(ungrouped, spread)
	}

	groupReports := make(map[int]backtest.SpreadGroupReport, len(groups))
	for _, group := range groups {
		groupReports[group.ID] = group
		if _, exists := groupSpreads[group.ID]; !exists {
			groupSpreads[group.ID] = nil
		}
	}

	views := make([]spreadGroupView, 0, len(groupSpreads))
	for groupID, groupedSpreads := range groupSpreads {
		if groupID <= 0 {
			continue
		}

		report, hasReport := groupReports[groupID]
		orderedSpreads := orderedGroupedSpreads(report, groupedSpreads, spreadMap)
		if len(orderedSpreads) == 0 {
			continue
		}
		rows := buildSpreadRows(orderedSpreads, unit, metricResolver, priceResolver)

		openTime := earliestSpreadOpenTime(orderedSpreads)
		if hasReport && !report.OpenTime.IsZero() {
			openTime = report.OpenTime
		}
		closeTime := latestSpreadCloseTimeReport(orderedSpreads)
		if hasReport && report.CloseTime != nil {
			closeTime = report.CloseTime
		}
		totalPnL := totalGroupedSpreadPnL(orderedSpreads)
		status := groupedSpreadStatus(report, orderedSpreads)
		view := spreadGroupView{
			ID:            groupID,
			Tag:           groupedSpreadTag(report, groupID),
			AnchorID:      fmt.Sprintf("spread-group-%d", groupID),
			Status:        translateSpreadStatus(status),
			StatusClass:   statusClass(status),
			OpenTime:      formatDateTime(openTime),
			CloseTime:     nullableTime(closeTime),
			InitAmount:    groupedInitAmount(report, unit),
			HighestEquity: amount(report.HighestEquity, unit),
			LowestEquity:  amount(report.LowestEquity, unit),
			MaxDrawdown:   pct(report.MaxDrawdown),
			DecayFactor:   groupedDecayFactor(report),
			RollCount:     groupedRollCount(report),
			TotalPnL:      signedAmount(totalPnL, unit),
			SpreadCount:   len(orderedSpreads),
			EventCount:    len(rows),
			Spreads:       rows,
		}
		if !openTime.IsZero() {
			view.eventUnix = openTime.Unix()
		}
		views = append(views, view)
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].eventUnix != views[j].eventUnix {
			return views[i].eventUnix < views[j].eventUnix
		}
		return views[i].ID < views[j].ID
	})

	return views, buildSpreadRows(ungrouped, unit, metricResolver, priceResolver)
}

func buildTopSpreadGroupDrawdownViews(groups []backtest.SpreadGroupReport, unit string) []spreadGroupDrawdownView {
	if len(groups) == 0 {
		return nil
	}
	ordered := append([]backtest.SpreadGroupReport(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].MaxDrawdown != ordered[j].MaxDrawdown {
			return ordered[i].MaxDrawdown > ordered[j].MaxDrawdown
		}
		return ordered[i].ID < ordered[j].ID
	})
	if len(ordered) > 5 {
		ordered = ordered[:5]
	}
	views := make([]spreadGroupDrawdownView, 0, len(ordered))
	for _, group := range ordered {
		status := strings.TrimSpace(group.Status)
		if status == "" {
			status = "open"
		}
		views = append(views, spreadGroupDrawdownView{
			ID:            group.ID,
			Tag:           groupedSpreadTag(group, group.ID),
			AnchorID:      fmt.Sprintf("spread-group-%d", group.ID),
			Status:        translateSpreadStatus(status),
			StatusClass:   statusClass(status),
			MaxDrawdown:   pct(group.MaxDrawdown),
			HighestEquity: amount(group.HighestEquity, unit),
			LowestEquity:  amount(group.LowestEquity, unit),
			TotalPnL:      signedAmount(group.TotalPnL, unit),
		})
	}
	return views
}

func orderedGroupedSpreads(report backtest.SpreadGroupReport, spreads []backtest.SpreadPositionReport, spreadMap map[int]backtest.SpreadPositionReport) []backtest.SpreadPositionReport {
	if len(spreads) == 0 {
		return nil
	}

	ordered := make([]backtest.SpreadPositionReport, 0, len(spreads))
	seen := make(map[int]struct{}, len(spreads))
	for _, spreadID := range report.SpreadIDs {
		spread, ok := spreadMap[spreadID]
		if !ok || spread.GroupID != report.ID {
			continue
		}
		ordered = append(ordered, spread)
		seen[spread.ID] = struct{}{}
	}
	for _, spread := range spreads {
		if _, ok := seen[spread.ID]; ok {
			continue
		}
		ordered = append(ordered, spread)
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].OpenTime.Equal(ordered[j].OpenTime) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].OpenTime.Before(ordered[j].OpenTime)
	})
	return ordered
}

func earliestSpreadOpenTime(spreads []backtest.SpreadPositionReport) time.Time {
	var earliest time.Time
	for _, spread := range spreads {
		if earliest.IsZero() || spread.OpenTime.Before(earliest) {
			earliest = spread.OpenTime
		}
	}
	return earliest
}

func latestSpreadCloseTimeReport(spreads []backtest.SpreadPositionReport) *time.Time {
	var latest time.Time
	found := false
	for _, spread := range spreads {
		if spread.CloseTime != nil && (!found || spread.CloseTime.After(latest)) {
			latest = *spread.CloseTime
			found = true
		}
	}
	if !found {
		return nil
	}
	return &latest
}

func totalGroupedSpreadPnL(spreads []backtest.SpreadPositionReport) float64 {
	total := 0.0
	for _, spread := range spreads {
		total += spread.RealizedPnL
	}
	return total
}

func groupedSpreadStatus(report backtest.SpreadGroupReport, spreads []backtest.SpreadPositionReport) string {
	if strings.TrimSpace(report.Status) != "" {
		return report.Status
	}
	if len(spreads) == 0 {
		return "open"
	}
	for _, spread := range spreads {
		if !strings.EqualFold(spread.Status, "closed") {
			return "open"
		}
	}
	return "closed"
}

func groupedSpreadTag(report backtest.SpreadGroupReport, groupID int) string {
	if strings.TrimSpace(report.Tag) != "" {
		return report.Tag
	}
	return fmt.Sprintf("spread-group-%d", groupID)
}

func groupedInitAmount(report backtest.SpreadGroupReport, unit string) string {
	if report.InitAmount == 0 {
		return "-"
	}
	return amount4(report.InitAmount, unit)
}

func groupedDecayFactor(report backtest.SpreadGroupReport) string {
	if report.DecayFactor == 0 {
		return "-"
	}
	return decimal(report.DecayFactor)
}

func groupedRollCount(report backtest.SpreadGroupReport) string {
	return integer(report.RollCount)
}

func nullableTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return formatDateTime(*value)
}

func stripExecDeltaTagSuffix(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	parts := strings.Split(tag, " | ")
	filtered := parts[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "exec_Delta=") {
			continue
		}
		filtered = append(filtered, part)
	}
	if len(filtered) == 0 {
		return tag
	}
	return strings.Join(filtered, " | ")
}

func newSpreadMetricResolver(result *backtest.Result) spreadMetricResolver {
	if result == nil || len(result.ReportColumns) == 0 || len(result.Timestamps) == 0 || len(result.Series) == 0 {
		return spreadMetricResolver{}
	}
	return spreadMetricResolver{
		timestamps: result.Timestamps,
		columns:    result.ReportColumns,
		series:     result.Series,
	}
}

func newUnderlyingPriceResolver(result *backtest.Result, source chartSeriesSource) underlyingPriceResolver {
	if result == nil || len(result.Timestamps) == 0 || len(result.Series) == 0 {
		return underlyingPriceResolver{}
	}
	return underlyingPriceResolver{
		timestamps: result.Timestamps,
		series:     result.Series,
		source:     source,
	}
}

func (resolver underlyingPriceResolver) valueAt(eventTime time.Time) string {
	if eventTime.IsZero() || len(resolver.timestamps) == 0 || len(resolver.series) == 0 {
		return ""
	}
	index := reportColumnIndexAtOrBefore(resolver.timestamps, eventTime)
	if index < 0 {
		return ""
	}
	for _, key := range []string{"close", "open", "high", "low"} {
		values := seriesBySource(resolver.series, resolver.source.Prefix, key)
		if index >= len(values) {
			continue
		}
		value := values[index]
		if chartValueValid(value) {
			return currency(value)
		}
	}
	return ""
}

func (resolver spreadMetricResolver) valuesAt(eventTime time.Time) []spreadReportMetricView {
	if eventTime.IsZero() || len(resolver.timestamps) == 0 || len(resolver.columns) == 0 || len(resolver.series) == 0 {
		return nil
	}
	index := reportColumnIndexAtOrBefore(resolver.timestamps, eventTime)
	if index < 0 {
		return nil
	}
	metrics := make([]spreadReportMetricView, 0, len(resolver.columns))
	for _, column := range resolver.columns {
		values := resolver.series[column.Source]
		if index >= len(values) {
			continue
		}
		value := values[index]
		if !chartValueValid(value) {
			continue
		}
		kindLabel := "子图"
		kindClass := "bg-teal-500/10 text-teal-200 ring-1 ring-teal-400/20"
		if column.Overlay {
			kindLabel = "叠加"
			kindClass = "bg-sky-500/10 text-sky-200 ring-1 ring-sky-400/20"
		}
		metrics = append(metrics, spreadReportMetricView{
			Label:     fallbackText(strings.TrimSpace(column.Label), column.Source),
			Source:    column.Source,
			Value:     formatReportMetricValue(value, column.Decimals),
			KindLabel: kindLabel,
			KindClass: kindClass,
		})
	}
	if len(metrics) == 0 {
		return nil
	}
	return metrics
}

func reportColumnIndexAtOrBefore(timestamps []time.Time, eventTime time.Time) int {
	if len(timestamps) == 0 || eventTime.IsZero() {
		return -1
	}
	idx := sort.Search(len(timestamps), func(i int) bool {
		return timestamps[i].After(eventTime)
	})
	if idx == 0 {
		if timestamps[0].After(eventTime) {
			return -1
		}
		return 0
	}
	if idx >= len(timestamps) {
		return len(timestamps) - 1
	}
	return idx - 1
}

func renderSpreadSectionsHTML(groups []spreadGroupView, ungrouped []spreadRowView) string {
	if len(groups) == 0 && len(ungrouped) == 0 {
		return ""
	}
	var builder strings.Builder
	if len(groups) > 0 {
		builder.WriteString("<div class=\"space-y-5 mb-5\">")
		for _, group := range groups {
			fmt.Fprintf(&builder, "<details id=\"%s\" class=\"border border-white/5 rounded-xl overflow-hidden bg-white/[0.02]\" data-spread-group open><summary class=\"spread-group-summary cursor-pointer px-4 py-3 bg-white/[0.03] transition hover:bg-white/[0.045] focus:outline-none focus-visible:ring-2 focus-visible:ring-teal-400/60\"><div class=\"flex flex-wrap items-center justify-between gap-3\"><div class=\"flex flex-wrap items-center gap-3\"><span class=\"mono text-xs text-slate-400 inline-flex items-center gap-2\"><span class=\"spread-group-chevron text-sm text-teal-300\">▸</span><span class=\"spread-group-state-open\">展开</span><span class=\"spread-group-state-closed\">收起</span></span><span class=\"font-medium text-slate-100\">组 #%d %s</span><span class=\"mono text-xs px-2 py-0.5 rounded %s\">%s</span><span class=\"mono text-xs text-slate-400\">开组 %s</span>",
				template.HTMLEscapeString(group.AnchorID), group.ID, template.HTMLEscapeString(group.Tag), group.StatusClass, template.HTMLEscapeString(group.Status), template.HTMLEscapeString(group.OpenTime))
			if group.CloseTime != "-" {
				fmt.Fprintf(&builder, "<span class=\"mono text-xs text-slate-400\">闭组 %s</span>", template.HTMLEscapeString(group.CloseTime))
			}
			fmt.Fprintf(&builder, "</div><div class=\"flex flex-wrap gap-5 text-xs text-slate-400\"><span>%d 个持仓</span><span>%d 个事件</span><span>滚仓 %s 次</span><span>初始资金 <span class=\"mono text-slate-300\">%s</span></span><span>Highest Equity <span class=\"mono text-slate-300\">%s</span></span><span>Lowest Equity <span class=\"mono text-slate-300\">%s</span></span><span>Max Drawdown <span class=\"mono text-rose-200\">%s</span></span><span>衰减 <span class=\"mono text-slate-300\">%s</span></span><span>组盈亏 <span class=\"mono text-slate-200\">%s</span></span></div></div></summary><div class=\"border-t border-white/5 p-4 space-y-4\">",
				group.SpreadCount,
				group.EventCount,
				template.HTMLEscapeString(group.RollCount),
				template.HTMLEscapeString(group.InitAmount),
				template.HTMLEscapeString(group.HighestEquity),
				template.HTMLEscapeString(group.LowestEquity),
				template.HTMLEscapeString(group.MaxDrawdown),
				template.HTMLEscapeString(group.DecayFactor),
				template.HTMLEscapeString(group.TotalPnL),
			)
			for _, spread := range group.Spreads {
				builder.WriteString(renderSpreadEventCardHTML(spread))
			}
			builder.WriteString("</div></details>")
		}
		builder.WriteString("</div>")
	}
	if len(ungrouped) > 0 {
		fmt.Fprintf(&builder, "<div><div class=\"flex items-center justify-between mb-3\"><h3 class=\"text-sm font-medium text-slate-200\">未分组持仓</h3><span class=\"mono text-xs text-slate-400\">%d 个事件</span></div>", len(ungrouped))
		for _, spread := range ungrouped {
			builder.WriteString(renderSpreadEventCardHTML(spread))
		}
		builder.WriteString("</div>")
	}
	return builder.String()
}

func renderSpreadEventCardHTML(row spreadRowView) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "<div id=\"%s\" class=\"mb-4 border border-white/5 rounded-lg overflow-hidden\"><div class=\"flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 bg-white/[0.02] border-b border-white/5\"><div class=\"flex items-center gap-3\"><span class=\"font-medium text-slate-200\">#%d %s</span>", template.HTMLEscapeString(row.AnchorID), row.ID, template.HTMLEscapeString(row.Tag))
	if row.GroupID > 0 {
		fmt.Fprintf(&builder, "<span class=\"mono text-xs px-2 py-0.5 rounded bg-violet-500/15 text-violet-300 ring-1 ring-violet-400/30\">组 #%d</span>", row.GroupID)
	}
	fmt.Fprintf(&builder, "<span class=\"mono text-xs px-2 py-0.5 rounded %s\">%s</span><span class=\"mono text-xs text-slate-400\">%s %s</span>", row.StatusClass, template.HTMLEscapeString(row.Status), template.HTMLEscapeString(row.HeaderTimeLabel), template.HTMLEscapeString(row.HeaderTime))
	if row.UnderlyingPrice != "" {
		fmt.Fprintf(&builder, "<span class=\"mono text-xs text-slate-400\">标的 %s</span>", template.HTMLEscapeString(row.UnderlyingPrice))
	}
	builder.WriteString("</div><div class=\"flex gap-5 text-xs text-slate-400\">")
	if row.EventUnix > 0 {
		fmt.Fprintf(&builder, "<button type=\"button\" class=\"rounded-md border border-white/10 px-2.5 py-1 text-[11px] text-slate-300 transition hover:border-sky-400/40 hover:text-sky-200\" data-chart-jump-time=\"%d\" data-chart-window-start=\"%d\" data-chart-window-end=\"%d\">定位到图表</button>", row.EventUnix, row.WindowStartUnix, row.WindowEndUnix)
	}
	if row.EventTime != row.HeaderTime {
		label := "平仓"
		if row.EventType == "OPEN" {
			label = "开仓"
		}
		fmt.Fprintf(&builder, "<span>%s %s</span>", label, template.HTMLEscapeString(row.EventTime))
	}
	fmt.Fprintf(&builder, "<span>盈亏 <span class=\"mono text-slate-300\">%s</span></span>", template.HTMLEscapeString(row.RealizedPnL))
	if row.RelatedLink != "" {
		fmt.Fprintf(&builder, "<a class=\"text-sky-300 hover:text-sky-200 underline underline-offset-2\" href=\"#%s\">%s</a>", template.HTMLEscapeString(row.RelatedLink), template.HTMLEscapeString(row.RelatedText))
	}
	builder.WriteString("</div></div><div class=\"px-4 py-3 space-y-3\">")
	if len(row.ReportMetrics) > 0 {
		builder.WriteString("<div class=\"grid gap-2 md:grid-cols-2 xl:grid-cols-4\">")
		for _, metric := range row.ReportMetrics {
			fmt.Fprintf(&builder, "<div class=\"spread-metric-card\"><div class=\"flex items-center justify-between gap-2\"><div class=\"text-xs text-slate-400\">%s</div><span class=\"mono text-[10px] px-2 py-0.5 rounded %s\">%s</span></div><div class=\"mt-2 mono text-sm text-slate-200\">%s</div><div class=\"mt-1 mono text-[11px] text-slate-500\">%s</div></div>", template.HTMLEscapeString(metric.Label), metric.KindClass, template.HTMLEscapeString(metric.KindLabel), template.HTMLEscapeString(metric.Value), template.HTMLEscapeString(metric.Source))
		}
		builder.WriteString("</div>")
	}
	fmt.Fprintf(&builder, "<div class=\"grid gap-3 md:grid-cols-2 xl:grid-cols-5 text-sm\"><div><div class=\"text-slate-400\">状态</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">开仓时间</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">平仓时间</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">持有天数</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">实现盈亏</div><div class=\"mt-1 mono text-slate-200\">%s</div></div></div>", template.HTMLEscapeString(row.Status), template.HTMLEscapeString(row.OpenTime), template.HTMLEscapeString(row.CloseTime), template.HTMLEscapeString(row.DaysHeld), template.HTMLEscapeString(row.RealizedPnL))
	builder.WriteString(renderSpreadLegsTableHTML(row))
	builder.WriteString("</div></div>")
	return builder.String()
}

func renderSpreadLegsTableHTML(row spreadRowView) string {
	if len(row.Legs) == 0 {
		return ""
	}
	var builder strings.Builder
	if row.EventType == "OPEN" {
		builder.WriteString("<div class=\"overflow-x-auto border border-white/8 rounded-lg\"><table class=\"min-w-full text-sm\"><thead class=\"bg-white/[0.02] border-b border-white/8\"><tr class=\"text-left text-slate-400\"><th class=\"px-4 py-2\">合约</th><th class=\"px-4 py-2\">事件</th><th class=\"px-4 py-2\">方向</th><th class=\"px-4 py-2\">类型</th><th class=\"px-4 py-2\">行权价</th><th class=\"px-4 py-2\">到期日</th><th class=\"px-4 py-2\">筛选</th><th class=\"px-4 py-2\">数量</th><th class=\"px-4 py-2\">入场价</th><th class=\"px-4 py-2\">入场额</th></tr></thead><tbody class=\"divide-y divide-white/5\">")
		for _, leg := range row.Legs {
			fmt.Fprintf(&builder, "<tr class=\"border-b border-white/[0.03]\"><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5\"><span class=\"mono text-xs px-2 py-0.5 rounded ring-1 %s\">开仓</span></td><td class=\"px-4 py-1.5 text-slate-300\">%s</td><td class=\"px-4 py-1.5 text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td></tr>", template.HTMLEscapeString(leg.Symbol), row.EventClass, template.HTMLEscapeString(leg.Side), template.HTMLEscapeString(leg.Type), template.HTMLEscapeString(leg.StrikePrice), template.HTMLEscapeString(leg.Expiration), template.HTMLEscapeString(leg.OpenSelect), template.HTMLEscapeString(leg.Qty), template.HTMLEscapeString(leg.EntryPrice), template.HTMLEscapeString(leg.EntryAmount))
		}
		builder.WriteString("</tbody></table></div>")
		return builder.String()
	}
	closeTimeLabel := "平仓时间"
	if label := strings.TrimSpace(row.Legs[0].CloseTimeLabel); label != "" {
		closeTimeLabel = label
	}
	fmt.Fprintf(&builder, "<div class=\"overflow-x-auto border border-white/8 rounded-lg\"><table class=\"min-w-full text-sm\"><thead class=\"bg-white/[0.02] border-b border-white/8\"><tr class=\"text-left text-slate-400\"><th class=\"px-4 py-2\">合约</th><th class=\"px-4 py-2\">事件</th><th class=\"px-4 py-2\">方向</th><th class=\"px-4 py-2\">类型</th><th class=\"px-4 py-2\">行权价</th><th class=\"px-4 py-2\">到期日</th><th class=\"px-4 py-2\">数量</th><th class=\"px-4 py-2\">平仓价</th><th class=\"px-4 py-2\">%s</th><th class=\"px-4 py-2\">原因</th><th class=\"px-4 py-2\">实现盈亏</th></tr></thead><tbody class=\"divide-y divide-white/5\">", template.HTMLEscapeString(closeTimeLabel))
	for _, leg := range row.Legs {
		fmt.Fprintf(&builder, "<tr class=\"border-b border-white/[0.03]\"><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5\"><span class=\"mono text-xs px-2 py-0.5 rounded ring-1 %s\">平仓</span></td><td class=\"px-4 py-1.5 text-slate-300\">%s</td><td class=\"px-4 py-1.5 text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td></tr>", template.HTMLEscapeString(leg.Symbol), row.EventClass, template.HTMLEscapeString(leg.Side), template.HTMLEscapeString(leg.Type), template.HTMLEscapeString(leg.StrikePrice), template.HTMLEscapeString(leg.Expiration), template.HTMLEscapeString(leg.Qty), template.HTMLEscapeString(leg.ClosePrice), template.HTMLEscapeString(leg.CloseTime), template.HTMLEscapeString(leg.CloseReason), template.HTMLEscapeString(leg.RealizedPnL))
	}
	builder.WriteString("</tbody></table></div>")
	return builder.String()
}

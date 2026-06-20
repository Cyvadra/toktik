package runtime

import "strings"

func applyBuiltinDocDefaults(doc *BuiltinDoc) {
	if override, ok := builtinDocOverrides[doc.Name]; ok {
		if len(doc.Params) == 0 && len(override.Params) > 0 {
			doc.Params = append([]string(nil), override.Params...)
		}
		if override.Summary != "" {
			doc.Summary = override.Summary
		}
		if override.Example != "" {
			doc.Example = override.Example
		}
		if override.ReturnValue != "" {
			doc.ReturnValue = override.ReturnValue
		}
	}
	if len(doc.Params) == 0 {
		if params, ok := builtinParamHints[doc.Name]; ok {
			doc.Params = append([]string(nil), params...)
		}
	}
	if doc.Summary == "" {
		doc.Summary = defaultBuiltinSummary(doc.Name, doc.Kind)
	}
	if doc.Example == "" {
		doc.Example = defaultBuiltinExample(doc.Name, doc.Params, doc.Kind)
	}
	if doc.ReturnValue == "" {
		doc.ReturnValue = defaultBuiltinReturn(doc.Name, doc.Kind)
	}
}

func defaultBuiltinSummary(name string, kind BuiltinKind) string {
	if kind == BuiltinProperty {
		return "用於讀取目前回測上下文中的帳戶、部位或狀態欄位。"
	}
	if summary, ok := builtinSummaryByName[name]; ok {
		return summary
	}
	switch namespace(name) {
	case "alpha":
		return "用於把價格或成交量序列轉成量化 alpha 特徵，通常作為進出場濾網或排序訊號。"
	case "contract":
		return "用於讀取已選期權合約的欄位，讓策略能依履約價、到期日、Greeks 或報價做篩選。"
	case "event":
		return "用於逐筆消費外部信號事件，避免同一根 bar 重複處理同一個事件。"
	case "group":
		return "用於管理一組相關期權 spread，常見於分批建倉、roll 倉與組合平倉。"
	case "leg":
		return "用於把期權合約包成買方或賣方 leg，再提交給 spread.open 類函數。"
	case "math":
		return "用於風控、倉位大小、價格門檻或指標轉換中的數值計算。"
	case "options":
		return "用於從期權鏈中篩出符合到期日、Delta、權利金或履約價條件的候選合約。"
	case "order":
		return "用於提交明確類型的交易指令，適合需要限價、停損、TWAP 或名義金額下單的策略。"
	case "portfolio":
		return "用於讀取回測請求注入的多標的組合設定，支援輪詢符號與權重配置。"
	case "ref":
		return "用於跨 bar 保存自訂狀態，例如上次訂單 ID、spread ID 或已處理次數。"
	case "schedule":
		return "用於排程未來幾根 bar 後關閉 spread、leg 或 group。"
	case "signal":
		return "用於讀取目前 bar 的外部信號欄位，適合把信號平台輸出轉成 DSL 下單邏輯。"
	case "spread":
		return "用於建立、查詢、關閉與監控期權價差部位。"
	case "str":
		return "用於處理符號、標籤、設定字串或報告欄位文字。"
	case "ta":
		return "用於把 OHLCV 序列轉成技術指標，作為趨勢、動能、波動或突破條件。"
	}
	return "用於策略腳本中的資料處理、條件判斷或交易流程控制。"
}

func defaultBuiltinReturn(name string, kind BuiltinKind) string {
	if kind == BuiltinProperty || kind == BuiltinConstant {
		return "值"
	}
	switch {
	case strings.HasPrefix(name, "str."):
		return "字串或陣列"
	case strings.HasPrefix(name, "options."):
		return "期權鏈或值"
	case strings.HasPrefix(name, "contract."):
		return "合約欄位"
	case strings.HasPrefix(name, "spread."), strings.HasPrefix(name, "group."), strings.HasPrefix(name, "schedule."), strings.HasPrefix(name, "leg."):
		return "handle 或值"
	case strings.HasPrefix(name, "signal."), strings.HasPrefix(name, "event."):
		return "信號值"
	case name == "strategy.entry" || name == "strategy.close" || name == "strategy.exit" || name == "buy" || name == "sell":
		return "na"
	}
	return "數值"
}

func defaultBuiltinExample(name string, params []string, kind BuiltinKind) string {
	if kind == BuiltinProperty || kind == BuiltinConstant {
		return name
	}
	if example, ok := builtinExampleByName[name]; ok {
		return example
	}
	if len(params) == 0 {
		return zeroArgExample(name)
	}
	args := make([]string, 0, len(params))
	for _, param := range params {
		args = append(args, sampleArg(param))
	}
	return name + "(" + strings.Join(args, ", ") + ")"
}

func sampleArg(param string) string {
	switch strings.ToLower(param) {
	case "s":
		return "\"SPY,QQQ\""
	case "left", "a":
		return "ta.sma(close, 10)"
	case "right", "b":
		return "ta.sma(close, 30)"
	case "x":
		return "close"
	case "y":
		return "volume"
	case "window", "period", "occurrence":
		return "20"
	case "condition":
		return "close > ta.sma(close, 20)"
	case "target", "target_days":
		return "30"
	case "target_sum":
		return "1"
	case "min_delta":
		return "-0.35"
	case "max_delta":
		return "-0.15"
	case "min_days":
		return "20"
	case "max_days":
		return "45"
	case "min_bid":
		return "1.0"
	case "min":
		return "close * 0.9"
	case "max":
		return "close * 1.1"
	case "chain":
		return "options.chain(\"us-options\", \"SPY\")"
	case "contract":
		return "contract"
	case "spread_id":
		return "spread_id"
	case "leg_index":
		return "0"
	case "close_price":
		return "contract.mark(contract)"
	case "reason":
		return "\"risk_exit\""
	case "init_amount":
		return "10000"
	case "decay_factor":
		return "0.5"
	case "exp":
		return "2"
	case "source", "series", "value":
		return "close"
	case "length", "bars", "twap_bars", "bars_offset", "index", "precision":
		return "20"
	case "id", "name", "title", "tag", "ref", "group_ref", "group_id":
		return "\"main\""
	case "direction":
		return "strategy.long"
	case "side":
		return "order.buy"
	case "qty":
		return "1"
	case "notional":
		return "1000"
	case "market":
		return "\"us-options\""
	case "symbol", "underlying":
		return "\"SPY\""
	case "interval":
		return "\"1d\""
	case "field":
		return "\"close\""
	case "defval", "minval", "maxval", "step":
		return "0"
	case "limit", "limit_price", "price":
		return "close * 0.99"
	case "stop", "stop_price":
		return "close * 0.95"
	case "immediate", "overlay":
		return "false"
	case "note":
		return "\"note\""
	case "type":
		return "\"market\""
	case "options":
		return "[\"fast\", \"slow\"]"
	case "parts":
		return "[\"A\", \"B\"]"
	case "sep":
		return "\",\""
	case "legs":
		return "[leg.sell(contract, 1)]"
	case "schedule_at":
		return "0"
	}
	return param
}

func zeroArgExample(name string) string {
	switch namespace(name) {
	case "event", "signal":
		return name + "()"
	case "portfolio":
		return name + "()"
	case "spread", "group":
		return name + "()"
	case "options":
		return name + "(options.chain(\"us-options\", \"SPY\"))"
	case "contract":
		return name + "(contract)"
	case "str":
		return name + "(\"SPY\")"
	case "math":
		return name + "(close)"
	case "ta", "alpha":
		return name + "(close, 20)"
	}
	return name + "()"
}

func namespace(name string) string {
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		return name[:idx]
	}
	return "core"
}

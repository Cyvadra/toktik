package runtime

func builtinConstants() []BuiltinDoc {
	docs := []BuiltinDoc{
		{Name: "bar_index", Kind: BuiltinConstant, Summary: "用於限制策略暖身期、排除前幾根資料不足的 bar。", Example: "ready = bar_index > 50", ReturnValue: "數值"},
		{Name: "close", Kind: BuiltinConstant, Summary: "用作主要價格序列，常拿來計算均線、動能、突破與下單價格。", Example: "fast = ta.sma(close, 20)", ReturnValue: "series"},
		{Name: "close_raw", Kind: BuiltinConstant, Summary: "用於讀取未復權收盤價；涉及期權 strike、行權價比較或原始價格檢查時使用。", Example: "itm = close_raw > option_strike", ReturnValue: "series"},
		{Name: "high", Kind: BuiltinConstant, Summary: "用於偵測近期高點、突破條件或計算震幅類指標。", Example: "breakout = close > ta.highest(high, 20)[1]", ReturnValue: "series"},
		{Name: "high_raw", Kind: BuiltinConstant, Summary: "用於讀取未復權最高價。", Example: "raw_range = high_raw - low_raw", ReturnValue: "series"},
		{Name: "low", Kind: BuiltinConstant, Summary: "用於偵測近期低點、停損位置或回撤條件。", Example: "stop_price = ta.lowest(low, 10)", ReturnValue: "series"},
		{Name: "low_raw", Kind: BuiltinConstant, Summary: "用於讀取未復權最低價。", Example: "raw_range = high_raw - low_raw", ReturnValue: "series"},
		{Name: "math_e", Kind: BuiltinConstant, Summary: "用於自然對數或指數模型中的 e 常數。", Example: "one = math.log(math_e)", ReturnValue: "數值"},
		{Name: "math_phi", Kind: BuiltinConstant, Summary: "用於黃金比例相關的價格或比例計算。", Example: "target = close * math_phi", ReturnValue: "數值"},
		{Name: "math_pi", Kind: BuiltinConstant, Summary: "用於需要圓周率的自訂數學轉換。", Example: "cycle = math_pi * 2", ReturnValue: "數值"},
		{Name: "open", Kind: BuiltinConstant, Summary: "用於判斷當根 K 線方向或估算開盤後價格變化。", Example: "green_bar = close > open", ReturnValue: "series"},
		{Name: "open_raw", Kind: BuiltinConstant, Summary: "用於讀取未復權開盤價。", Example: "gap_raw = open_raw / close_raw[1] - 1", ReturnValue: "series"},
		{Name: "order.buy", Kind: BuiltinConstant, Summary: "用於 order.* 函數表示買入方向。", Example: "order.market(order.buy, 100, note=\"enter\")", ReturnValue: "數值"},
		{Name: "order.sell", Kind: BuiltinConstant, Summary: "用於 order.* 函數表示賣出方向。", Example: "order.market(order.sell, 100, note=\"exit\")", ReturnValue: "數值"},
		{Name: "strategy.long", Kind: BuiltinConstant, Summary: "用於 strategy.entry 表示建立多頭部位。", Example: "strategy.entry(id=\"long\", direction=strategy.long, qty=1)", ReturnValue: "數值"},
		{Name: "strategy.short", Kind: BuiltinConstant, Summary: "用於 strategy.entry 表示建立空頭部位。", Example: "strategy.entry(id=\"short\", direction=strategy.short, qty=1)", ReturnValue: "數值"},
		{Name: "volume", Kind: BuiltinConstant, Summary: "用於成交量濾網、量能均線或流動性條件。", Example: "active_volume = volume > ta.sma(volume, 20)", ReturnValue: "series"},
	}
	return docs
}

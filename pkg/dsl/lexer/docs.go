package lexer

// LexicalDoc describes a lexer-level DSL rule that should appear in generated docs.
type LexicalDoc struct {
	Topic   string
	Syntax  string
	Example string
	Notes   string
}

// LexicalDocs returns user-facing notes for rules enforced by the lexer.
func LexicalDocs() []LexicalDoc {
	return []LexicalDoc{
		{
			Topic:   "單行註釋",
			Syntax:  "// ...",
			Example: "//@version=6\n// rebalance monthly",
			Notes:   "從 `//` 到行尾都會被忽略；`//@version=6` 也是註釋，保留給 Pine v6 風格相容。",
		},
		{
			Topic:   "多行註釋",
			Syntax:  "/* ... */",
			Example: "/*\nentry rules\nexit rules\n*/",
			Notes:   "可跨多行，直到第一個 `*/` 結束；目前不支援巢狀 block comment。",
		},
		{
			Topic:   "換行",
			Syntax:  "\\n",
			Example: "fast = ta.sma(close, 10)\nslow = ta.sma(close, 30)",
			Notes:   "換行是 statement separator；空格、tab、carriage return 會被略過。",
		},
		{
			Topic:   "續行",
			Syntax:  "\\ + newline",
			Example: "signal = close > open and \\\n    volume > ta.sma(volume, 20)",
			Notes:   "反斜線必須緊接換行，lexer 會移除該換行，讓下一行接續同一個 expression。",
		},
		{
			Topic:   "字串",
			Syntax:  "\"...\" 或 '...'",
			Example: "label = \"SMA\"\nside = 'long'",
			Notes:   "支援 `\\n`、`\\t`、`\\\\`、`\\'`、`\\\"` escape；字串不可直接跨換行。",
		},
		{
			Topic:   "數字",
			Syntax:  "int、float、scientific notation",
			Example: "risk = 0.02\nscale = 6.02e23\nwhole = 3.",
			Notes:   "整數、小數、科學記號都會被 token 化；`3.` 會被視為 float。",
		},
		{
			Topic:   "識別字",
			Syntax:  "letter 或 underscore 開頭，後續可含數字",
			Example: "_fast_len = 10\n信號 = close > open",
			Notes:   "使用 Unicode letter 判定，因此非 ASCII 變數名可被 lexer 接受；關鍵字會被轉成對應 token。",
		},
	}
}

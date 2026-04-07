package dslcatalog

func init() {
	const coveredCallSignalSample = `strategy(
  "covered-call-0330-tvsig-dsl-sample",
  signal_source="data/signals/covered_call_0330_tvsig/12h.txt,data/signals/covered_call_0330_tvsig/6h.txt",
  signal_name="entry_signal",
  signal_time_layout="Jan 2, 2006, 15:04",
  signal_timezone="UTC",
  signal_optional_index=true
)

plot(entry_signal, title="Entry Signal", precision=0)

if entry_signal == 1 {
  strategy.entry(id="signal_long", direction=strategy.long, qty=1)
}
`

	_ = RegisterDSL("covered-call-0330-tvsig-dsl-sample", coveredCallSignalSample)
}

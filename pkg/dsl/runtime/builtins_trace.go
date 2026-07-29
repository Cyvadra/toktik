package runtime

import (
	"strconv"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
)

const maxTraceDiagnostics = 10000
const traceTruncatedKey = "__trace_truncated__"

func RegisterTraceBuiltins(ip *Interpreter) {
	ip.RegisterBuiltinWithParams("trace.emit", []string{"stage", "symbol", "reason"}, func(args []Value) Value {
		if ip == nil || ip.Diagnostics == nil || len(args) < 3 {
			return NaVal()
		}
		if len(ip.traceKeys) >= maxTraceDiagnostics {
			if _, seen := ip.traceKeys[traceTruncatedKey]; !seen {
				ip.traceKeys[traceTruncatedKey] = struct{}{}
				barIndex := ip.BarIndex
				ip.Diagnostics.Add(diagnostics.Diagnostic{
					Severity: diagnostics.SeverityWarning,
					Code:     "dsl.trace.truncated",
					Message:  "trace diagnostics exceeded the configured limit",
					Function: "trace.emit",
					BarIndex: &barIndex,
				})
			}
			return NaVal()
		}
		stage := strings.TrimSpace(args[0].Str())
		symbol := strings.ToUpper(strings.TrimSpace(args[1].Str()))
		reason := strings.TrimSpace(args[2].Str())
		if stage == "" || reason == "" {
			return NaVal()
		}
		key := strconv.Itoa(ip.BarIndex) + "|" + stage + "|" + symbol + "|" + reason
		if _, seen := ip.traceKeys[key]; seen {
			return NaVal()
		}
		ip.traceKeys[key] = struct{}{}
		barIndex := ip.BarIndex
		ip.Diagnostics.Add(diagnostics.Diagnostic{
			Severity: diagnostics.SeverityInfo,
			Code:     "dsl.trace." + stage,
			Message:  strings.TrimSpace(stage + " " + symbol + ": " + reason),
			Function: "trace.emit",
			BarIndex: &barIndex,
		})
		return NaVal()
	})
}

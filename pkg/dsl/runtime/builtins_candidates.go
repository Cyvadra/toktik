package runtime

import (
	"math"
	"sort"
	"strings"

	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
)

type strategyCandidate struct {
	Symbol         string
	Score          float64
	SecondaryScore float64
	Payload        Value
}

// RegisterCandidateBuiltins exposes a small, deterministic candidate collection
// API for cross-sectional DSL selection without adding record or lambda syntax.
func RegisterCandidateBuiltins(ip *Interpreter) {
	ip.RegisterBuiltinWithParams("candidates.new", []string{"symbol", "score", "secondary_score", "payload"}, func(args []Value) Value {
		if len(args) < 2 {
			addCandidateDiagnostic(ip, "candidates.new", "requires symbol and score")
			return NaVal()
		}
		symbol := strings.ToUpper(strings.TrimSpace(args[0].Str()))
		score := args[1].Float()
		if symbol == "" || math.IsNaN(score) || math.IsInf(score, 0) {
			addCandidateDiagnostic(ip, "candidates.new", "requires a non-empty symbol and finite score")
			return NaVal()
		}
		secondaryScore := 0.0
		if len(args) >= 3 {
			secondaryScore = args[2].Float()
			if math.IsNaN(secondaryScore) || math.IsInf(secondaryScore, 0) {
				addCandidateDiagnostic(ip, "candidates.new", "secondary_score must be finite")
				return NaVal()
			}
		}
		payload := NaVal()
		if len(args) >= 4 {
			payload = args[3]
		}
		return ObjVal(strategyCandidate{Symbol: symbol, Score: score, SecondaryScore: secondaryScore, Payload: payload})
	})

	ip.RegisterBuiltinWithParams("candidates.add", []string{"items", "candidate"}, func(args []Value) Value {
		if len(args) < 2 {
			return ArrayVal(nil)
		}
		candidate, ok := candidateValue(args[1])
		if !ok {
			return ArrayVal(append([]Value(nil), args[0].Array()...))
		}
		items := append([]Value(nil), args[0].Array()...)
		items = append(items, ObjVal(candidate))
		return ArrayVal(items)
	})

	ip.RegisterBuiltinWithParams("candidates.sort", []string{"items", "direction"}, func(args []Value) Value {
		if len(args) < 1 {
			return ArrayVal(nil)
		}
		direction := "asc"
		if len(args) >= 2 {
			direction = strings.ToLower(strings.TrimSpace(args[1].Str()))
		}
		if direction != "asc" && direction != "desc" {
			addCandidateDiagnostic(ip, "candidates.sort", "direction must be asc or desc")
			return NaVal()
		}
		items, valid := candidateValues(args[0].Array())
		if !valid {
			addCandidateDiagnostic(ip, "candidates.sort", "items must contain only candidates")
			return NaVal()
		}
		descending := direction == "desc"
		sort.SliceStable(items, func(i, j int) bool {
			left, right := items[i], items[j]
			if left.Score != right.Score {
				if descending {
					return left.Score > right.Score
				}
				return left.Score < right.Score
			}
			if left.SecondaryScore != right.SecondaryScore {
				return left.SecondaryScore < right.SecondaryScore
			}
			return left.Symbol < right.Symbol
		})
		out := make([]Value, len(items))
		for index, candidate := range items {
			out[index] = ObjVal(candidate)
		}
		return ArrayVal(out)
	})

	ip.RegisterBuiltinWithParams("candidates.take", []string{"items", "count"}, func(args []Value) Value {
		if len(args) < 2 {
			return ArrayVal(nil)
		}
		countValue := args[1].Float()
		if math.IsNaN(countValue) || math.IsInf(countValue, 0) || countValue != math.Trunc(countValue) {
			addCandidateDiagnostic(ip, "candidates.take", "count must be a finite integer")
			return NaVal()
		}
		count := int(countValue)
		if count <= 0 {
			return ArrayVal(nil)
		}
		items := args[0].Array()
		if count > len(items) {
			count = len(items)
		}
		return ArrayVal(append([]Value(nil), items[:count]...))
	})

	ip.RegisterBuiltinWithParams("candidates.contains_symbol", []string{"items", "symbol"}, func(args []Value) Value {
		if len(args) < 2 {
			return BoolVal(false)
		}
		symbol := strings.ToUpper(strings.TrimSpace(args[1].Str()))
		candidates, valid := candidateValues(args[0].Array())
		if !valid {
			addCandidateDiagnostic(ip, "candidates.contains_symbol", "items must contain only candidates")
			return NaVal()
		}
		for _, candidate := range candidates {
			if candidate.Symbol == symbol {
				return BoolVal(true)
			}
		}
		return BoolVal(false)
	})

	ip.RegisterBuiltin("candidates.symbol", func(args []Value) Value {
		candidate, ok := firstCandidate(args)
		if !ok {
			return NaVal()
		}
		return StringVal(candidate.Symbol)
	})
	ip.RegisterBuiltin("candidates.score", func(args []Value) Value {
		candidate, ok := firstCandidate(args)
		if !ok {
			return NaVal()
		}
		return FloatVal(candidate.Score)
	})
	ip.RegisterBuiltin("candidates.secondary_score", func(args []Value) Value {
		candidate, ok := firstCandidate(args)
		if !ok {
			return NaVal()
		}
		return FloatVal(candidate.SecondaryScore)
	})
	ip.RegisterBuiltin("candidates.payload", func(args []Value) Value {
		candidate, ok := firstCandidate(args)
		if !ok {
			return NaVal()
		}
		return candidate.Payload
	})
}

func addCandidateDiagnostic(ip *Interpreter, function, message string) {
	if ip == nil || ip.Diagnostics == nil {
		return
	}
	barIndex := ip.BarIndex
	ip.Diagnostics.Add(diagnostics.Diagnostic{Severity: diagnostics.SeverityError, Code: "dsl.invalid_candidate_argument", Message: message, Function: function, BarIndex: &barIndex})
}

func firstCandidate(args []Value) (strategyCandidate, bool) {
	if len(args) == 0 {
		return strategyCandidate{}, false
	}
	return candidateValue(args[0])
}

func candidateValue(value Value) (strategyCandidate, bool) {
	candidate, ok := value.Obj().(strategyCandidate)
	return candidate, ok
}

func candidateValues(values []Value) ([]strategyCandidate, bool) {
	out := make([]strategyCandidate, 0, len(values))
	for _, value := range values {
		candidate, ok := candidateValue(value)
		if !ok {
			return nil, false
		}
		out = append(out, candidate)
	}
	return out, true
}

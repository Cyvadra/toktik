package runtime

import "math"

// RegisterTABuiltins adds Pine Script-style ta.* functions.
// These operate on the interpreter's series map.
func RegisterTABuiltins(ip *Interpreter) {
	// ta.sma(source, length)
	ip.RegisterBuiltin("ta.sma", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		src := args[0]
		length := int(args[1].Float())
		if length <= 0 {
			return NaVal()
		}
		s := src.SeriesPtr()
		if s == nil || s.Len() < length {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			sum += s.At(i)
		}
		return FloatVal(sum / float64(length))
	})

	// ta.ema(source, length)
	ip.RegisterBuiltin("ta.ema", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		src := args[0]
		length := int(args[1].Float())
		if length <= 0 {
			return NaVal()
		}
		s := src.SeriesPtr()
		if s == nil || s.Len() < 1 {
			return NaVal()
		}
		k := 2.0 / float64(length+1)
		data := s.Data()
		ema := data[0]
		for i := 1; i < len(data); i++ {
			ema = data[i]*k + ema*(1-k)
		}
		return FloatVal(ema)
	})

	// ta.rsi(source, length)
	ip.RegisterBuiltin("ta.rsi", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length+1 {
			return NaVal()
		}
		data := s.Data()
		n := len(data)
		avgGain, avgLoss := 0.0, 0.0
		start := n - length - 1
		if start < 0 {
			start = 0
		}
		for i := start + 1; i < n; i++ {
			diff := data[i] - data[i-1]
			if diff > 0 {
				avgGain += diff
			} else {
				avgLoss -= diff
			}
		}
		cnt := float64(n - start - 1)
		if cnt == 0 {
			return NaVal()
		}
		avgGain /= cnt
		avgLoss /= cnt
		if avgLoss == 0 {
			return FloatVal(100)
		}
		rs := avgGain / avgLoss
		return FloatVal(100 - 100/(1+rs))
	})

	// ta.atr(length) — uses high, low, close series from interpreter.
	ip.RegisterBuiltin("ta.atr", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		length := int(args[0].Float())
		if length <= 0 {
			return NaVal()
		}
		highS := ip.seriesMap["high"]
		lowS := ip.seriesMap["low"]
		closeS := ip.seriesMap["close"]
		if highS == nil || lowS == nil || closeS == nil {
			return NaVal()
		}
		if highS.Len() < length+1 {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			h := highS.At(i)
			l := lowS.At(i)
			c := closeS.At(i + 1)
			tr := math.Max(h-l, math.Max(math.Abs(h-c), math.Abs(l-c)))
			sum += tr
		}
		return FloatVal(sum / float64(length))
	})

	// ta.highest(source, length)
	ip.RegisterBuiltin("ta.highest", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length {
			return NaVal()
		}
		mx := math.Inf(-1)
		for i := 0; i < length; i++ {
			v := s.At(i)
			if v > mx {
				mx = v
			}
		}
		return FloatVal(mx)
	})

	// ta.lowest(source, length)
	ip.RegisterBuiltin("ta.lowest", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length {
			return NaVal()
		}
		mn := math.Inf(1)
		for i := 0; i < length; i++ {
			v := s.At(i)
			if v < mn {
				mn = v
			}
		}
		return FloatVal(mn)
	})

	// ta.stdev(source, length)
	ip.RegisterBuiltin("ta.stdev", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			sum += s.At(i)
		}
		mean := sum / float64(length)
		variance := 0.0
		for i := 0; i < length; i++ {
			d := s.At(i) - mean
			variance += d * d
		}
		return FloatVal(math.Sqrt(variance / float64(length)))
	})

	// ta.crossover(a, b) — true if a[0]>b[0] && a[1]<=b[1]
	ip.RegisterBuiltin("ta.crossover", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		a := args[0].SeriesPtr()
		b := args[1].SeriesPtr()
		if a == nil || b == nil || a.Len() < 2 || b.Len() < 2 {
			return BoolVal(false)
		}
		return BoolVal(a.At(0) > b.At(0) && a.At(1) <= b.At(1))
	})

	// ta.crossunder(a, b) — true if a[0]<b[0] && a[1]>=b[1]
	ip.RegisterBuiltin("ta.crossunder", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		a := args[0].SeriesPtr()
		b := args[1].SeriesPtr()
		if a == nil || b == nil || a.Len() < 2 || b.Len() < 2 {
			return BoolVal(false)
		}
		return BoolVal(a.At(0) < b.At(0) && a.At(1) >= b.At(1))
	})

	// ta.change(source, length=1)
	ip.RegisterBuiltin("ta.change", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := 1
		if len(args) >= 2 {
			length = int(args[1].Float())
		}
		if s == nil || s.Len() <= length {
			return NaVal()
		}
		return FloatVal(s.At(0) - s.At(length))
	})

	// ta.cum(source)
	ip.RegisterBuiltin("ta.cum", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		if s == nil || s.Len() == 0 {
			return NaVal()
		}
		sum := 0.0
		for _, v := range s.Data() {
			sum += v
		}
		return FloatVal(sum)
	})

	// ta.wma(source, length) — linearly-weighted moving average.
	ip.RegisterBuiltin("ta.wma", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length {
			return NaVal()
		}
		weightSum := 0.0
		valueSum := 0.0
		for i := 0; i < length; i++ {
			w := float64(length - i)
			valueSum += s.At(i) * w
			weightSum += w
		}
		return FloatVal(valueSum / weightSum)
	})

	// ta.bb(source, length, mult) — Bollinger Bands.
	// Returns an ArrayVal of [upper, basis, lower].
	ip.RegisterBuiltin("ta.bb", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		mult := args[2].Float()
		if s == nil || length <= 0 || s.Len() < length || math.IsNaN(mult) {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			sum += s.At(i)
		}
		basis := sum / float64(length)
		variance := 0.0
		for i := 0; i < length; i++ {
			d := s.At(i) - basis
			variance += d * d
		}
		std := math.Sqrt(variance / float64(length))
		upper := basis + mult*std
		lower := basis - mult*std
		return ArrayVal([]Value{FloatVal(upper), FloatVal(basis), FloatVal(lower)})
	})

	// ta.bb_upper(source, length, mult) — convenience for the upper band.
	ip.RegisterBuiltin("ta.bb_upper", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		mult := args[2].Float()
		if s == nil || length <= 0 || s.Len() < length || math.IsNaN(mult) {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			sum += s.At(i)
		}
		basis := sum / float64(length)
		variance := 0.0
		for i := 0; i < length; i++ {
			d := s.At(i) - basis
			variance += d * d
		}
		return FloatVal(basis + mult*math.Sqrt(variance/float64(length)))
	})

	// ta.bb_lower(source, length, mult) — convenience for the lower band.
	ip.RegisterBuiltin("ta.bb_lower", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		mult := args[2].Float()
		if s == nil || length <= 0 || s.Len() < length || math.IsNaN(mult) {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			sum += s.At(i)
		}
		basis := sum / float64(length)
		variance := 0.0
		for i := 0; i < length; i++ {
			d := s.At(i) - basis
			variance += d * d
		}
		return FloatVal(basis - mult*math.Sqrt(variance/float64(length)))
	})

	// ta.barssince(condition) — number of bars since condition was true (0 = current bar).
	// condition must be a Series; function scans history from offset 0 backward.
	ip.RegisterBuiltin("ta.barssince", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		if s == nil || s.Len() == 0 {
			return NaVal()
		}
		for i := 0; i < s.Len(); i++ {
			v := s.At(i)
			if !math.IsNaN(v) && v != 0 {
				return FloatVal(float64(i))
			}
		}
		return NaVal()
	})

	// ta.valuewhen(condition, source, occurrence) — value of source the n-th time condition was true.
	// occurrence=0 means most recent.
	ip.RegisterBuiltin("ta.valuewhen", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		cond := args[0].SeriesPtr()
		src := args[1].SeriesPtr()
		occurrence := int(args[2].Float())
		if cond == nil || src == nil {
			return NaVal()
		}
		count := 0
		maxLen := cond.Len()
		if src.Len() < maxLen {
			maxLen = src.Len()
		}
		for i := 0; i < maxLen; i++ {
			v := cond.At(i)
			if !math.IsNaN(v) && v != 0 {
				if count == occurrence {
					return FloatVal(src.At(i))
				}
				count++
			}
		}
		return NaVal()
	})

	// ta.percentrank(source, length) — percent rank of the current value in last length bars.
	ip.RegisterBuiltin("ta.percentrank", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length {
			return NaVal()
		}
		current := s.At(0)
		count := 0
		for i := 1; i < length; i++ {
			if s.At(i) <= current {
				count++
			}
		}
		return FloatVal(float64(count) / float64(length-1) * 100)
	})
}

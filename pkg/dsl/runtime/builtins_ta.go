package runtime

// When changing DSL builtin behavior here, update builtins_docs.go so generated DSL docs stay accurate.

import (
	"fmt"
	"math"
	"strings"
)

func taSeriesValue(ip *Interpreter, name string, value float64, args ...Value) Value {
	return ip.CaptureSeries(taSeriesKey(name, args...), value)
}

func taSeriesKey(name string, args ...Value) string {
	var key strings.Builder
	key.WriteString("__ta__:")
	key.WriteString(name)
	for _, arg := range args {
		switch arg.Tag() {
		case TagSeries:
			key.WriteString(":s:")
			key.WriteString(fmt.Sprintf("%p", arg.SeriesPtr()))
		case TagFloat:
			key.WriteString(":f:")
			key.WriteString(fmt.Sprintf("%.12g", arg.Float()))
		case TagBool:
			key.WriteString(":b:")
			key.WriteString(fmt.Sprintf("%t", arg.Bool()))
		case TagString:
			key.WriteString(":str:")
			key.WriteString(arg.Str())
		default:
			key.WriteString(":tag:")
			key.WriteString(fmt.Sprintf("%d", arg.Tag()))
		}
	}
	return key.String()
}

func previousCapturedValue(ip *Interpreter, key string) float64 {
	if ip == nil {
		return math.NaN()
	}
	series, ok := ip.seriesMap[key]
	if !ok || series.Len() == 0 {
		return math.NaN()
	}
	return series.Current()
}

func currentCapturedSeriesValue(ip *Interpreter, key string) (Value, bool) {
	if ip == nil || ip.BarIndex <= 0 {
		return Value{}, false
	}
	series, ok := ip.seriesMap[key]
	if !ok || series.Len() < ip.BarIndex {
		return Value{}, false
	}
	return SeriesVal(series), true
}

func captureRSIState(ip *Interpreter, value, avgGain, avgLoss float64, args ...Value) Value {
	_ = ip.CaptureSeries(taSeriesKey("ta.rsi.avgGain", args...), avgGain)
	_ = ip.CaptureSeries(taSeriesKey("ta.rsi.avgLoss", args...), avgLoss)
	return ip.CaptureSeries(taSeriesKey("ta.rsi", args...), value)
}

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
		return taSeriesValue(ip, "ta.sma", sum/float64(length), args...)
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
		return taSeriesValue(ip, "ta.ema", ema, args...)
	})

	// ta.rsi(source, length)
	ip.RegisterBuiltin("ta.rsi", func(args []Value) Value {
		if len(args) < 2 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		if s == nil || length <= 0 || s.Len() < length+1 {
			return captureRSIState(ip, math.NaN(), math.NaN(), math.NaN(), args...)
		}
		data := s.Data()
		n := len(data)
		rsiKey := taSeriesKey("ta.rsi", args...)
		if value, ok := currentCapturedSeriesValue(ip, rsiKey); ok {
			return value
		}

		avgGainKey := taSeriesKey("ta.rsi.avgGain", args...)
		avgLossKey := taSeriesKey("ta.rsi.avgLoss", args...)
		prevAvgGain := previousCapturedValue(ip, avgGainKey)
		prevAvgLoss := previousCapturedValue(ip, avgLossKey)

		avgGain, avgLoss := prevAvgGain, prevAvgLoss
		if n == length+1 || math.IsNaN(prevAvgGain) || math.IsNaN(prevAvgLoss) {
			avgGain, avgLoss = 0, 0
			validChanges := 0
			for i := 1; i <= length; i++ {
				if math.IsNaN(data[i]) || math.IsNaN(data[i-1]) {
					continue
				}
				diff := data[i] - data[i-1]
				if diff > 0 {
					avgGain += diff
				} else {
					avgLoss -= diff
				}
				validChanges++
			}
			if validChanges < length {
				return captureRSIState(ip, math.NaN(), math.NaN(), math.NaN(), args...)
			}
			avgGain /= float64(length)
			avgLoss /= float64(length)
		} else if math.IsNaN(data[n-1]) || math.IsNaN(data[n-2]) {
			return captureRSIState(ip, previousCapturedValue(ip, rsiKey), avgGain, avgLoss, args...)
		} else {
			diff := data[n-1] - data[n-2]
			gain, loss := 0.0, 0.0
			if diff > 0 {
				gain = diff
			} else {
				loss = -diff
			}
			avgGain = (avgGain*float64(length-1) + gain) / float64(length)
			avgLoss = (avgLoss*float64(length-1) + loss) / float64(length)
		}

		if avgLoss == 0 {
			return captureRSIState(ip, 100, avgGain, avgLoss, args...)
		}
		rs := avgGain / avgLoss
		return captureRSIState(ip, 100-100/(1+rs), avgGain, avgLoss, args...)
	})

	// ta.atr(length) or ta.atr(high, low, close, length)
	ip.RegisterBuiltin("ta.atr", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		var (
			highS  *Series
			lowS   *Series
			closeS *Series
			length int
		)
		if len(args) >= 4 {
			highS = args[0].SeriesPtr()
			lowS = args[1].SeriesPtr()
			closeS = args[2].SeriesPtr()
			length = int(args[3].Float())
		} else {
			length = int(args[0].Float())
			highS = ip.seriesMap["high"]
			lowS = ip.seriesMap["low"]
			closeS = ip.seriesMap["close"]
		}
		if length <= 0 || highS == nil || lowS == nil || closeS == nil || highS.Len() < length+1 || lowS.Len() < length+1 || closeS.Len() < length+1 {
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
		return taSeriesValue(ip, "ta.atr", sum/float64(length), args...)
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
		return taSeriesValue(ip, "ta.highest", mx, args...)
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
		return taSeriesValue(ip, "ta.lowest", mn, args...)
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
		return taSeriesValue(ip, "ta.stdev", math.Sqrt(variance/float64(length)), args...)
	})

	// ta.cci(length), ta.cci(source, length), or ta.cci(high, low, close, length)
	ip.RegisterBuiltin("ta.cci", func(args []Value) Value {
		if len(args) < 1 {
			return NaVal()
		}
		var (
			source []float64
			length int
		)
		if len(args) == 1 {
			length = int(args[0].Float())
			highS := ip.seriesMap["high"]
			lowS := ip.seriesMap["low"]
			closeS := ip.seriesMap["close"]
			if highS == nil || lowS == nil || closeS == nil || length <= 0 || highS.Len() < length || lowS.Len() < length || closeS.Len() < length {
				return NaVal()
			}
			source = make([]float64, length)
			for i := 0; i < length; i++ {
				source[i] = (highS.At(i) + lowS.At(i) + closeS.At(i)) / 3
			}
		} else if len(args) >= 4 {
			highS := args[0].SeriesPtr()
			lowS := args[1].SeriesPtr()
			closeS := args[2].SeriesPtr()
			length = int(args[3].Float())
			if highS == nil || lowS == nil || closeS == nil || length <= 0 || highS.Len() < length || lowS.Len() < length || closeS.Len() < length {
				return NaVal()
			}
			source = make([]float64, length)
			for i := 0; i < length; i++ {
				source[i] = (highS.At(i) + lowS.At(i) + closeS.At(i)) / 3
			}
		} else {
			s := args[0].SeriesPtr()
			length = int(args[1].Float())
			if s == nil || length <= 0 || s.Len() < length {
				return NaVal()
			}
			source = make([]float64, length)
			for i := 0; i < length; i++ {
				source[i] = s.At(i)
			}
		}
		if length <= 0 || len(source) < length {
			return NaVal()
		}
		sum := 0.0
		for i := 0; i < length; i++ {
			sum += source[i]
		}
		sma := sum / float64(length)
		meanDeviation := 0.0
		for i := 0; i < length; i++ {
			meanDeviation += math.Abs(source[i] - sma)
		}
		meanDeviation /= float64(length)
		if meanDeviation == 0 {
			return NaVal()
		}
		return taSeriesValue(ip, "ta.cci", (source[0]-sma)/(0.015*meanDeviation), args...)
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
		return taSeriesValue(ip, "ta.change", s.At(0)-s.At(length), args...)
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
		return taSeriesValue(ip, "ta.cum", sum, args...)
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
		return taSeriesValue(ip, "ta.wma", valueSum/weightSum, args...)
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
		return taSeriesValue(ip, "ta.bb_upper", basis+mult*math.Sqrt(variance/float64(length)), args...)
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
		return taSeriesValue(ip, "ta.bb_lower", basis-mult*math.Sqrt(variance/float64(length)), args...)
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
		return taSeriesValue(ip, "ta.percentrank", float64(count)/float64(length-1)*100, args...)
	})

	// ta.percentrank_valid(source, length, min_samples) — percent rank using only valid observations.
	ip.RegisterBuiltin("ta.percentrank_valid", func(args []Value) Value {
		if len(args) < 3 {
			return NaVal()
		}
		s := args[0].SeriesPtr()
		length := int(args[1].Float())
		minSamples := int(args[2].Float())
		if s == nil || length <= 0 || minSamples < 2 || s.Len() < length {
			return NaVal()
		}
		current := s.At(0)
		if math.IsNaN(current) {
			return NaVal()
		}
		valid, lessOrEqual := 1, 0
		for i := 1; i < length; i++ {
			value := s.At(i)
			if math.IsNaN(value) {
				continue
			}
			valid++
			if value <= current {
				lessOrEqual++
			}
		}
		if valid < minSamples {
			return NaVal()
		}
		return taSeriesValue(ip, "ta.percentrank_valid", float64(lessOrEqual)/float64(valid-1)*100, args...)
	})
}

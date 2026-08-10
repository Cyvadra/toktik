// Package optionsanalytics provides reusable analytics for option chains.
package optionsanalytics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	AlgorithmName              = "oi_weighted_five_point_iv_smile"
	AlgorithmVersion           = "v1"
	DefaultStrikeDistanceRatio = 0.20
)

var smoothingKernel = [...]float64{1, 4, 16, 4, 1}

// IVPoint is one option contract observation used to construct a smile.
type IVPoint struct {
	Expiration   time.Time
	OptionType   string
	Strike       float64
	IV           float64
	OpenInterest float64
}

// CurvePoint preserves the raw and smoothed IV at one strike.
type CurvePoint struct {
	Strike       float64
	RawIV        float64
	SmoothedIV   float64
	OpenInterest float64
}

// Curve is a same-expiry, same-option-type IV curve.
type Curve struct {
	OptionType       string
	Points           []CurvePoint
	PositiveOIPoints int
}

// ExpirationSmile contains the Call and Put curves for one expiry.
type ExpirationSmile struct {
	Expiration time.Time
	TotalOI    float64
	Call       Curve
	Put        Curve
}

// IVSmileSurface is the complete smile surface at one option-chain snapshot.
type IVSmileSurface struct {
	AlgorithmName       string
	AlgorithmVersion    string
	StrikeDistanceRatio float64
	Expirations         []ExpirationSmile
}

// BuildIVSmileSurface creates separately smoothed Call and Put curves for all
// valid expiration dates in points.
func BuildIVSmileSurface(points []IVPoint, maxStrikeDistanceRatio float64) (*IVSmileSurface, error) {
	if math.IsNaN(maxStrikeDistanceRatio) || math.IsInf(maxStrikeDistanceRatio, 0) || maxStrikeDistanceRatio < 0 || maxStrikeDistanceRatio > 1 {
		return nil, fmt.Errorf("max strike distance ratio must be between 0 and 1")
	}

	grouped := make(map[time.Time]map[string][]IVPoint)
	for _, point := range points {
		optionType := normalizeOptionType(point.OptionType)
		if optionType == "" || point.Expiration.IsZero() || !finite(point.Strike) || !finite(point.IV) {
			continue
		}
		point.OptionType = optionType
		point.Expiration = point.Expiration.UTC()
		if !finite(point.OpenInterest) || point.OpenInterest < 0 {
			point.OpenInterest = 0
		}
		if grouped[point.Expiration] == nil {
			grouped[point.Expiration] = make(map[string][]IVPoint)
		}
		grouped[point.Expiration][optionType] = append(grouped[point.Expiration][optionType], point)
	}

	expirations := make([]time.Time, 0, len(grouped))
	for expiration := range grouped {
		expirations = append(expirations, expiration)
	}
	sort.Slice(expirations, func(i, j int) bool { return expirations[i].Before(expirations[j]) })

	surface := &IVSmileSurface{
		AlgorithmName:       AlgorithmName,
		AlgorithmVersion:    AlgorithmVersion,
		StrikeDistanceRatio: maxStrikeDistanceRatio,
		Expirations:         make([]ExpirationSmile, 0, len(expirations)),
	}
	for _, expiration := range expirations {
		curves := grouped[expiration]
		call := buildCurve("call", curves["call"], maxStrikeDistanceRatio)
		put := buildCurve("put", curves["put"], maxStrikeDistanceRatio)
		if len(call.Points) == 0 && len(put.Points) == 0 {
			continue
		}
		surface.Expirations = append(surface.Expirations, ExpirationSmile{
			Expiration: expiration,
			TotalOI:    totalOI(call) + totalOI(put),
			Call:       call,
			Put:        put,
		})
	}
	return surface, nil
}

func buildCurve(optionType string, points []IVPoint, distanceRatio float64) Curve {
	sort.Slice(points, func(i, j int) bool { return points[i].Strike < points[j].Strike })
	curve := Curve{OptionType: optionType, Points: make([]CurvePoint, 0, len(points))}
	for i, center := range points {
		weightedIV, totalWeight := 0.0, 0.0
		for offset := -2; offset <= 2; offset++ {
			index := i + offset
			if index < 0 || index >= len(points) {
				continue
			}
			candidate := points[index]
			if math.Abs(candidate.Strike-center.Strike) > math.Abs(center.Strike)*distanceRatio {
				continue
			}
			weight := smoothingKernel[offset+2] * candidate.OpenInterest
			weightedIV += candidate.IV * weight
			totalWeight += weight
		}
		smoothedIV := center.IV
		if totalWeight > 0 {
			smoothedIV = weightedIV / totalWeight
		}
		if center.OpenInterest > 0 {
			curve.PositiveOIPoints++
		}
		curve.Points = append(curve.Points, CurvePoint{Strike: center.Strike, RawIV: center.IV, SmoothedIV: smoothedIV, OpenInterest: center.OpenInterest})
	}
	return curve
}

func totalOI(curve Curve) float64 {
	total := 0.0
	for _, point := range curve.Points {
		total += point.OpenInterest
	}
	return total
}

func normalizeOptionType(optionType string) string {
	switch strings.ToLower(strings.TrimSpace(optionType)) {
	case "call", "c":
		return "call"
	case "put", "p":
		return "put"
	default:
		return ""
	}
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

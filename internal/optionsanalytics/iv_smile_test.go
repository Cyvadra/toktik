package optionsanalytics

import (
	"math"
	"testing"
	"time"
)

func TestBuildIVSmileSurfaceAppliesOIKernelAndSeparatesOptionTypes(t *testing.T) {
	expiration := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	surface, err := BuildIVSmileSurface([]IVPoint{
		{Expiration: expiration, OptionType: "call", Strike: 80, IV: 0.20, OpenInterest: 1},
		{Expiration: expiration, OptionType: "call", Strike: 90, IV: 0.30, OpenInterest: 1},
		{Expiration: expiration, OptionType: "call", Strike: 100, IV: 0.40, OpenInterest: 1},
		{Expiration: expiration, OptionType: "call", Strike: 110, IV: 0.50, OpenInterest: 1},
		{Expiration: expiration, OptionType: "call", Strike: 120, IV: 0.60, OpenInterest: 1},
		{Expiration: expiration, OptionType: "put", Strike: 100, IV: 0.90, OpenInterest: 5},
	}, DefaultStrikeDistanceRatio)
	if err != nil {
		t.Fatal(err)
	}
	if len(surface.Expirations) != 1 {
		t.Fatalf("expiration count = %d, want 1", len(surface.Expirations))
	}
	call := surface.Expirations[0].Call
	if got, want := call.Points[2].SmoothedIV, 0.40; math.Abs(got-want) > 1e-12 {
		t.Fatalf("center smoothed IV = %v, want %v", got, want)
	}
	if got, want := call.Points[0].SmoothedIV, (0.20*16+0.30*4)/20; math.Abs(got-want) > 1e-12 {
		t.Fatalf("edge smoothed IV = %v, want %v", got, want)
	}
	if got := surface.Expirations[0].Put.Points[0].SmoothedIV; got != 0.90 {
		t.Fatalf("put smoothed IV = %v, want 0.9", got)
	}
}

func TestBuildIVSmileSurfaceHonorsDistanceAndZeroOIBehavior(t *testing.T) {
	expiration := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	surface, err := BuildIVSmileSurface([]IVPoint{
		{Expiration: expiration, OptionType: "call", Strike: 100, IV: 0.20, OpenInterest: 10},
		{Expiration: expiration, OptionType: "call", Strike: 120, IV: 0.80, OpenInterest: 10},
		{Expiration: expiration, OptionType: "call", Strike: 130, IV: 0.95, OpenInterest: 10},
		{Expiration: expiration, OptionType: "put", Strike: 100, IV: 0.55, OpenInterest: 0},
		{Expiration: expiration, OptionType: "put", Strike: 105, IV: 0.65, OpenInterest: -3},
		{Expiration: expiration, OptionType: "call", Strike: 110, IV: math.NaN(), OpenInterest: 1},
	}, DefaultStrikeDistanceRatio)
	if err != nil {
		t.Fatal(err)
	}
	smile := surface.Expirations[0]
	if got, want := smile.Call.Points[0].SmoothedIV, (0.20*16+0.80*4)/20; math.Abs(got-want) > 1e-12 {
		t.Fatalf("20 percent boundary should be included: got %v, want %v", got, want)
	}
	if len(smile.Call.Points) != 3 {
		t.Fatalf("call point count = %d, want 3 after filtering NaN", len(smile.Call.Points))
	}
	for _, point := range smile.Put.Points {
		if point.SmoothedIV != point.RawIV {
			t.Fatalf("zero-OI put point = %+v, want raw IV fallback", point)
		}
	}
}

func TestBuildIVSmileSurfaceRejectsInvalidDistanceRatio(t *testing.T) {
	if _, err := BuildIVSmileSurface(nil, 1.01); err == nil {
		t.Fatal("expected invalid ratio error")
	}
}

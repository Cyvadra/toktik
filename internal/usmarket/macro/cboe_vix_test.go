package macro

import (
	"testing"
	"time"

	cbvix "github.com/Cyvadra/toktik/pkg/cboe/vix"
)

func TestBuildCBOEVIXObservationRowsUsesNewYorkClose(t *testing.T) {
	rows := buildCBOEVIXObservationRows(DefaultCBOEVIXDataset, []cbvix.Bar{{
		Date:  time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC),
		Open:  20,
		High:  21,
		Low:   19,
		Close: 20.5,
	}}, "SPY", CBOEVIXConfig{})
	if len(rows) != 4 {
		t.Fatalf("len(rows)=%d want 4", len(rows))
	}
	wantTS := time.Date(2026, 5, 22, 20, 0, 0, 0, time.UTC)
	for _, row := range rows {
		if !row.EventTS.Equal(wantTS) {
			t.Fatalf("EventTS=%s want %s", row.EventTS, wantTS)
		}
		if !row.KnownAt.Equal(wantTS) {
			t.Fatalf("KnownAt=%s want %s", row.KnownAt, wantTS)
		}
		if row.ReferenceSymbol != "SPY" {
			t.Fatalf("ReferenceSymbol=%s want SPY", row.ReferenceSymbol)
		}
	}
}

func TestBuildCBOEVIXCatalogRows(t *testing.T) {
	rows := buildCBOEVIXCatalogRows(DefaultCBOEVIXDataset, "SPY")
	if len(rows) != 4 {
		t.Fatalf("len(rows)=%d want 4", len(rows))
	}
	if rows[0].Dataset != DefaultCBOEVIXDataset {
		t.Fatalf("Dataset=%s want %s", rows[0].Dataset, DefaultCBOEVIXDataset)
	}
	if rows[0].PreferredFrequency != defaultDailyFrequency {
		t.Fatalf("PreferredFrequency=%s want %s", rows[0].PreferredFrequency, defaultDailyFrequency)
	}
	if rows[0].Source != DefaultCBOEVIXSource {
		t.Fatalf("Source=%s want %s", rows[0].Source, DefaultCBOEVIXSource)
	}
}

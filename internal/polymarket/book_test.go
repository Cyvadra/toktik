package polymarket

import (
	"errors"
	"reflect"
	"testing"
)

func TestBookSnapshotAndPriceChanges(t *testing.T) {
	book := NewBook()
	err := book.Apply(Event{
		Type:     EventBook,
		BidsJSON: `[["0.01","62"],["0.02","14.03"],["0.03","49.56"]]`,
		AsksJSON: `[["0.99","130.5"],["0.98","57.16"],["0.70","111.44"]]`,
	})
	if err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}

	if got, ok := book.BestBid(); !ok || got != (Level{Price: 300, Size: 49_560_000}) {
		t.Fatalf("unexpected best bid: %+v ok=%v", got, ok)
	}
	if got, ok := book.BestAsk(); !ok || got != (Level{Price: 7_000, Size: 111_440_000}) {
		t.Fatalf("unexpected best ask: %+v ok=%v", got, ok)
	}

	if err := book.Apply(Event{Type: EventPriceChange, Side: SideBid, Price: 400, Size: 12_000_000}); err != nil {
		t.Fatalf("add bid: %v", err)
	}
	if err := book.Apply(Event{Type: EventPriceChange, Side: SideAsk, Price: 7_000, Size: 0}); err != nil {
		t.Fatalf("delete ask: %v", err)
	}

	if got, _ := book.BestBid(); got.Price != 400 {
		t.Fatalf("best bid after update = %d, want 400", got.Price)
	}
	if got, _ := book.BestAsk(); got.Price != 9_800 {
		t.Fatalf("best ask after delete = %d, want 9800", got.Price)
	}
}

func TestBookRequiresSnapshotBeforeDelta(t *testing.T) {
	book := NewBook()
	err := book.Apply(Event{Type: EventPriceChange, Side: SideBid, Price: 100, Size: 1})
	if !errors.Is(err, ErrBookNotInitialized) {
		t.Fatalf("expected ErrBookNotInitialized, got %v", err)
	}
}

func TestBookValidatesPublishedBestQuotesAtBoundary(t *testing.T) {
	book := NewBook()
	if err := book.Apply(Event{Type: EventBook, BidsJSON: `[["0.40","1"]]`, AsksJSON: `[["0.60","1"]]`}); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	if err := book.Apply(Event{Type: EventPriceChange, Side: SideBid, Price: 5_000, Size: 1_000_000}); err != nil {
		t.Fatalf("apply price change: %v", err)
	}
	err := book.ValidatePublishedQuotes(4_000, true, 0, false)
	if err == nil {
		t.Fatal("expected published quote mismatch")
	}
}

func TestLevelsReturnBestFirst(t *testing.T) {
	book := NewBook()
	if err := book.Apply(Event{Type: EventBook, BidsJSON: `[["0.10","1"],["0.30","3"],["0.20","2"]]`, AsksJSON: `[["0.80","8"],["0.60","6"],["0.70","7"]]`}); err != nil {
		t.Fatalf("apply snapshot: %v", err)
	}
	if got, want := book.Levels(SideBid), []Level{{Price: 3_000, Size: 3_000_000}, {Price: 2_000, Size: 2_000_000}, {Price: 1_000, Size: 1_000_000}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bid levels = %+v, want %+v", got, want)
	}
	if got, want := book.Levels(SideAsk), []Level{{Price: 6_000, Size: 6_000_000}, {Price: 7_000, Size: 7_000_000}, {Price: 8_000, Size: 8_000_000}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ask levels = %+v, want %+v", got, want)
	}
}

func TestParseFixedRejectsExcessPrecision(t *testing.T) {
	if _, err := ParseFixed("0.12345", PriceScale); err == nil {
		t.Fatal("expected excess precision error")
	}
	if got, err := ParseFixed("14.03", SizeScale); err != nil || got != 14_030_000 {
		t.Fatalf("ParseFixed = %d, %v", got, err)
	}
}

package optutil

import (
	"strconv"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// PendingRefCounter generates unique spread reference strings for
// scheduled (deferred) spread opens. Embed it in your strategy struct.
type PendingRefCounter struct {
	Prefix string
	nextID int
}

// Next returns a unique reference string incorporating the given tag.
// Example output: "MyStrategy/short-put/3"
func (p *PendingRefCounter) Next(tag string) string {
	p.nextID++
	return p.Prefix + "/" + tag + "/" + strconv.Itoa(p.nextID)
}

// FindSpreadByRef searches the tracker for a spread matching the given ref.
// Returns nil when no match is found.
func FindSpreadByRef(tracker *backtest.SpreadTracker, ref string) *backtest.SpreadPosition {
	if tracker == nil || ref == "" {
		return nil
	}
	for _, sp := range tracker.All() {
		if sp != nil && sp.Ref == ref {
			return sp
		}
	}
	return nil
}

package backtest

import "time"

type ProgressPhase string

const (
	ProgressPhasePrepare ProgressPhase = "prepare"
	ProgressPhaseReplay  ProgressPhase = "replay"
)

type ProgressUpdate struct {
	Phase     ProgressPhase
	Current   int
	Total     int
	Message   string
	StartedAt time.Time
	Completed bool
	Timestamp time.Time
}

type ProgressFunc func(ProgressUpdate)

func emitProgress(fn ProgressFunc, update ProgressUpdate) {
	if fn == nil {
		return
	}
	if update.Timestamp.IsZero() {
		update.Timestamp = time.Now()
	}
	fn(update)
}

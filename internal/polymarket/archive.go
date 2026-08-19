package polymarket

import (
	"fmt"
	"time"
)

const syntheticSourcePrefix = "_synthetic:"

type ArchiveProcessorStats struct {
	RowsMatched              uint64
	RowsInserted             uint64
	SyntheticBooksInserted   uint64
	PreInitializationSkipped uint64
	LateRowsSkipped          uint64
}

type RawFileRef struct {
	Path        string
	Name        string
	Fingerprint string
	SizeBytes   int64
	Warmup      bool
	Committed   bool
	Replace     bool
}

type ArchiveProgressReporter interface {
	StartUnitProgress(description, unit string, total int)
	AdvanceUnitProgress(description string, completed int)
}

type ArchiveImportOptions struct {
	BatchSize     int
	DryRun        bool
	SelectionHash string
	Horizon       time.Duration
	Progress      ArchiveProgressReporter
}

type ArchiveImportResult struct {
	FilesWarmed              uint64
	FilesProcessed           uint64
	FilesSkipped             uint64
	RowsScanned              uint64
	RowsInserted             uint64
	SyntheticBooksInserted   uint64
	PreInitializationSkipped uint64
	LateRowsSkipped          uint64
	ConditionsInserted       uint64
	OutcomesInserted         uint64
}

type archiveBookState struct {
	conditionID string
	book        *Book
	emitted     bool
}

type ArchiveProcessor struct {
	catalog *ConditionCatalog
	horizon time.Duration
	books   map[string]*archiveBookState
	stats   ArchiveProcessorStats
}

func NewArchiveProcessor(catalog *ConditionCatalog, horizon time.Duration) *ArchiveProcessor {
	return &ArchiveProcessor{catalog: catalog, horizon: horizon, books: make(map[string]*archiveBookState)}
}

func (processor *ArchiveProcessor) clone() *ArchiveProcessor {
	clone := &ArchiveProcessor{
		catalog: processor.catalog,
		horizon: processor.horizon,
		books:   make(map[string]*archiveBookState, len(processor.books)),
		stats:   processor.stats,
	}
	for key, state := range processor.books {
		clone.books[key] = &archiveBookState{
			conditionID: state.conditionID,
			book:        cloneBook(state.book),
			emitted:     state.emitted,
		}
	}
	return clone
}

func cloneBook(book *Book) *Book {
	clone := &Book{
		initialized: book.initialized,
		tickSize:    book.tickSize,
		bids:        make(map[int64]int64, len(book.bids)),
		asks:        make(map[int64]int64, len(book.asks)),
	}
	for price, size := range book.bids {
		clone.bids[price] = size
	}
	for price, size := range book.asks {
		clone.asks[price] = size
	}
	return clone
}

func (processor *ArchiveProcessor) Process(event RawEvent, writable bool) ([]RawEvent, error) {
	return processor.process(event, writable, false)
}

func (processor *ArchiveProcessor) ReplayCommitted(event RawEvent) error {
	_, err := processor.process(event, false, true)
	return err
}

func (processor *ArchiveProcessor) process(event RawEvent, writable, committed bool) ([]RawEvent, error) {
	meta, ok := processor.catalog.Conditions[event.ConditionID]
	if !ok {
		return nil, nil
	}
	processor.stats.RowsMatched++
	if event.Key.ReceivedTime.After(meta.WindowEnd.Add(processor.horizon)) {
		delete(processor.books, archiveBookKey(event.ConditionID, event.AssetID))
		processor.stats.LateRowsSkipped++
		return nil, nil
	}

	key := archiveBookKey(event.ConditionID, event.AssetID)
	state := processor.books[key]
	if state == nil {
		state = &archiveBookState{conditionID: event.ConditionID, book: NewBook()}
		processor.books[key] = state
	}
	replay, err := event.ReplayEvent()
	if err != nil {
		return nil, err
	}
	inWindow := eventWithinConditionWindow(event, meta)
	if inWindow && committed && (state.book.Initialized() || event.Type == EventBook) {
		state.emitted = true
	}
	if inWindow && writable && !state.emitted {
		if event.Type == EventBook {
			if err := state.book.Apply(replay); err != nil {
				return nil, err
			}
			state.emitted = true
			processor.stats.RowsInserted++
			return []RawEvent{event}, nil
		}
		if !state.book.Initialized() {
			processor.stats.PreInitializationSkipped++
			return nil, nil
		}
		synthetic, err := processor.syntheticBook(event, meta, state.book)
		if err != nil {
			return nil, err
		}
		if err := state.book.Apply(replay); err != nil {
			return nil, err
		}
		state.emitted = true
		processor.stats.SyntheticBooksInserted++
		processor.stats.RowsInserted += 2
		return []RawEvent{synthetic, event}, nil
	}

	if err := state.book.Apply(replay); err != nil {
		if err == ErrBookNotInitialized {
			processor.stats.PreInitializationSkipped++
			return nil, nil
		}
		return nil, err
	}
	if inWindow && writable && state.emitted {
		processor.stats.RowsInserted++
		return []RawEvent{event}, nil
	}
	return nil, nil
}

func (processor *ArchiveProcessor) Stats() ArchiveProcessorStats { return processor.stats }

func (processor *ArchiveProcessor) AdvanceWatermark(receivedAt time.Time) {
	for key, state := range processor.books {
		meta, ok := processor.catalog.Conditions[state.conditionID]
		if ok && receivedAt.After(meta.WindowEnd.Add(processor.horizon)) {
			delete(processor.books, key)
		}
	}
}

func (processor *ArchiveProcessor) ActiveBooks() int { return len(processor.books) }

func (processor *ArchiveProcessor) InitializedStreams() int {
	count := 0
	for _, state := range processor.books {
		if state.emitted {
			count++
		}
	}
	return count
}

func (processor *ArchiveProcessor) syntheticBook(trigger RawEvent, meta ConditionMeta, book *Book) (RawEvent, error) {
	snapshot, err := book.SnapshotEvent()
	if err != nil {
		return RawEvent{}, err
	}
	source := fmt.Sprintf("%s%s:%s:%d", syntheticSourcePrefix, trigger.ConditionID, trigger.AssetID, meta.WindowStart.UnixMilli())
	return RawEvent{
		Key: EventKey{
			ExchangeTime: meta.WindowStart,
			ReceivedTime: trigger.Key.ReceivedTime,
			SourceFile:   source,
		},
		ConditionID: trigger.ConditionID,
		AssetID:     trigger.AssetID,
		Type:        EventBook,
		BidsJSON:    NullableString{Value: snapshot.BidsJSON, Valid: true},
		AsksJSON:    NullableString{Value: snapshot.AsksJSON, Valid: true},
		NewTickSizeE4: NullableInt64{
			Value: snapshot.NewTickSize,
			Valid: snapshot.NewTickSize > 0,
		},
	}, nil
}

func archiveBookKey(conditionID, assetID string) string { return conditionID + "\x00" + assetID }

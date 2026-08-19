package polymarket

import (
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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
	Path          string
	Name          string
	Fingerprint   string
	SizeBytes     int64
	Warmup        bool
	Committed     bool
	Replace       bool
	StagePath     string
	StageRows     uint64
	StageCacheHit bool
	StageWait     time.Duration
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
	EventConns    []driver.Conn
	FileCompleted func(ArchiveFileMetrics)
	StageRoot     string
	StageWorkers  int
}

type ArchiveFileStatus string

const (
	ArchiveFileWarmed   ArchiveFileStatus = "warmed"
	ArchiveFileImported ArchiveFileStatus = "imported"
	ArchiveFileSkipped  ArchiveFileStatus = "skipped"
)

type ArchiveFileMetrics struct {
	Name          string
	Status        ArchiveFileStatus
	SizeBytes     int64
	SourceRows    uint64
	SelectedRows  uint64
	WriterBatches uint64
	WriterWait    time.Duration
	Elapsed       time.Duration
	StageCacheHit bool
	StageWait     time.Duration
}

func (metrics ArchiveFileMetrics) MiBPerSecond() float64 {
	if metrics.Elapsed <= 0 {
		return 0
	}
	return float64(metrics.SizeBytes) / (1024 * 1024) / metrics.Elapsed.Seconds()
}

func (metrics ArchiveFileMetrics) RowsPerSecond() float64 {
	if metrics.Elapsed <= 0 {
		return 0
	}
	return float64(metrics.SourceRows) / metrics.Elapsed.Seconds()
}

func (metrics ArchiveFileMetrics) WriterWaitRatio() float64 {
	if metrics.Elapsed <= 0 {
		return 0
	}
	return float64(metrics.WriterWait) / float64(metrics.Elapsed)
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
	WriterBatches            uint64
	WriterWait               time.Duration
	Elapsed                  time.Duration
}

type archiveBookState struct {
	conditionID string
	book        *Book
	emitted     bool
}

type ArchiveProcessor struct {
	catalog   *ConditionCatalog
	horizon   time.Duration
	books     map[string]*archiveBookState
	stats     ArchiveProcessorStats
	undo      map[string]*archiveBookState
	undoStats ArchiveProcessorStats
}

func NewArchiveProcessor(catalog *ConditionCatalog, horizon time.Duration) *ArchiveProcessor {
	return &ArchiveProcessor{catalog: catalog, horizon: horizon, books: make(map[string]*archiveBookState)}
}

func (processor *ArchiveProcessor) BeginFile() {
	processor.undo = make(map[string]*archiveBookState)
	processor.undoStats = processor.stats
}

func (processor *ArchiveProcessor) CommitFile() {
	processor.undo = nil
}

func (processor *ArchiveProcessor) RollbackFile() {
	for key, previous := range processor.undo {
		if previous == nil {
			delete(processor.books, key)
			continue
		}
		processor.books[key] = previous
	}
	processor.stats = processor.undoStats
	processor.undo = nil
}

func (processor *ArchiveProcessor) beforeMutate(key string) {
	if processor.undo == nil {
		return
	}
	if _, recorded := processor.undo[key]; recorded {
		return
	}
	state := processor.books[key]
	if state == nil {
		processor.undo[key] = nil
		return
	}
	processor.undo[key] = &archiveBookState{
		conditionID: state.conditionID,
		book:        cloneBook(state.book),
		emitted:     state.emitted,
	}
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
		processor.beforeMutate(key)
		state = &archiveBookState{conditionID: event.ConditionID, book: NewBook()}
		processor.books[key] = state
	}
	inWindow := eventWithinConditionWindow(event, meta)
	if inWindow && committed && (state.book.Initialized() || event.Type == EventBook) {
		processor.beforeMutate(key)
		state.emitted = true
	}
	if state.emitted {
		if inWindow && writable {
			processor.stats.RowsInserted++
			return []RawEvent{event}, nil
		}
		return nil, nil
	}

	replay, err := event.ReplayEvent()
	if err != nil {
		return nil, err
	}
	if inWindow && writable && !state.emitted {
		if event.Type == EventBook {
			processor.beforeMutate(key)
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
		processor.beforeMutate(key)
		if err := state.book.Apply(replay); err != nil {
			return nil, err
		}
		state.emitted = true
		processor.stats.SyntheticBooksInserted++
		processor.stats.RowsInserted += 2
		return []RawEvent{synthetic, event}, nil
	}

	processor.beforeMutate(key)
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

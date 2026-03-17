package thetadata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	stateStatusInflight  = "inflight"
	stateStatusFailed    = "failed"
	stateStatusCompleted = "completed"
)

type SyncState struct {
	Root                string        `json:"root"`
	Date                string        `json:"date"`
	Status              string        `json:"status"`
	Stage               string        `json:"stage"`
	Attempt             int           `json:"attempt"`
	LastError           string        `json:"last_error,omitempty"`
	ExpectedContracts   int           `json:"expected_contracts,omitempty"`
	DownloadedContracts int           `json:"downloaded_contracts,omitempty"`
	ExpectedBars        int           `json:"expected_bars,omitempty"`
	StoredBars          int           `json:"stored_bars,omitempty"`
	ExpectedSpotBars    int           `json:"expected_spot_bars,omitempty"`
	StoredSpotBars      int           `json:"stored_spot_bars,omitempty"`
	StartedAt           time.Time     `json:"started_at,omitempty"`
	CompletedAt         time.Time     `json:"completed_at,omitempty"`
	UpdatedAt           time.Time     `json:"updated_at"`
	Duration            time.Duration `json:"duration,omitempty"`
}

// Progress tracks sync state per (root, date) using structured, date-sharded files.
type Progress struct {
	dir       string
	completed map[string]bool
	inflight  map[string]bool
	states    map[string]SyncState
	mu        sync.Mutex
}

func NewProgress(dir string) (*Progress, error) {
	if err := os.MkdirAll(filepath.Join(dir, "states"), 0755); err != nil {
		return nil, fmt.Errorf("create progress dir: %w", err)
	}
	p := &Progress{
		dir:       dir,
		completed: make(map[string]bool),
		inflight:  make(map[string]bool),
		states:    make(map[string]SyncState),
	}
	if err := p.loadStates(); err != nil {
		return nil, err
	}
	return p, nil
}

func makeKey(root string, date time.Time) string {
	return root + "\t" + date.Format("2006-01-02")
}

func sanitizeRoot(root string) string {
	replacer := strings.NewReplacer("/", "_", `\\`, "_", ":", "_", "\t", "_", " ", "_")
	return replacer.Replace(root)
}

func (p *Progress) statesDir() string {
	return filepath.Join(p.dir, "states")
}

func (p *Progress) stateFilePath(root string, date time.Time) string {
	return filepath.Join(p.statesDir(), date.Format("2006-01-02"), sanitizeRoot(root)+".json")
}

func (p *Progress) loadStates() error {
	return filepath.WalkDir(p.statesDir(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read state file %s: %w", path, err)
		}
		var state SyncState
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("parse state file %s: %w", path, err)
		}

		date, err := time.Parse("2006-01-02", state.Date)
		if err != nil {
			return fmt.Errorf("parse state date %s: %w", state.Date, err)
		}
		key := makeKey(state.Root, date)
		p.states[key] = state
		switch state.Status {
		case stateStatusCompleted:
			p.completed[key] = true
		case stateStatusInflight, stateStatusFailed:
			p.inflight[key] = true
		}
		return nil
	})
}

func (p *Progress) writeStateLocked(state SyncState) error {
	date, err := time.Parse("2006-01-02", state.Date)
	if err != nil {
		return fmt.Errorf("parse state date: %w", err)
	}
	stateDir := filepath.Dir(p.stateFilePath(state.Root, date))
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(p.stateFilePath(state.Root, date), append(payload, '\n'), 0644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	key := makeKey(state.Root, date)
	p.states[key] = state
	delete(p.completed, key)
	delete(p.inflight, key)
	switch state.Status {
	case stateStatusCompleted:
		p.completed[key] = true
	case stateStatusInflight, stateStatusFailed:
		p.inflight[key] = true
	}
	return nil
}

func (p *Progress) IsCompleted(root string, date time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed[makeKey(root, date)]
}

func (p *Progress) IsInFlight(root string, date time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inflight[makeKey(root, date)]
}

func (p *Progress) State(root string, date time.Time) (SyncState, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.states[makeKey(root, date)]
	return state, ok
}

func (p *Progress) MarkStarted(root string, date time.Time, expectedContracts int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := makeKey(root, date)
	state := p.states[key]
	state.Root = root
	state.Date = date.Format("2006-01-02")
	state.Status = stateStatusInflight
	state.Stage = "started"
	state.Attempt++
	state.ExpectedContracts = expectedContracts
	state.LastError = ""
	now := time.Now().UTC()
	state.StartedAt = now
	state.CompletedAt = time.Time{}
	state.UpdatedAt = now
	state.Duration = 0
	return p.writeStateLocked(state)
}

func (p *Progress) MarkDownloadProgress(root string, date time.Time, downloadedContracts int, expectedBars int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := makeKey(root, date)
	state := p.states[key]
	state.Root = root
	state.Date = date.Format("2006-01-02")
	state.Status = stateStatusInflight
	state.Stage = "downloaded"
	state.DownloadedContracts = downloadedContracts
	state.ExpectedBars = expectedBars
	state.UpdatedAt = time.Now().UTC()
	return p.writeStateLocked(state)
}

func (p *Progress) MarkFailed(root string, date time.Time, stage string, err error, stats DateSyncStats) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := makeKey(root, date)
	state := p.states[key]
	state.Root = root
	state.Date = date.Format("2006-01-02")
	state.Status = stateStatusFailed
	state.Stage = stage
	state.LastError = err.Error()
	state.ExpectedContracts = stats.ExpectedContracts
	state.DownloadedContracts = stats.DownloadedContracts
	state.ExpectedBars = stats.ExpectedBars
	state.StoredBars = stats.StoredBars
	state.ExpectedSpotBars = stats.ExpectedSpotBars
	state.StoredSpotBars = stats.StoredSpotBars
	state.UpdatedAt = time.Now().UTC()
	return p.writeStateLocked(state)
}

func (p *Progress) MarkCompleted(root string, date time.Time, stats DateSyncStats) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := makeKey(root, date)
	state := p.states[key]
	state.Root = root
	state.Date = date.Format("2006-01-02")
	state.Status = stateStatusCompleted
	state.Stage = "completed"
	state.LastError = ""
	state.ExpectedContracts = stats.ExpectedContracts
	state.DownloadedContracts = stats.DownloadedContracts
	state.ExpectedBars = stats.ExpectedBars
	state.StoredBars = stats.StoredBars
	state.ExpectedSpotBars = stats.ExpectedSpotBars
	state.StoredSpotBars = stats.StoredSpotBars
	now := time.Now().UTC()
	state.CompletedAt = now
	state.UpdatedAt = now
	if !state.StartedAt.IsZero() {
		state.Duration = now.Sub(state.StartedAt)
	}
	return p.writeStateLocked(state)
}

func (p *Progress) CompletedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.completed)
}

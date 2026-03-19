package thetadata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Progress tracks per-(root, date) sync state on disk for resumability.
type Progress struct {
	dir       string
	mu        sync.Mutex
	completed map[string]bool       // key → true
	states    map[string]*SyncState // key → state
}

// NewProgress loads existing state from disk.
func NewProgress(dir string) (*Progress, error) {
	p := &Progress{
		dir:       dir,
		completed: make(map[string]bool),
		states:    make(map[string]*SyncState),
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

func stateKey(root, date string) string {
	return root + "\t" + date
}

func (p *Progress) load() error {
	statesDir := filepath.Join(p.dir, "states")
	if _, err := os.Stat(statesDir); os.IsNotExist(err) {
		return nil
	}

	dateDirs, err := os.ReadDir(statesDir)
	if err != nil {
		return fmt.Errorf("read progress dir: %w", err)
	}

	for _, dd := range dateDirs {
		if !dd.IsDir() {
			continue
		}
		dateDir := filepath.Join(statesDir, dd.Name())
		files, err := os.ReadDir(dateDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dateDir, f.Name()))
			if err != nil {
				continue
			}
			var s SyncState
			if err := json.Unmarshal(data, &s); err != nil {
				continue
			}
			key := stateKey(s.Root, s.Date)
			p.states[key] = &s
			if s.Status == "completed" {
				p.completed[key] = true
			}
		}
	}
	return nil
}

// IsCompleted returns true if the (root, date) has been fully processed.
func (p *Progress) IsCompleted(root, date string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed[stateKey(root, date)]
}

// CompletedCount returns the number of completed (root, date) pairs.
func (p *Progress) CompletedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.completed)
}

// MarkStarted records that processing has begun for a (root, date).
func (p *Progress) MarkStarted(root, date string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := stateKey(root, date)
	s, ok := p.states[key]
	if !ok {
		s = &SyncState{Root: root, Date: date}
		p.states[key] = s
	}
	s.Status = "started"
	s.Attempt++
	s.StartedAt = time.Now().UTC().Format(time.RFC3339)
	delete(p.completed, key)
	return p.writeLocked(s)
}

// MarkCompleted records successful completion.
func (p *Progress) MarkCompleted(root, date string, bars int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := stateKey(root, date)
	s, ok := p.states[key]
	if !ok {
		s = &SyncState{Root: root, Date: date}
		p.states[key] = s
	}
	s.Status = "completed"
	s.Bars = bars
	s.Error = ""
	s.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	p.completed[key] = true
	return p.writeLocked(s)
}

// MarkFailed records a failure.
func (p *Progress) MarkFailed(root, date string, errMsg string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := stateKey(root, date)
	s, ok := p.states[key]
	if !ok {
		s = &SyncState{Root: root, Date: date}
		p.states[key] = s
	}
	s.Status = "failed"
	s.Error = errMsg
	delete(p.completed, key)
	return p.writeLocked(s)
}

func (p *Progress) writeLocked(s *SyncState) error {
	dir := filepath.Join(p.dir, "states", s.Date)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, s.Root+".json"), data, 0o644)
}

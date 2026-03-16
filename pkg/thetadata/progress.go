package thetadata

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Progress tracks which (root, date) pairs have been fully downloaded.
// Uses a simple text file: one "ROOT\tYYYY-MM-DD" per line.
type Progress struct {
	dir       string
	completed map[string]bool // key: "ROOT\tYYYY-MM-DD"
	mu        sync.Mutex
}

// NewProgress creates a Progress tracker using files in the given directory.
func NewProgress(dir string) (*Progress, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create progress dir: %w", err)
	}
	p := &Progress{
		dir:       dir,
		completed: make(map[string]bool),
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Progress) filePath() string {
	return filepath.Join(p.dir, "sync_progress.txt")
}

func (p *Progress) load() error {
	f, err := os.Open(p.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open progress file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			p.completed[line] = true
		}
	}
	return scanner.Err()
}

func makeKey(root string, date time.Time) string {
	return root + "\t" + date.Format("2006-01-02")
}

// IsCompleted checks if data for the given root and date has already been downloaded.
func (p *Progress) IsCompleted(root string, date time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed[makeKey(root, date)]
}

// MarkCompleted records that data for the given root and date has been downloaded.
func (p *Progress) MarkCompleted(root string, date time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := makeKey(root, date)
	if p.completed[key] {
		return nil
	}
	p.completed[key] = true

	f, err := os.OpenFile(p.filePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open progress file for append: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintln(f, key)
	return err
}

// CompletedCount returns the number of completed (root, date) pairs.
func (p *Progress) CompletedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.completed)
}

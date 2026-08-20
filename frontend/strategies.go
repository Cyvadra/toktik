package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var strategyNamePattern = regexp.MustCompile(`(?m)^\s*strategy\s*\(\s*["']([^"']+)["']`)

type strategy struct {
	FileName    string
	DisplayName string
	Source      string
}

type strategyStore struct {
	strategies []strategy
	byFileName map[string]strategy
}

func newStrategyStore(dir string) (*strategyStore, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read strategy directory: %w", err)
	}
	store := &strategyStore{byFileName: make(map[string]strategy)}
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".toktik" {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read strategy %s: %w", entry.Name(), err)
		}
		item := strategy{
			FileName:    entry.Name(),
			DisplayName: strategyDisplayName(entry.Name(), string(source)),
			Source:      string(source),
		}
		store.strategies = append(store.strategies, item)
		store.byFileName[item.FileName] = item
	}
	sort.Slice(store.strategies, func(left, right int) bool {
		return store.strategies[left].FileName < store.strategies[right].FileName
	})
	if len(store.strategies) == 0 {
		return nil, fmt.Errorf("no .toktik strategies found in %s", dir)
	}
	return store, nil
}

func (s *strategyStore) list() []strategy {
	return append([]strategy(nil), s.strategies...)
}

func (s *strategyStore) get(fileName string) (strategy, bool) {
	item, ok := s.byFileName[fileName]
	return item, ok
}

func strategyDisplayName(fileName, source string) string {
	match := strategyNamePattern.FindStringSubmatch(source)
	if len(match) == 2 && strings.TrimSpace(match[1]) != "" {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}

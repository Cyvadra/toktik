package feeds

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = make(map[string]Feed)
)

// Register adds a feed to the global registry. Typically called from init().
// Panics if a feed with the same name is already registered.
func Register(f Feed) {
	mu.Lock()
	defer mu.Unlock()
	name := f.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("feeds: duplicate feed registration %q", name))
	}
	registry[name] = f
}

// Get returns a registered feed by name, or nil if not found.
func Get(name string) Feed {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// List returns the names of all registered feeds.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// All returns a snapshot of all registered feeds.
func All() map[string]Feed {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]Feed, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

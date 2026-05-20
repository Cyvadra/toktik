package fmp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultCacheTTL = 24 * time.Hour

var defaultClientCache struct {
	mu  sync.RWMutex
	dir string
}

// SetDefaultCacheDir configures the default on-disk cache directory used by
// newly constructed FMP clients.
func SetDefaultCacheDir(dir string) {
	defaultClientCache.mu.Lock()
	defaultClientCache.dir = normalizeCacheDir(dir)
	defaultClientCache.mu.Unlock()
}

func defaultCacheDir() string {
	defaultClientCache.mu.RLock()
	defer defaultClientCache.mu.RUnlock()
	return defaultClientCache.dir
}

func (c *Client) loadCachedBody(url string) ([]byte, bool) {
	path := c.cachePath(url)
	if path == "" || c.cacheTTL <= 0 {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if time.Since(info.ModTime()) > c.cacheTTL {
		_ = os.Remove(path)
		return nil, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return body, true
}

func (c *Client) storeCachedBody(url string, body []byte) {
	path := c.cachePath(url)
	if path == "" || c.cacheTTL <= 0 || len(body) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (c *Client) deleteCachedBody(url string) error {
	path := c.cachePath(url)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *Client) cachePath(url string) string {
	if c == nil || c.cacheDir == "" || url == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(url))
	key := hex.EncodeToString(hash[:])
	return filepath.Join(c.cacheDir, key[:2], key+".json")
}

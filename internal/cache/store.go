package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Cyvadra/toktik/internal/config"
	redis "github.com/redis/go-redis/v9"
)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Close() error
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]memoryEntry)}
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	now := time.Now()
	s.mu.RLock()
	entry, ok := s.entries[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
		s.mu.Lock()
		delete(s.entries, key)
		s.mu.Unlock()
		return nil, false, nil
	}
	copyValue := make([]byte, len(entry.value))
	copy(copyValue, entry.value)
	return copyValue, true, nil
}

func (s *MemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	copyValue := make([]byte, len(value))
	copy(copyValue, value)
	entry := memoryEntry{value: copyValue}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.entries[key] = entry
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Close() error {
	return nil
}

type RedisStore struct {
	client    *redis.Client
	keyPrefix string
}

func NewRedisStore(ctx context.Context, cfg config.Runtime) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  cfg.RedisDialTimeout(),
		ReadTimeout:  cfg.RedisReadTimeout(),
		WriteTimeout: cfg.RedisWriteTimeout(),
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &RedisStore{client: client, keyPrefix: cfg.Redis.KeyPrefix}, nil
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.Get(ctx, s.withPrefix(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return value, true, nil
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, s.withPrefix(key), value, ttl).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) withPrefix(key string) string {
	if s == nil || s.keyPrefix == "" {
		return key
	}
	return s.keyPrefix + ":" + key
}

func NewStore(ctx context.Context, cfg config.Runtime) (Store, error) {
	if cfg.Redis.Enabled {
		return NewRedisStore(ctx, cfg)
	}
	return NewMemoryStore(), nil
}

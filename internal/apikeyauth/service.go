package apikeyauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/Cyvadra/toktik/internal/apikeyrepo"
)

const (
	DefaultCacheTTL = 30 * time.Second
	TokenPrefix     = "tk_live_"
)

type Principal struct {
	ID           uint64
	KeyDigest    string
	KeyPrefix    string
	OwnerType    string
	OwnerID      string
	UserType     string
	AuthLevel    string
	RateLimitRPS *float64
	ExpiresAt    *time.Time
}

type Repository interface {
	FindByDigest(ctx context.Context, digest string) (*apikeyrepo.APIKey, bool, error)
	TouchLastUsed(ctx context.Context, id uint64, usedAt time.Time) error
}

type Service struct {
	repo     Repository
	now      func() time.Time
	cacheTTL time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	principal Principal
	found     bool
	expiresAt time.Time
}

func New(repo Repository) *Service {
	return &Service{
		repo:     repo,
		now:      time.Now,
		cacheTTL: DefaultCacheTTL,
		cache:    make(map[string]cacheEntry),
	}
}

func (s *Service) WithCacheTTL(ttl time.Duration) *Service {
	if ttl > 0 {
		s.cacheTTL = ttl
	}
	return s
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, plaintext string) (Principal, bool, error) {
	key := strings.TrimSpace(plaintext)
	if key == "" {
		return Principal{}, false, nil
	}
	digest := Digest(key)
	now := s.now()

	if principal, found, ok := s.getCached(digest, now); ok {
		return principal, found, nil
	}

	record, ok, err := s.repo.FindByDigest(ctx, digest)
	if err != nil {
		return Principal{}, false, err
	}
	if !ok || !record.Active || isExpired(record.ExpiresAt, now) {
		s.setCached(digest, Principal{}, false, now)
		return Principal{}, false, nil
	}

	principal := principalFromRecord(record)
	s.setCached(digest, principal, true, now)
	_ = s.repo.TouchLastUsed(ctx, record.ID, now)
	return principal, true, nil
}

func (s *Service) getCached(digest string, now time.Time) (Principal, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[digest]
	if !ok {
		return Principal{}, false, false
	}
	if !now.Before(entry.expiresAt) {
		delete(s.cache, digest)
		return Principal{}, false, false
	}
	return entry.principal, entry.found, true
}

func (s *Service) setCached(digest string, principal Principal, found bool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[digest] = cacheEntry{principal: principal, found: found, expiresAt: now.Add(s.cacheTTL)}
}

func principalFromRecord(record *apikeyrepo.APIKey) Principal {
	return Principal{
		ID:           record.ID,
		KeyDigest:    record.KeyDigest,
		KeyPrefix:    record.KeyPrefix,
		OwnerType:    record.OwnerType,
		OwnerID:      record.OwnerID,
		UserType:     record.UserType,
		AuthLevel:    record.AuthLevel,
		RateLimitRPS: record.RateLimitRPS,
		ExpiresAt:    record.ExpiresAt,
	}
}

func isExpired(expiresAt *time.Time, now time.Time) bool {
	return expiresAt != nil && !expiresAt.After(now)
}

func Digest(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func GenerateToken() (string, string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", err
	}
	token := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, Prefix(token), Digest(token), nil
}

func Prefix(token string) string {
	if len(token) <= 16 {
		return token
	}
	return token[:16]
}

package apikeyauth

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/apikeyrepo"
)

type fakeRepo struct {
	records map[string]*apikeyrepo.APIKey
	lookups int
	touched []uint64
}

func (f *fakeRepo) FindByDigest(_ context.Context, digest string) (*apikeyrepo.APIKey, bool, error) {
	f.lookups++
	record, ok := f.records[digest]
	return record, ok, nil
}

func (f *fakeRepo) TouchLastUsed(_ context.Context, id uint64, _ time.Time) error {
	f.touched = append(f.touched, id)
	return nil
}

func TestAuthenticateAPIKeyReturnsPrincipalMetadata(t *testing.T) {
	limit := 7.5
	expiresAt := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	token := "secret"
	digest := Digest(token)
	repo := &fakeRepo{records: map[string]*apikeyrepo.APIKey{
		digest: {
			ID:           42,
			KeyDigest:    digest,
			KeyPrefix:    "tk_live_abc",
			OwnerType:    "business",
			OwnerID:      "biz-1",
			UserType:     "internal",
			AuthLevel:    "admin",
			RateLimitRPS: &limit,
			ExpiresAt:    &expiresAt,
			Active:       true,
		},
	}}
	svc := New(repo).WithCacheTTL(time.Minute)
	svc.now = func() time.Time { return time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC) }

	principal, ok, err := svc.AuthenticateAPIKey(context.Background(), token)
	if err != nil {
		t.Fatalf("AuthenticateAPIKey failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected key to authenticate")
	}
	if principal.ID != 42 || principal.OwnerType != "business" || principal.OwnerID != "biz-1" || principal.UserType != "internal" || principal.AuthLevel != "admin" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if principal.RateLimitRPS == nil || *principal.RateLimitRPS != limit {
		t.Fatalf("unexpected principal rate limit: %+v", principal.RateLimitRPS)
	}
	if len(repo.touched) != 1 || repo.touched[0] != 42 {
		t.Fatalf("expected last-used touch for id 42, got %#v", repo.touched)
	}
}

func TestAuthenticateAPIKeyRejectsInactiveAndExpiredKeys(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Second)
	tests := []struct {
		name   string
		record apikeyrepo.APIKey
	}{
		{name: "inactive", record: apikeyrepo.APIKey{ID: 1, Active: false}},
		{name: "expired", record: apikeyrepo.APIKey{ID: 2, Active: true, ExpiresAt: &expiredAt}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := "secret-" + tt.name
			digest := Digest(token)
			record := tt.record
			record.KeyDigest = digest
			repo := &fakeRepo{records: map[string]*apikeyrepo.APIKey{digest: &record}}
			svc := New(repo)
			svc.now = func() time.Time { return now }

			_, ok, err := svc.AuthenticateAPIKey(context.Background(), token)
			if err != nil {
				t.Fatalf("AuthenticateAPIKey failed: %v", err)
			}
			if ok {
				t.Fatalf("expected key to be rejected")
			}
			if len(repo.touched) != 0 {
				t.Fatalf("rejected key should not be touched: %#v", repo.touched)
			}
		})
	}
}

func TestAuthenticateAPIKeyCachesNegativeLookups(t *testing.T) {
	repo := &fakeRepo{records: map[string]*apikeyrepo.APIKey{}}
	svc := New(repo).WithCacheTTL(time.Minute)
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	for range 2 {
		_, ok, err := svc.AuthenticateAPIKey(context.Background(), "missing")
		if err != nil {
			t.Fatalf("AuthenticateAPIKey failed: %v", err)
		}
		if ok {
			t.Fatalf("missing key should not authenticate")
		}
	}
	if repo.lookups != 1 {
		t.Fatalf("expected one repo lookup due to negative cache, got %d", repo.lookups)
	}
}

func TestGenerateTokenReturnsDigestAndPrefix(t *testing.T) {
	token, prefix, digest, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if len(token) <= len(TokenPrefix) || token[:len(TokenPrefix)] != TokenPrefix {
		t.Fatalf("unexpected token format: %q", token)
	}
	if prefix != Prefix(token) {
		t.Fatalf("prefix = %q, want %q", prefix, Prefix(token))
	}
	if digest != Digest(token) {
		t.Fatalf("digest mismatch")
	}
}

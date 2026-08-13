package apikeyrepo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type APIKey struct {
	ID           uint64 `gorm:"primaryKey"`
	Name         string `gorm:"size:128;not null"`
	KeyDigest    string `gorm:"size:64;not null;uniqueIndex"`
	KeyPrefix    string `gorm:"size:32;not null;index"`
	OwnerType    string `gorm:"size:40;not null;default:'';index:idx_api_keys_owner,priority:1"`
	OwnerID      string `gorm:"size:128;not null;default:'';index:idx_api_keys_owner,priority:2"`
	UserType     string `gorm:"size:40;not null;default:'';index"`
	AuthLevel    string `gorm:"size:40;not null;default:'';index"`
	RateLimitRPS *float64
	ExpiresAt    *time.Time `gorm:"index"`
	Active       bool       `gorm:"not null;default:true;index"`
	LastUsedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Repo struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) AutoMigrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&APIKey{})
}

func (r *Repo) Create(ctx context.Context, key *APIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *Repo) FindByDigest(ctx context.Context, digest string) (*APIKey, bool, error) {
	var key APIKey
	result := r.db.WithContext(ctx).Where("key_digest = ?", digest).Limit(1).Find(&key)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	return &key, true, nil
}

func (r *Repo) List(ctx context.Context, filter ListFilter) ([]APIKey, error) {
	query := r.db.WithContext(ctx).Model(&APIKey{})
	if filter.ActiveOnly {
		query = query.Where("active = ?", true)
	}
	if filter.OwnerType != "" {
		query = query.Where("owner_type = ?", filter.OwnerType)
	}
	if filter.OwnerID != "" {
		query = query.Where("owner_id = ?", filter.OwnerID)
	}
	var keys []APIKey
	err := query.Order("id ASC").Find(&keys).Error
	return keys, err
}

func (r *Repo) Disable(ctx context.Context, id uint64) (bool, error) {
	result := r.db.WithContext(ctx).Model(&APIKey{}).Where("id = ?", id).Update("active", false)
	return result.RowsAffected > 0, result.Error
}

func (r *Repo) SetRateLimit(ctx context.Context, id uint64, rateLimitRPS float64) (bool, error) {
	result := r.db.WithContext(ctx).Model(&APIKey{}).Where("id = ?", id).Update("rate_limit_rps", rateLimitRPS)
	return result.RowsAffected > 0, result.Error
}

func (r *Repo) Rotate(ctx context.Context, id uint64, digest, prefix string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&APIKey{}).Where("id = ?", id).Updates(map[string]any{
		"key_digest": digest,
		"key_prefix": prefix,
		"active":     true,
	})
	return result.RowsAffected > 0, result.Error
}

func (r *Repo) TouchLastUsed(ctx context.Context, id uint64, usedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&APIKey{}).Where("id = ?", id).Update("last_used_at", usedAt).Error
}

type ListFilter struct {
	ActiveOnly bool
	OwnerType  string
	OwnerID    string
}

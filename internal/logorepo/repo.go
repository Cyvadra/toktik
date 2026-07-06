package logorepo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StockLogo struct {
	ID          uint64 `gorm:"primaryKey"`
	Symbol      string `gorm:"size:32;not null;uniqueIndex"`
	ContentType string `gorm:"size:96;not null"`
	DataBase64  string `gorm:"type:longtext;not null"`
	SourceURL   string `gorm:"size:1024"`
	Source      string `gorm:"size:32;not null;default:'fmp'"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Repo struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) AutoMigrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&StockLogo{})
}

func (r *Repo) Find(ctx context.Context, symbol string) (*StockLogo, bool, error) {
	var logo StockLogo
	result := r.db.WithContext(ctx).Where("symbol = ?", symbol).Limit(1).Find(&logo)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected > 0 {
		return &logo, true, nil
	}
	return nil, false, nil
}

func (r *Repo) Upsert(ctx context.Context, logo StockLogo) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "symbol"}},
		UpdateAll: true,
	}).Create(&logo).Error
}

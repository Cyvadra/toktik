package universerepo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Definition struct {
	ID         uint64 `gorm:"primaryKey"`
	Code       string `gorm:"size:96;not null;index:idx_universe_definition_code,unique,priority:1"`
	Market     string `gorm:"size:32;not null;index:idx_universe_definition_code,unique,priority:2"`
	SourceType string `gorm:"size:64;not null;index"`
	Parameters string `gorm:"type:json"`
	Version    uint64 `gorm:"not null;default:1"`
	Active     bool   `gorm:"not null;default:true;index"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Run struct {
	ID             uint64 `gorm:"primaryKey"`
	RunID          string `gorm:"size:64;not null;index:idx_universe_run_id,unique"`
	DefinitionCode string `gorm:"size:96;not null;index:idx_universe_run_lookup,priority:1"`
	Market         string `gorm:"size:32;not null;index:idx_universe_run_lookup,priority:2"`
	Version        uint64 `gorm:"not null;default:1"`
	Status         string `gorm:"size:32;not null;index"`
	FromDate       string `gorm:"size:16;not null;default:''"`
	ToDate         string `gorm:"size:16;not null;default:''"`
	ParamsHash     string `gorm:"size:128;not null;default:'';index"`
	IdempotencyKey string `gorm:"size:160;not null;default:'';index"`
	Stats          string `gorm:"type:json"`
	Error          string `gorm:"type:text"`
	StartedAt      *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Repo struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) AutoMigrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&Definition{}, &Run{})
}

func (r *Repo) UpsertDefinition(ctx context.Context, definition Definition) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "code"}, {Name: "market"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_type",
			"parameters",
			"version",
			"active",
			"updated_at",
		}),
	}).Create(&definition).Error
}

func (r *Repo) ActiveDefinition(ctx context.Context, market, code string) (Definition, bool, error) {
	var definition Definition
	err := r.db.WithContext(ctx).
		Where("market = ? AND code = ? AND active = ?", market, code, true).
		First(&definition).Error
	if err == nil {
		return definition, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return Definition{}, false, nil
	}
	return Definition{}, false, err
}

func (r *Repo) UpsertRun(ctx context.Context, run Run) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}},
		UpdateAll: true,
	}).Create(&run).Error
}

func (r *Repo) LatestRun(ctx context.Context, market, definitionCode string) (Run, bool, error) {
	var run Run
	err := r.db.WithContext(ctx).
		Where("market = ? AND definition_code = ?", market, definitionCode).
		Order("updated_at DESC").
		First(&run).Error
	if err == nil {
		return run, true, nil
	}
	if err == gorm.ErrRecordNotFound {
		return Run{}, false, nil
	}
	return Run{}, false, err
}

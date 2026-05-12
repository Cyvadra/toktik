package importledger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const TableName = "import_ledger"

const SchemaDDL = `CREATE TABLE IF NOT EXISTS import_ledger
(
    importer_name LowCardinality(String),
    source_key    String,
    scope_key     String,
    import_id     String,
    source_hash   String DEFAULT '',
    status        Enum8('pending' = 1, 'success' = 2, 'failed' = 3),
    rows_inserted UInt64 DEFAULT 0,
    error_message String DEFAULT '',
    started_at    DateTime64(3, 'UTC'),
    completed_at  DateTime64(3, 'UTC') DEFAULT toDateTime64(0, 3, 'UTC'),
    version       UInt64
)
ENGINE = ReplacingMergeTree(version)
ORDER BY (importer_name, source_key, scope_key)
SETTINGS index_granularity = 8192`

type Status string

const (
	StatusPending Status = "pending"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

type Repository struct {
	conn driver.Conn
}

func New(conn driver.Conn) *Repository {
	return &Repository{conn: conn}
}

type StartRequest struct {
	ImporterName string
	SourceKey    string
	ScopeKey     string
	SourceHash   string
	StartedAt    time.Time
}

type CompletionRequest struct {
	ImporterName string
	SourceKey    string
	ScopeKey     string
	ImportID     string
	SourceHash   string
	RowsInserted uint64
	ErrorMessage string
	CompletedAt  time.Time
}

func EnsureSchema(ctx context.Context, conn driver.Conn) error {
	return conn.Exec(ctx, SchemaDDL)
}

func (r *Repository) AlreadySucceeded(ctx context.Context, importerName, sourceKey, scopeKey string) (bool, error) {
	importerName, sourceKey, scopeKey, err := normalizeKey(importerName, sourceKey, scopeKey)
	if err != nil {
		return false, err
	}

	rows, err := r.conn.Query(ctx, `
SELECT status
FROM import_ledger FINAL
WHERE importer_name = {importer_name:String}
  AND source_key = {source_key:String}
  AND scope_key = {scope_key:String}
ORDER BY version DESC
LIMIT 1`,
		clickhouse.Named("importer_name", importerName),
		clickhouse.Named("source_key", sourceKey),
		clickhouse.Named("scope_key", scopeKey),
	)
	if err != nil {
		return false, fmt.Errorf("query import ledger: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return false, rows.Err()
	}
	var status string
	if err := rows.Scan(&status); err != nil {
		return false, fmt.Errorf("scan import ledger status: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read import ledger status: %w", err)
	}
	return Status(status) == StatusSuccess, nil
}

func (r *Repository) Start(ctx context.Context, req StartRequest) (string, error) {
	importerName, sourceKey, scopeKey, err := normalizeKey(req.ImporterName, req.SourceKey, req.ScopeKey)
	if err != nil {
		return "", err
	}
	importID, err := newImportID()
	if err != nil {
		return "", err
	}
	startedAt := normalizeTime(req.StartedAt)
	if err := r.insert(ctx, ledgerRow{
		ImporterName: importerName,
		SourceKey:    sourceKey,
		ScopeKey:     scopeKey,
		ImportID:     importID,
		SourceHash:   strings.TrimSpace(req.SourceHash),
		Status:       StatusPending,
		StartedAt:    startedAt,
		CompletedAt:  zeroTime(),
		Version:      versionFromTime(startedAt),
	}); err != nil {
		return "", err
	}
	return importID, nil
}

func (r *Repository) MarkSuccess(ctx context.Context, req CompletionRequest) error {
	return r.complete(ctx, req, StatusSuccess)
}

func (r *Repository) MarkFailed(ctx context.Context, req CompletionRequest) error {
	return r.complete(ctx, req, StatusFailed)
}

func (r *Repository) complete(ctx context.Context, req CompletionRequest, status Status) error {
	importerName, sourceKey, scopeKey, err := normalizeKey(req.ImporterName, req.SourceKey, req.ScopeKey)
	if err != nil {
		return err
	}
	importID := strings.TrimSpace(req.ImportID)
	if importID == "" {
		return fmt.Errorf("import_id is required")
	}
	completedAt := normalizeTime(req.CompletedAt)
	return r.insert(ctx, ledgerRow{
		ImporterName: importerName,
		SourceKey:    sourceKey,
		ScopeKey:     scopeKey,
		ImportID:     importID,
		SourceHash:   strings.TrimSpace(req.SourceHash),
		Status:       status,
		RowsInserted: req.RowsInserted,
		ErrorMessage: strings.TrimSpace(req.ErrorMessage),
		StartedAt:    completedAt,
		CompletedAt:  completedAt,
		Version:      versionFromTime(completedAt),
	})
}

type ledgerRow struct {
	ImporterName string
	SourceKey    string
	ScopeKey     string
	ImportID     string
	SourceHash   string
	Status       Status
	RowsInserted uint64
	ErrorMessage string
	StartedAt    time.Time
	CompletedAt  time.Time
	Version      uint64
}

func (r *Repository) insert(ctx context.Context, row ledgerRow) error {
	batch, err := r.conn.PrepareBatch(ctx, `INSERT INTO import_ledger (
importer_name, source_key, scope_key, import_id, source_hash, status,
rows_inserted, error_message, started_at, completed_at, version
)`)
	if err != nil {
		return fmt.Errorf("prepare import ledger batch: %w", err)
	}
	if err := batch.Append(
		row.ImporterName,
		row.SourceKey,
		row.ScopeKey,
		row.ImportID,
		row.SourceHash,
		string(row.Status),
		row.RowsInserted,
		row.ErrorMessage,
		row.StartedAt.UTC(),
		row.CompletedAt.UTC(),
		row.Version,
	); err != nil {
		return fmt.Errorf("append import ledger row: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send import ledger batch: %w", err)
	}
	return nil
}

func normalizeKey(importerName, sourceKey, scopeKey string) (string, string, string, error) {
	importerName = strings.TrimSpace(importerName)
	sourceKey = strings.TrimSpace(sourceKey)
	scopeKey = strings.TrimSpace(scopeKey)
	if importerName == "" {
		return "", "", "", fmt.Errorf("importer_name is required")
	}
	if sourceKey == "" {
		return "", "", "", fmt.Errorf("source_key is required")
	}
	if scopeKey == "" {
		scopeKey = "default"
	}
	return importerName, sourceKey, scopeKey, nil
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func zeroTime() time.Time {
	return time.Unix(0, 0).UTC()
}

func versionFromTime(value time.Time) uint64 {
	return uint64(normalizeTime(value).UnixNano())
}

func newImportID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate import id: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := make([]byte, 32)
	hex.Encode(encoded, raw)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}

func SourceHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func NonNegativeRows(rows int64) uint64 {
	if rows <= 0 {
		return 0
	}
	return uint64(rows)
}

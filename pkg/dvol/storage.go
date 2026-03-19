package dvol

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

const insertDvolSQL = `INSERT INTO deribit_dvol_bar (
currency, index_name, resolution, timestamp, open, high, low, close
)`

// Store wraps a ClickHouse connection for Deribit DVOL data.
type Store struct {
	conn driver.Conn
}

// NewStore opens a ClickHouse connection.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return &Store{conn: conn}, nil
}

// Close closes the ClickHouse connection.
func (s *Store) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// InitSchema executes DVOL DDL SQL from schemaFile.
func (s *Store) InitSchema(ctx context.Context, schemaFile string) error {
	if schemaFile == "" {
		return nil
	}
	return cryptooptions.InitSchema(ctx, s.conn, schemaFile)
}

// InsertBars inserts DVOL bars in batches.
func (s *Store) InsertBars(ctx context.Context, bars []Bar, batchSize int) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 5000
	}

	batch, err := s.conn.PrepareBatch(ctx, insertDvolSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare DVOL batch: %w", err)
	}

	total := 0
	pending := 0
	for _, b := range bars {
		if err := batch.Append(
			b.Currency,
			b.IndexName,
			b.Resolution,
			b.Timestamp,
			float32(b.Open),
			float32(b.High),
			float32(b.Low),
			float32(b.Close),
		); err != nil {
			return total, fmt.Errorf("append DVOL row: %w", err)
		}
		total++
		pending++

		if pending >= batchSize {
			if err := batch.Send(); err != nil {
				return total, fmt.Errorf("send DVOL batch: %w", err)
			}
			batch, err = s.conn.PrepareBatch(ctx, insertDvolSQL)
			if err != nil {
				return total, fmt.Errorf("prepare next DVOL batch: %w", err)
			}
			pending = 0
		}
	}

	if pending > 0 {
		if err := batch.Send(); err != nil {
			return total, fmt.Errorf("send final DVOL batch: %w", err)
		}
	}

	return total, nil
}

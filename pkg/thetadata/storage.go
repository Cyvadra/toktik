package thetadata

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// Store handles ClickHouse operations for persisting downloaded option data.
type Store struct {
	conn driver.Conn
}

// NewStore creates a new Store with the given ClickHouse DSN.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse ClickHouse DSN: %w", err)
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return &Store{conn: conn}, nil
}

// InitSchema ensures the required tables exist. Reuses the existing
// crypto_options schema (same field structure).
func (s *Store) InitSchema(ctx context.Context, schemaFile string) error {
	if schemaFile != "" {
		if err := cryptooptions.InitSchema(ctx, s.conn, schemaFile); err != nil {
			return err
		}
	}
	if err := cryptooptions.InitKlineSchema(ctx, s.conn); err != nil {
		return err
	}
	return cryptooptions.InitSpotKlineSchema(ctx, s.conn)
}

// InsertBars batch-inserts 1m option bars into crypto_options_bar_1m.
func (s *Store) InsertBars(ctx context.Context, bars []cryptooptions.Bar1m) error {
	if len(bars) == 0 {
		return nil
	}

	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO crypto_options_bar_1m (
		timestamp, symbol_id, base_asset,
		mark_open, mark_high, mark_low, mark_close,
		last_open, last_high, last_low, last_close,
		bid_open, bid_high, bid_low, bid_close,
		ask_open, ask_high, ask_low, ask_close,
		mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
		delta, gamma, vega, theta, rho,
		open_interest, tick_count
	)`)
	if err != nil {
		return fmt.Errorf("prepare bar batch: %w", err)
	}

	for _, bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.SymbolID,
			bar.BaseAsset,
			bar.MarkOpen, bar.MarkHigh, bar.MarkLow, bar.MarkClose,
			bar.LastOpen, bar.LastHigh, bar.LastLow, bar.LastClose,
			bar.BidOpen, bar.BidHigh, bar.BidLow, bar.BidClose,
			bar.AskOpen, bar.AskHigh, bar.AskLow, bar.AskClose,
			bar.MarkIVOpen, bar.MarkIVClose, bar.BidIVOpen, bar.AskIVOpen,
			bar.Delta, bar.Gamma, bar.Vega, bar.Theta, bar.Rho,
			bar.OpenInterest, bar.TickCount,
		); err != nil {
			return fmt.Errorf("append bar: %w", err)
		}
	}

	return batch.Send()
}

// InsertSymbols batch-inserts symbol metadata.
func (s *Store) InsertSymbols(ctx context.Context, symbols []cryptooptions.SymbolMeta) error {
	return cryptooptions.InsertSymbols(ctx, s.conn, symbols)
}

// InsertSpotBars batch-inserts underlying price bars into crypto_spot_bar_1m.
func (s *Store) InsertSpotBars(ctx context.Context, bars []cryptooptions.SpotBar1m) error {
	if len(bars) == 0 {
		return nil
	}

	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO crypto_spot_bar_1m (
		timestamp, symbol, price_source,
		open, high, low, close,
		tick_count
	)`)
	if err != nil {
		return fmt.Errorf("prepare spot bar batch: %w", err)
	}

	for _, bar := range bars {
		if err := batch.Append(
			bar.Timestamp,
			bar.Symbol,
			bar.PriceSource,
			bar.Open, bar.High, bar.Low, bar.Close,
			bar.TickCount,
		); err != nil {
			return fmt.Errorf("append spot bar: %w", err)
		}
	}

	return batch.Send()
}

// HasDateData checks if any bar data exists for the given root and date.
func (s *Store) HasDateData(ctx context.Context, root string, date time.Time) (bool, error) {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := startOfDay.Add(24 * time.Hour)

	rows, err := s.conn.Query(ctx,
		`SELECT count() FROM crypto_options_bar_1m
		 WHERE base_asset = $1
		   AND timestamp >= $2
		   AND timestamp < $3
		 LIMIT 1`,
		root, startOfDay, endOfDay,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	if rows.Next() {
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return false, err
		}
		return count > 0, nil
	}
	return false, nil
}

// Close closes the ClickHouse connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

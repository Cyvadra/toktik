package service

import (
	"context"
	"fmt"
	"strings"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dto"
)

var cryptoSpotBarIntervals = map[string]string{
	"1m":  "crypto_spot_bar_1m",
	"5m":  "crypto_spot_bar_5m",
	"15m": "crypto_spot_bar_15m",
	"30m": "crypto_spot_bar_30m",
	"1h":  "crypto_spot_bar_1h",
	"2h":  "crypto_spot_bar_2h",
	"3h":  "crypto_spot_bar_3h",
	"4h":  "crypto_spot_bar_4h",
	"6h":  "crypto_spot_bar_6h",
	"8h":  "crypto_spot_bar_8h",
	"12h": "crypto_spot_bar_12h",
	"1d":  "crypto_spot_bar_1d",
}

// CryptoSpotService provides crypto spot/underlying market-data queries.
type CryptoSpotService struct {
	conn driver.Conn
}

func NewCryptoSpotService(conn driver.Conn) *CryptoSpotService {
	return &CryptoSpotService{conn: conn}
}

func (s *CryptoSpotService) QuerySymbols(ctx context.Context, req dto.CryptoSpotSymbolRequest) (*dto.CryptoSpotSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	query := `SELECT symbol FROM crypto_spot_bar_1m WHERE 1 = 1`

	args := make([]interface{}, 0, 2)
	if req.Search != "" {
		query += ` AND symbol ILIKE {search:String}`
		args = append(args, clickhouse.Named("search", "%"+req.Search+"%"))
	}
	if req.Cursor != "" {
		cursorSymbol, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += ` AND symbol > {cursor_symbol:String}`
		args = append(args, clickhouse.Named("cursor_symbol", cursorSymbol))
	}

	query += fmt.Sprintf(` GROUP BY symbol ORDER BY symbol LIMIT %d`, limit+1)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query crypto spot symbols: %w", err)
	}
	defer rows.Close()

	symbols := make([]dto.CryptoSpotSymbolRow, 0, limit)
	for rows.Next() {
		var row dto.CryptoSpotSymbolRow
		if err := rows.Scan(&row.Symbol); err != nil {
			return nil, fmt.Errorf("scan crypto spot symbol row: %w", err)
		}
		symbols = append(symbols, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate crypto spot symbol rows: %w", err)
	}

	resp := &dto.CryptoSpotSymbolResponse{}
	if len(symbols) > limit {
		resp.NextCursor = encodeCursorString(symbols[limit-1].Symbol)
		resp.Data = symbols[:limit]
	} else {
		resp.Data = symbols
	}
	return resp, nil
}

func (s *CryptoSpotService) QueryBars(ctx context.Context, req dto.CryptoSpotBarRequest) (*dto.CryptoSpotBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval := strings.ToLower(strings.TrimSpace(req.Interval))
	tableName, ok := cryptoSpotBarIntervals[interval]
	if !ok {
		return nil, dto.NewValidationError("unsupported crypto spot interval %q", req.Interval)
	}

	limit := clamp(req.Limit, defaultBarLimit, maxBarLimit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	query := fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    open,
    high,
    low,
    close,
    toFloat64(volume_base) AS volume,
    tick_count
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT %d`, tableName, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", cryptooptions.ClickHouseTimeParam(fromT)),
		clickhouse.Named("to", cryptooptions.ClickHouseTimeParam(toT)),
	)
	if err != nil {
		return nil, fmt.Errorf("query crypto spot bars: %w", err)
	}
	defer rows.Close()

	bars := make([]dto.CryptoSpotBarRow, 0, limit)
	for rows.Next() {
		var row dto.CryptoSpotBarRow
		if err := rows.Scan(
			&row.Timestamp,
			&row.Symbol,
			&row.Open,
			&row.High,
			&row.Low,
			&row.Close,
			&row.Volume,
			&row.TickCount,
		); err != nil {
			return nil, fmt.Errorf("scan crypto spot bar row: %w", err)
		}
		bars = append(bars, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate crypto spot bar rows: %w", err)
	}

	resp := &dto.CryptoSpotBarResponse{}
	if len(bars) > limit {
		resp.NextCursor = encodeCursor(bars[limit-1].Timestamp)
		resp.Data = bars[:limit]
	} else {
		resp.Data = bars
	}
	return resp, nil
}

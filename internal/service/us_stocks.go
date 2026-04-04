package service

import (
	"context"
	"fmt"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dto"
)

// USStocksService provides low-level US stock market-data queries.
type USStocksService struct {
	conn driver.Conn
}

func NewUSStocksService(conn driver.Conn) *USStocksService {
	return &USStocksService{conn: conn}
}

func (s *USStocksService) QuerySymbols(ctx context.Context, req dto.USStockSymbolRequest) (*dto.USStockSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	query := `SELECT symbol
FROM us_stocks_bar_1m
WHERE 1 = 1`

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

	query += fmt.Sprintf(`
GROUP BY symbol
ORDER BY symbol
LIMIT %d`, limit+1)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query US stock symbols: %w", err)
	}
	defer rows.Close()

	symbols := make([]dto.USStockSymbolRow, 0, limit)
	for rows.Next() {
		var row dto.USStockSymbolRow
		if err := rows.Scan(&row.Symbol); err != nil {
			return nil, fmt.Errorf("scan US stock symbol row: %w", err)
		}
		symbols = append(symbols, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock symbol rows: %w", err)
	}

	resp := &dto.USStockSymbolResponse{}
	if len(symbols) > limit {
		resp.NextCursor = encodeCursorString(symbols[limit-1].Symbol)
		resp.Data = symbols[:limit]
	} else {
		resp.Data = symbols
	}
	return resp, nil
}

func (s *USStocksService) QueryBars(ctx context.Context, req dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	tableName, err := resolveUSBarTable(req.Interval, usStockBarIntervals, "US stock")
	if err != nil {
		return nil, err
	}
	session, err := normalizeUSSession(req.Session, req.Interval)
	if err != nil {
		return nil, err
	}
	limit := usBarLimit(req.Limit)

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
    toUInt64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')%s
ORDER BY timestamp
LIMIT %d`, tableName, usSessionCondition(session), limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", cryptooptions.ClickHouseTimeParam(fromT)),
		clickhouse.Named("to", cryptooptions.ClickHouseTimeParam(toT)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US stock bars: %w", err)
	}
	defer rows.Close()

	bars := make([]dto.USStockBarRow, 0, limit)
	for rows.Next() {
		var row dto.USStockBarRow
		if err := rows.Scan(
			&row.Timestamp,
			&row.Symbol,
			&row.Open,
			&row.High,
			&row.Low,
			&row.Close,
			&row.Volume,
			&row.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US stock bar row: %w", err)
		}
		bars = append(bars, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock bar rows: %w", err)
	}

	resp := &dto.USStockBarResponse{}
	if len(bars) > limit {
		resp.NextCursor = encodeCursor(bars[limit-1].Timestamp)
		resp.Data = bars[:limit]
	} else {
		resp.Data = bars
	}
	return resp, nil
}

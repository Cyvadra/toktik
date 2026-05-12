package service

import (
	"context"
	"fmt"

	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

// ForexService provides low-level forex market-data queries.
type ForexService struct {
	repo *chrepo.Repo
}

func NewForexService(repo *chrepo.Repo) *ForexService {
	return &ForexService{repo: repo}
}

func (s *ForexService) QuerySymbols(ctx context.Context, req dto.ForexSymbolRequest) (*dto.ForexSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	query := chquery.ForexSymbolsBase
	if req.Search != "" {
		query += fmt.Sprintf(` AND symbol ILIKE %s`, clickhouseStringLiteral("%"+req.Search+"%"))
	}
	if req.Cursor != "" {
		cursorSymbol, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += fmt.Sprintf(` AND symbol > %s`, clickhouseStringLiteral(cursorSymbol))
	}
	query += fmt.Sprintf(`
GROUP BY symbol
ORDER BY symbol
LIMIT %s`, clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query forex symbols: %w", err)
	}
	defer rows.Close()

	symbols := make([]dto.ForexSymbolRow, 0, limit)
	for rows.Next() {
		var row dto.ForexSymbolRow
		if err := rows.Scan(&row.Symbol); err != nil {
			return nil, fmt.Errorf("scan forex symbol row: %w", err)
		}
		symbols = append(symbols, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate forex symbol rows: %w", err)
	}

	resp := &dto.ForexSymbolResponse{Data: make([]dto.ForexSymbolRow, 0)}
	resp.Data, resp.NextCursor = applySymbolCursorPagination(symbols, limit, func(r dto.ForexSymbolRow) string {
		return encodeCursorString(r.Symbol)
	})
	return resp, nil
}

func (s *ForexService) QueryBars(ctx context.Context, req dto.ForexBarRequest) (*dto.ForexBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	tableName, err := resolveUSBarTable(req.Interval, chquery.ForexIntervals, "forex")
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
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')
ORDER BY timestamp
LIMIT %s`, tableName, clickhouseStringLiteral(req.Symbol), clickhouseDateTimeLiteral(fromT), clickhouseDateTimeLiteral(toT), clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query forex bars: %w", err)
	}
	defer rows.Close()

	bars := make([]dto.ForexBarRow, 0, limit)
	for rows.Next() {
		var row dto.ForexBarRow
		if err := rows.Scan(&row.Timestamp, &row.Symbol, &row.Open, &row.High, &row.Low, &row.Close, &row.Volume, &row.Transactions); err != nil {
			return nil, fmt.Errorf("scan forex bar row: %w", err)
		}
		bars = append(bars, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate forex bar rows: %w", err)
	}

	resp := &dto.ForexBarResponse{Data: make([]dto.ForexBarRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(bars, limit, func(r dto.ForexBarRow) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

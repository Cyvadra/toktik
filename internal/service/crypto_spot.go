package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

// CryptoSpotService provides crypto spot/underlying market-data queries.
type CryptoSpotService struct {
	repo *chrepo.Repo
}

func NewCryptoSpotService(repo *chrepo.Repo) *CryptoSpotService {
	return &CryptoSpotService{repo: repo}
}

func (s *CryptoSpotService) QuerySymbols(ctx context.Context, req dto.CryptoSpotSymbolRequest) (*dto.CryptoSpotSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	query := `SELECT symbol FROM crypto_spot_bar_1m WHERE 1 = 1`

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

	query += fmt.Sprintf(` GROUP BY symbol ORDER BY symbol LIMIT %d`, limit+1)

	rows, err := s.repo.Query(ctx, query)
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

	resp := &dto.CryptoSpotSymbolResponse{Data: make([]dto.CryptoSpotSymbolRow, 0)}
	resp.Data, resp.NextCursor = applySymbolCursorPagination(symbols, limit, func(r dto.CryptoSpotSymbolRow) string {
		return encodeCursorString(r.Symbol)
	})
	return resp, nil
}

func (s *CryptoSpotService) QueryBars(ctx context.Context, req dto.CryptoSpotBarRequest) (*dto.CryptoSpotBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval := strings.ToLower(strings.TrimSpace(req.Interval))
	tableName, ok := chquery.CryptoSpotIntervals[interval]
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
	volume,
	    toUInt64(tick_count) AS tick_count
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')
ORDER BY timestamp
LIMIT %d`, tableName,
		clickhouseStringLiteral(req.Symbol),
		clickhouseDateTimeLiteral(fromT),
		clickhouseDateTimeLiteral(toT),
		limit+1)

	rows, err := s.repo.Query(ctx, query)
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

	resp := &dto.CryptoSpotBarResponse{Data: make([]dto.CryptoSpotBarRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(bars, limit, func(r dto.CryptoSpotBarRow) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

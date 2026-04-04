package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/dto"
)

// InfraService exposes non-business infrastructure endpoints.
type InfraService struct {
	conn driver.Conn
}

type infraDatasetSpec struct {
	Name      string
	Market    string
	Relation  string
	TimeField string
	MaxAge    time.Duration
}

var infraDatasetSpecs = []infraDatasetSpec{
	{Name: "crypto-options-bars", Market: "crypto-options", Relation: "crypto_options_bar_1m", TimeField: "timestamp", MaxAge: 96 * time.Hour},
	{Name: "crypto-spot-bars", Market: "crypto-options", Relation: "crypto_spot_bar_1m", TimeField: "timestamp", MaxAge: 96 * time.Hour},
	{Name: "us-stocks-bars", Market: "us-stocks", Relation: "us_stocks_bar_1m", TimeField: "timestamp", MaxAge: 96 * time.Hour},
	{Name: "us-options-bars", Market: "us-options", Relation: "us_options_bar_1m", TimeField: "timestamp", MaxAge: 96 * time.Hour},
	{Name: "us-options-chain", Market: "us-options", Relation: "us_options_chain_1d", TimeField: "timestamp", MaxAge: 10 * 24 * time.Hour},
	{Name: "feature-volatility-snapshots", Market: "feature-store", Relation: "feature_volatility_snapshot_daily", TimeField: "updated_at", MaxAge: 48 * time.Hour},
	{Name: "feature-term-structure-snapshots", Market: "feature-store", Relation: "feature_term_structure_snapshot_daily", TimeField: "updated_at", MaxAge: 48 * time.Hour},
	{Name: "feature-skew-snapshots", Market: "feature-store", Relation: "feature_skew_snapshot_daily", TimeField: "updated_at", MaxAge: 48 * time.Hour},
	{Name: "feature-liquidity-snapshots", Market: "feature-store", Relation: "feature_liquidity_snapshot_daily", TimeField: "updated_at", MaxAge: 48 * time.Hour},
	{Name: "feature-daily-panels", Market: "feature-store", Relation: "feature_daily_panel_daily", TimeField: "updated_at", MaxAge: 48 * time.Hour},
}

func NewInfraService(conn driver.Conn) *InfraService {
	return &InfraService{conn: conn}
}

func (s *InfraService) Readiness(ctx context.Context) (*dto.ReadinessResponse, error) {
	if err := s.conn.Ping(ctx); err != nil {
		return nil, err
	}
	return &dto.ReadinessResponse{Status: "ready"}, nil
}

func (s *InfraService) ListMarkets(_ context.Context) (*dto.MarketCatalogResponse, error) {
	return &dto.MarketCatalogResponse{
		Markets: []dto.MarketDescriptor{
			{
				Name:         "crypto-options",
				Status:       "available",
				Capabilities: []string{"bars", "symbols", "greeks", "backtest", "chain-cache"},
			},
			{
				Name:         "us-options",
				Status:       "available",
				Capabilities: []string{"bars", "symbols", "greeks", "chain-cache", "sessionized-kline"},
			},
			{
				Name:         "us-stocks",
				Status:       "available",
				Capabilities: []string{"bars", "symbols", "sessionized-kline"},
			},
			{
				Name:         "feature-store",
				Status:       "available",
				Capabilities: []string{"volatility-snapshots", "volatility-history", "term-structure-snapshots", "skew-snapshots", "liquidity-snapshots", "liquidity-history", "event-window-snapshots", "event-window-history", "daily-feature-panel", "backfill", "freshness-monitoring"},
			},
		},
	}, nil
}

func (s *InfraService) ListDatasets(ctx context.Context, req dto.DatasetQueryRequest) (*dto.DatasetCatalogResponse, error) {
	marketFilter := strings.ToLower(strings.TrimSpace(req.Market))
	statusFilter := strings.ToLower(strings.TrimSpace(req.Status))
	datasets := make([]dto.DatasetDescriptor, 0, len(infraDatasetSpecs))
	for _, spec := range infraDatasetSpecs {
		dataset, err := s.inspectDataset(ctx, spec)
		if err != nil {
			return nil, err
		}
		if marketFilter != "" && strings.ToLower(dataset.Market) != marketFilter {
			continue
		}
		if statusFilter != "" && strings.ToLower(dataset.Status) != statusFilter {
			continue
		}
		datasets = append(datasets, dataset)
	}
	summary := summarizeDatasets(datasets)
	return &dto.DatasetCatalogResponse{Summary: summary, Datasets: datasets}, nil
}

func (s *InfraService) inspectDataset(ctx context.Context, spec infraDatasetSpec) (dto.DatasetDescriptor, error) {
	exists, err := s.relationExists(ctx, spec.Relation)
	if err != nil {
		return dto.DatasetDescriptor{}, fmt.Errorf("inspect dataset %s existence: %w", spec.Name, err)
	}
	dataset := dto.DatasetDescriptor{
		Name:      spec.Name,
		Market:    spec.Market,
		Relation:  spec.Relation,
		Status:    "missing",
		Freshness: "unknown",
	}
	if !exists {
		return dataset, nil
	}

	rowCount, err := s.relationRowCount(ctx, spec.Relation)
	if err != nil {
		return dto.DatasetDescriptor{}, fmt.Errorf("inspect dataset %s row count: %w", spec.Name, err)
	}
	dataset.RowCount = rowCount

	timeField := spec.TimeField
	if timeField == "" {
		timeField = "timestamp"
	}
	lastTS, hasData, err := s.relationLastTimestamp(ctx, spec.Relation, timeField)
	if err != nil {
		return dto.DatasetDescriptor{}, fmt.Errorf("inspect dataset %s freshness: %w", spec.Name, err)
	}
	if !hasData {
		dataset.Status = "empty"
		dataset.Freshness = "empty"
		return dataset, nil
	}
	ageSeconds := int64(time.Since(lastTS).Seconds())
	dataset.AgeSeconds = &ageSeconds
	dataset.LastTimestamp = &lastTS
	if spec.MaxAge > 0 && time.Since(lastTS) > spec.MaxAge {
		dataset.Status = "stale"
		dataset.Freshness = "stale"
		return dataset, nil
	}
	dataset.Status = "ready"
	dataset.Freshness = "fresh"
	return dataset, nil
}

func (s *InfraService) relationExists(ctx context.Context, relation string) (bool, error) {
	rows, err := s.conn.Query(ctx, `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = {relation:String}`, clickhouse.Named("relation", relation))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *InfraService) relationLastTimestamp(ctx context.Context, relation, timeField string) (time.Time, bool, error) {
	query := fmt.Sprintf(`SELECT ifNull(toDateTime(maxOrNull(%s), 'UTC'), toDateTime(0, 'UTC')) AS last_ts FROM %s`, timeField, relation)
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return time.Time{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var lastTS time.Time
	if err := rows.Scan(&lastTS); err != nil {
		return time.Time{}, false, err
	}
	if lastTS.IsZero() || lastTS.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return lastTS.UTC(), true, nil
}

func (s *InfraService) relationRowCount(ctx context.Context, relation string) (uint64, error) {
	query := fmt.Sprintf(`SELECT toUInt64(count()) FROM %s`, relation)
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func summarizeDatasets(datasets []dto.DatasetDescriptor) dto.DatasetSummary {
	summary := dto.DatasetSummary{Total: len(datasets)}
	marketSummaries := make(map[string]*dto.DatasetMarketSummary)
	for _, dataset := range datasets {
		switch strings.ToLower(dataset.Status) {
		case "ready":
			summary.Ready++
		case "stale":
			summary.Stale++
		case "missing":
			summary.Missing++
		case "empty":
			summary.Empty++
		}

		marketSummary, ok := marketSummaries[dataset.Market]
		if !ok {
			marketSummary = &dto.DatasetMarketSummary{Market: dataset.Market}
			marketSummaries[dataset.Market] = marketSummary
		}
		marketSummary.Total++
		switch strings.ToLower(dataset.Status) {
		case "ready":
			marketSummary.Ready++
		case "stale":
			marketSummary.Stale++
		case "missing":
			marketSummary.Missing++
		case "empty":
			marketSummary.Empty++
		}
	}

	summary.Markets = make([]dto.DatasetMarketSummary, 0, len(marketSummaries))
	for _, marketSummary := range marketSummaries {
		summary.Markets = append(summary.Markets, *marketSummary)
	}
	sort.Slice(summary.Markets, func(i, j int) bool {
		return summary.Markets[i].Market < summary.Markets[j].Market
	})
	return summary
}

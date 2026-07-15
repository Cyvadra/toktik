package service

import (
	"context"
	"fmt"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/api"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

const apiTrafficMinuteTable = "api_traffic_minute"

// TrafficStatsService persists durable API traffic aggregates.
type TrafficStatsService struct {
	repo *chrepo.Repo
}

func NewTrafficStatsService(repo *chrepo.Repo) *TrafficStatsService {
	return &TrafficStatsService{repo: repo}
}

func (s *TrafficStatsService) WriteTrafficMinutes(ctx context.Context, rows []api.TrafficMinute) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
    minute_ts, method, route, status_class, request_count, ingress_bytes,
    egress_bytes, peak_ingress_bytes, peak_egress_bytes, peak_total_bytes
)`, apiTrafficMinuteTable))
	if err != nil {
		return fmt.Errorf("prepare API traffic insert: %w", err)
	}
	for _, row := range rows {
		if err := batch.Append(
			row.Minute,
			row.Method,
			row.Route,
			row.StatusClass,
			row.RequestCount,
			row.IngressBytes,
			row.EgressBytes,
			row.PeakIngressBytes,
			row.PeakEgressBytes,
			row.PeakTotalBytes,
		); err != nil {
			return fmt.Errorf("append API traffic row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send API traffic rows: %w", err)
	}
	return nil
}

func (s *TrafficStatsService) QueryTrafficStats(ctx context.Context, req dto.TrafficStatsRequest) (*dto.TrafficStatsResponse, error) {
	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	if to.Sub(from) > 90*24*time.Hour {
		return nil, dto.NewValidationError("traffic statistics range must not exceed 90 days")
	}
	points, err := s.queryTrafficHours(ctx, from, to)
	if err != nil {
		return nil, err
	}
	summary, err := s.queryTrafficSummary(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return &dto.TrafficStatsResponse{From: from, To: to, Interval: "1h", Data: points, Summary: summary}, nil
}

func (s *TrafficStatsService) queryTrafficHours(ctx context.Context, from, to time.Time) ([]dto.TrafficStatsPoint, error) {
	rows, err := s.repo.Query(ctx, `
SELECT
    toStartOfHour(minute_ts) AS timestamp,
    sum(request_count), sum(ingress_bytes), sum(egress_bytes),
    max(peak_ingress_bytes), max(peak_egress_bytes), max(peak_total_bytes)
FROM api_traffic_minute FINAL
WHERE minute_ts >= {from:DateTime('UTC')} AND minute_ts < {to:DateTime('UTC')}
GROUP BY timestamp
ORDER BY timestamp`, clickhouse.Named("from", from), clickhouse.Named("to", to))
	if err != nil {
		return nil, fmt.Errorf("query API traffic hours: %w", err)
	}
	defer rows.Close()
	points := make([]dto.TrafficStatsPoint, 0)
	for rows.Next() {
		var point dto.TrafficStatsPoint
		if err := rows.Scan(&point.Timestamp, &point.RequestCount, &point.IngressBytes, &point.EgressBytes, &point.PeakIngressBytes, &point.PeakEgressBytes, &point.PeakTotalBytes); err != nil {
			return nil, fmt.Errorf("scan API traffic hour: %w", err)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API traffic hours: %w", err)
	}
	return points, nil
}

func (s *TrafficStatsService) queryTrafficSummary(ctx context.Context, from, to time.Time) (dto.TrafficStatsSummary, error) {
	var summary dto.TrafficStatsSummary
	rows, err := s.repo.Query(ctx, `
SELECT
    sum(request_count), sum(ingress_bytes), sum(egress_bytes),
    max(ingress_bytes), max(egress_bytes), max(ingress_bytes + egress_bytes),
    max(peak_ingress_bytes), max(peak_egress_bytes), max(peak_total_bytes)
FROM api_traffic_minute FINAL
WHERE minute_ts >= {from:DateTime('UTC')} AND minute_ts < {to:DateTime('UTC')}`,
		clickhouse.Named("from", from), clickhouse.Named("to", to))
	if err != nil {
		return summary, fmt.Errorf("query API traffic summary: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(
			&summary.RequestCount, &summary.IngressBytes, &summary.EgressBytes,
			&summary.PeakMinuteIngressBytes, &summary.PeakMinuteEgressBytes, &summary.PeakMinuteTotalBytes,
			&summary.PeakFiveSecondIngress, &summary.PeakFiveSecondEgress, &summary.PeakFiveSecondTotal,
		); err != nil {
			return summary, fmt.Errorf("scan API traffic summary: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("iterate API traffic summary: %w", err)
	}
	summary.PeakFiveSecondIngressMbps = bytesPerFiveSecondsToMbps(summary.PeakFiveSecondIngress)
	summary.PeakFiveSecondEgressMbps = bytesPerFiveSecondsToMbps(summary.PeakFiveSecondEgress)
	summary.PeakFiveSecondTotalMbps = bytesPerFiveSecondsToMbps(summary.PeakFiveSecondTotal)
	return summary, nil
}

func bytesPerFiveSecondsToMbps(bytes uint64) float64 {
	return float64(bytes) * 8 / (5 * 1_000_000)
}

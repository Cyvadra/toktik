package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

func TestTrafficMeterAssignsEventsToTheirMinuteAndTracksPeakWindow(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 1, 10, 0, time.UTC)
	meter := newTrafficMeter(func() time.Time { return now })
	request := meter.NewRequest()
	request.addIngress(time.Date(2026, time.July, 15, 12, 0, 58, 0, time.UTC), 10)
	request.addEgress(time.Date(2026, time.July, 15, 12, 0, 59, 0, time.UTC), 20)
	request.addEgress(time.Date(2026, time.July, 15, 12, 1, 1, 0, time.UTC), 30)
	request.Finish(TrafficRecord{Method: "GET", Route: "/api/v1/test", StatusClass: 200})

	minutes := meter.Snapshot(time.Date(2026, time.July, 15, 12, 2, 0, 0, time.UTC))
	if len(minutes) != 2 {
		t.Fatalf("expected 2 minute aggregates, got %d", len(minutes))
	}

	var first, second TrafficMinute
	for _, minute := range minutes {
		switch minute.Minute.Minute() {
		case 0:
			first = minute
		case 1:
			second = minute
		}
	}
	if first.IngressBytes != 10 || first.EgressBytes != 20 || first.PeakTotalBytes != 30 {
		t.Fatalf("unexpected first-minute aggregate: %#v", first)
	}
	if first.RequestCount != 0 {
		t.Fatalf("expected request count only in completion minute, got %#v", first)
	}
	if second.RequestCount != 1 || second.EgressBytes != 30 || second.PeakEgressBytes != 30 || second.PeakTotalBytes != 30 {
		t.Fatalf("unexpected second-minute aggregate: %#v", second)
	}
}

func TestTrafficMeterGroupsFiveSecondPeakWindows(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	meter := newTrafficMeter(func() time.Time { return now })
	request := meter.NewRequest()
	request.addEgress(now.Add(time.Second), 10)
	request.addEgress(now.Add(4*time.Second), 20)
	request.addEgress(now.Add(6*time.Second), 25)
	request.Finish(TrafficRecord{Method: "GET", Route: "/api/v1/test", StatusClass: 200})

	minutes := meter.Snapshot(now.Add(time.Minute))
	if len(minutes) != 1 {
		t.Fatalf("expected one minute aggregate, got %d", len(minutes))
	}
	if got := minutes[0].PeakEgressBytes; got != 30 {
		t.Fatalf("expected peak egress 30 bytes, got %d", got)
	}
}

func TestTrafficMeterMiddlewareRecordsKnownRequestAndResponseBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	meter := NewTrafficMeter()
	router := gin.New()
	router.Use(TrafficMeterMiddleware(meter))
	router.POST("/api/v1/test", func(c *gin.Context) {
		_, _ = io.Copy(io.Discard, c.Request.Body)
		_, _ = c.Writer.WriteString("response")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader("input"))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	minutes := meter.Snapshot(time.Now().UTC().Add(time.Minute))
	if len(minutes) != 1 {
		t.Fatalf("expected one minute aggregate, got %d", len(minutes))
	}
	minute := minutes[0]
	if minute.RequestCount != 1 || minute.IngressBytes != 5 || minute.EgressBytes != 8 {
		t.Fatalf("unexpected traffic aggregate: %#v", minute)
	}
}

type mockTrafficStatsProvider struct {
	response *dto.TrafficStatsResponse
	err      error
}

func (m mockTrafficStatsProvider) QueryTrafficStats(_ context.Context, _ dto.TrafficStatsRequest) (*dto.TrafficStatsResponse, error) {
	return m.response, m.err
}

func TestTrafficStatsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouterFromDeps(Deps{
		Config: config.DefaultRuntime(),
		TrafficStats: mockTrafficStatsProvider{response: &dto.TrafficStatsResponse{
			Interval: "1h",
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/traffic?from=2026-07-01&to=2026-07-02", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestTrafficStatsRouteReturnsNotImplementedWithoutProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouterFromDeps(Deps{Config: config.DefaultRuntime()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/traffic?from=2026-07-01&to=2026-07-02", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", resp.Code, resp.Body.String())
	}
}

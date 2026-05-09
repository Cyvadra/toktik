package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/gin-gonic/gin"
)

func TestRateLimitMiddlewareBypassesLocalClients(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		remote string
	}{
		{name: "loopback ipv4", remote: "127.0.0.1:1234"},
		{name: "private ipv4", remote: "192.168.1.10:1234"},
		{name: "loopback ipv6", remote: "[::1]:1234"},
		{name: "ula ipv6", remote: "[fd00::1]:1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			stop := make(chan struct{})
			defer close(stop)
			r.Use(RateLimitMiddleware(config.API{RateLimitRPS: 1}, stop))
			r.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			for range 4 {
				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.RemoteAddr = tt.remote
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("expected local client to bypass rate limit, got status %d", w.Code)
				}
			}
		})
	}
}

func TestRateLimitMiddlewareStillLimitsPublicClients(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	stop := make(chan struct{})
	defer close(stop)
	r.Use(RateLimitMiddleware(config.API{RateLimitRPS: 1}, stop))
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := range 3 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "8.8.8.8:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("request %d: expected status %d, got %d", i+1, want, w.Code)
		}
	}
}

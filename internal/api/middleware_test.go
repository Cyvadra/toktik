package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Cyvadra/toktik/internal/apikeyauth"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/gin-gonic/gin"
)

type fakeAPIKeyAuthenticator struct {
	keys map[string]apikeyauth.Principal
	err  error
}

func (f fakeAPIKeyAuthenticator) AuthenticateAPIKey(_ context.Context, plaintext string) (apikeyauth.Principal, bool, error) {
	if f.err != nil {
		return apikeyauth.Principal{}, false, f.err
	}
	principal, ok := f.keys[plaintext]
	return principal, ok, nil
}

func TestCORSMiddlewareDefaultsToLANOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		origin      string
		wantAllowed bool
	}{
		{name: "localhost", origin: "http://localhost:5173", wantAllowed: true},
		{name: "private ipv4", origin: "http://192.168.1.20:5173", wantAllowed: true},
		{name: "private ipv6", origin: "http://[fd00::1]:5173", wantAllowed: true},
		{name: "mdns", origin: "http://toktik.local:5173", wantAllowed: true},
		{name: "public", origin: "https://example.com", wantAllowed: false},
		{name: "invalid", origin: "not a url", wantAllowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(CORSMiddleware(config.API{}))
			r.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Origin", tt.origin)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			allowed := w.Header().Get("Access-Control-Allow-Origin") == tt.origin
			if allowed != tt.wantAllowed {
				t.Fatalf("expected allowed=%v for origin %q, got headers %#v", tt.wantAllowed, tt.origin, w.Header())
			}
		})
	}
}

func TestCORSMiddlewareExplicitOriginsOverrideLANDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CORSMiddleware(config.API{CORSOrigins: []string{"https://app.example"}}))
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("expected explicit origin to be allowed, got %q", got)
	}
}

func TestRouterClientIPHonorsOnlyConfiguredTrustedProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		trustedProxies []string
		wantClientIP   string
	}{
		{
			name:           "trusted local nginx forwards client IP",
			trustedProxies: []string{"127.0.0.1"},
			wantClientIP:   "203.0.113.25",
		},
		{
			name:         "unconfigured proxy headers are ignored",
			wantClientIP: "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := config.DefaultRuntime()
			runtime.API.TrustedProxies = tt.trustedProxies
			r := NewRouterFromDeps(Deps{
				Config: runtime,
				APIKeys: fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
					"secret": {ID: 1, KeyDigest: "secret-digest"},
				}},
			})
			r.GET("/client-ip", func(c *gin.Context) {
				c.String(http.StatusOK, c.ClientIP())
			})

			req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
			req.RemoteAddr = "127.0.0.1:54321"
			req.Header.Set("X-API-Key", "secret")
			req.Header.Set("X-Forwarded-For", "203.0.113.25")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
			}
			if got := w.Body.String(); got != tt.wantClientIP {
				t.Fatalf("expected client IP %q, got %q", tt.wantClientIP, got)
			}
		})
	}
}

func TestAPIKeyAuthOnlyBypassesExplicitPublicPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(APIKeyAuth(config.API{}, fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
		"secret": {ID: 1, KeyDigest: "secret-digest"},
	}}))
	r.GET("/utils/us-stocks/logos/:symbol", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.GET("/utils/other", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	publicReq := httptest.NewRequest(http.MethodGet, "/utils/us-stocks/logos/AAPL.png", nil)
	publicResp := httptest.NewRecorder()
	r.ServeHTTP(publicResp, publicReq)
	if publicResp.Code != http.StatusOK {
		t.Fatalf("expected logo utility to bypass auth, got status %d", publicResp.Code)
	}

	privateReq := httptest.NewRequest(http.MethodGet, "/utils/other", nil)
	privateResp := httptest.NewRecorder()
	r.ServeHTTP(privateResp, privateReq)
	if privateResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected unrelated utility route to require auth, got status %d", privateResp.Code)
	}
}

func TestAPIKeyAuthRequiresKeysForLANClients(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		remote string
		want   int
	}{
		{name: "loopback", remote: "127.0.0.1:1234", want: http.StatusUnauthorized},
		{name: "private", remote: "192.168.1.10:1234", want: http.StatusUnauthorized},
		{name: "public", remote: "8.8.8.8:1234", want: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(APIKeyAuth(config.API{}, fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
				"secret": {ID: 1, KeyDigest: "secret-digest"},
			}}))
			r.GET("/api/v1/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
			req.RemoteAddr = tt.remote
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d", tt.want, w.Code)
			}
		})
	}
}

func TestAPIKeyAuthAcceptsValidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(APIKeyAuth(config.API{}, fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
		"secret": {ID: 1, KeyDigest: "secret-digest"},
	}}))
	r.GET("/api/v1/test", func(c *gin.Context) {
		principal, ok := getAPIKeyPrincipal(c)
		if !ok || principal.ID != 1 {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("X-API-Key", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected valid key to pass, got status %d", w.Code)
	}
}

func TestAPIKeyAuthCanBypassLocalClientsWhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		remote string
		want   int
	}{
		{name: "loopback", remote: "127.0.0.1:1234", want: http.StatusOK},
		{name: "private", remote: "192.168.1.10:1234", want: http.StatusOK},
		{name: "public", remote: "8.8.8.8:1234", want: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(APIKeyAuth(config.API{BypassAuthForLocalClients: true}, fakeAPIKeyAuthenticator{keys: map[string]apikeyauth.Principal{
				"secret": {ID: 1, KeyDigest: "secret-digest"},
			}}))
			r.GET("/api/v1/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
			req.RemoteAddr = tt.remote
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d", tt.want, w.Code)
			}
		})
	}
}

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

func TestRateLimitMiddlewareUsesPrincipalRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	stop := make(chan struct{})
	defer close(stop)
	keyRPS := 1.0
	r.Use(func(c *gin.Context) {
		setAPIKeyPrincipal(c, apikeyauth.Principal{ID: 1, KeyDigest: "digest-1", RateLimitRPS: &keyRPS})
		c.Next()
	})
	r.Use(RateLimitMiddleware(config.API{RateLimitRPS: 100}, stop))
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

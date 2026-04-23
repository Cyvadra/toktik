package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const (
	// rateLimitMaxBuckets caps the in-memory bucket map to prevent
	// unbounded growth from spoofed/rotating client IPs.
	rateLimitMaxBuckets = 100_000
	// rateLimitBucketTTL is how long an idle bucket survives before eviction.
	rateLimitBucketTTL = 10 * time.Minute
	// rateLimitEvictInterval is how frequently the eviction sweep runs.
	rateLimitEvictInterval = 5 * time.Minute
)

// CORSMiddleware returns a gin middleware that handles CORS using the
// supplied API config. If no origins are configured, all origins are
// allowed and credentials are disabled.
func CORSMiddleware(cfg config.API) gin.HandlerFunc {
	c := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	if len(cfg.CORSOrigins) == 0 {
		c.AllowAllOrigins = true
		c.AllowCredentials = false
	} else {
		c.AllowOrigins = cfg.CORSOrigins
	}
	return cors.New(c)
}

// APIKeyAuth returns a gin middleware that checks the X-API-Key header.
// Keys are stored as SHA-256 digests so a process memory dump does not
// leak plaintext credentials. Comparison is constant time.
//
// If cfg.APIKeys is empty the returned middleware is a no-op.
func APIKeyAuth(cfg config.API) gin.HandlerFunc {
	if len(cfg.APIKeys) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	digests := make([][]byte, 0, len(cfg.APIKeys))
	for _, key := range cfg.APIKeys {
		if key == "" {
			continue
		}
		sum := sha256.Sum256([]byte(key))
		digests = append(digests, sum[:])
	}
	if len(digests) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "missing API key"})
			return
		}
		sum := sha256.Sum256([]byte(apiKey))
		for _, d := range digests {
			if subtle.ConstantTimeCompare(sum[:], d) == 1 {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse{Error: "invalid API key"})
	}
}

// rateBucket is one token bucket entry.
type rateBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimitMiddleware returns a gin middleware enforcing a per-key token
// bucket rate limit. Keys come from the X-API-Key header and fall back to
// gin's c.ClientIP() (which honours configured trusted proxies).
//
// The bucket map is bounded; when full, new entries displace the oldest one
// to prevent memory exhaustion via spoofed identifiers. The eviction
// goroutine stops when the supplied stop channel is closed.
func RateLimitMiddleware(cfg config.API, stop <-chan struct{}) gin.HandlerFunc {
	rps := cfg.RateLimitRPS
	if rps <= 0 {
		// Misconfiguration: disable instead of dividing by zero.
		return func(c *gin.Context) { c.Next() }
	}
	burst := rps * 2

	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)

	go func() {
		ticker := time.NewTicker(rateLimitEvictInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				mu.Lock()
				now := time.Now()
				for k, b := range buckets {
					if now.Sub(b.lastRefill) > rateLimitBucketTTL {
						delete(buckets, k)
					}
				}
				mu.Unlock()
			}
		}
	}()

	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.ClientIP()
		}

		mu.Lock()
		b, ok := buckets[key]
		if !ok {
			if len(buckets) >= rateLimitMaxBuckets {
				// Drop the oldest entry to bound memory.
				var oldestKey string
				var oldest time.Time
				for k, v := range buckets {
					if oldestKey == "" || v.lastRefill.Before(oldest) {
						oldestKey = k
						oldest = v.lastRefill
					}
				}
				delete(buckets, oldestKey)
			}
			b = &rateBucket{tokens: burst, lastRefill: time.Now()}
			buckets[key] = b
		}

		now := time.Now()
		elapsed := now.Sub(b.lastRefill).Seconds()
		b.tokens += elapsed * rps
		if b.tokens > burst {
			b.tokens = burst
		}
		b.lastRefill = now

		if b.tokens < 1 {
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, dto.ErrorResponse{Error: "rate limit exceeded"})
			return
		}
		b.tokens--
		mu.Unlock()
		c.Next()
	}
}

// SecurityHeadersMiddleware sets baseline response headers that mitigate
// common browser-based attacks. CSP is intentionally restrictive so that
// any HTML response (including backtest reports) cannot load remote
// scripts or be framed by other origins.
func SecurityHeadersMiddleware() gin.HandlerFunc {
	const csp = "default-src 'self'; script-src 'self' 'unsafe-inline'; " +
		"style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
		"frame-ancestors 'none'; base-uri 'self'"
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		c.Next()
	}
}

// RequestTimeoutMiddleware applies a per-request context deadline so a
// slow upstream cannot hold a goroutine indefinitely. Streaming endpoints
// (SSE, HTML reports) opt out via the skip predicate.
func RequestTimeoutMiddleware(timeout time.Duration, skip func(*gin.Context) bool) gin.HandlerFunc {
	if timeout <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if skip != nil && skip(c) {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// SlogRecoveryMiddleware logs panics through slog and returns a 500.
func SlogRecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in handler",
					"error", rec,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"stack", string(debug.Stack()),
				)
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "internal server error"})
				}
			}
		}()
		c.Next()
	}
}

// SlogRequestLogger logs a single structured line per request.
func SlogRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		level := slog.LevelInfo
		status := c.Writer.Status()
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		slog.Log(c.Request.Context(), level, "http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

// keyDigestHex returns a short hex prefix of a key's SHA-256 digest,
// for use in diagnostic logs that must not leak the key itself.
func keyDigestHex(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

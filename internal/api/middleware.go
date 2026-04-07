package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORSMiddleware returns a gin middleware that handles CORS.
// Allowed origins come from the CORS_ORIGINS env var (comma-separated).
// If unset, defaults to allowing all origins.
func CORSMiddleware() gin.HandlerFunc {
	origins := os.Getenv("CORS_ORIGINS")
	cfg := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	if origins == "" {
		cfg.AllowAllOrigins = true
		cfg.AllowCredentials = false
	} else {
		cfg.AllowOrigins = strings.Split(origins, ",")
	}
	return cors.New(cfg)
}

// APIKeyAuth returns a gin middleware that checks the X-API-Key header.
// Valid keys come from the API_KEYS env var (comma-separated).
// If API_KEYS is empty, authentication is disabled (all requests pass).
func APIKeyAuth() gin.HandlerFunc {
	raw := os.Getenv("API_KEYS")
	if raw == "" {
		return func(c *gin.Context) { c.Next() }
	}

	keys := make(map[string]struct{})
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keys[k] = struct{}{}
		}
	}
	if len(keys) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing API key"})
			return
		}
		if _, ok := keys[apiKey]; !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
			return
		}
		c.Next()
	}
}

// RateLimiter provides a simple per-key token bucket rate limiter.
type rateBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimitMiddleware returns a gin middleware enforcing rate limits.
// Rate is configured via RATE_LIMIT_RPS env var (requests per second, default 50).
// Burst is 2× the RPS. Keyed by X-API-Key header or remote IP.
func RateLimitMiddleware() gin.HandlerFunc {
	rps := 50.0
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			rps = parsed
		}
	}
	burst := rps * 2

	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)

	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.ClientIP()
		}

		mu.Lock()
		b, ok := buckets[key]
		if !ok {
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
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		b.tokens--
		mu.Unlock()
		c.Next()
	}
}

package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORSMiddleware returns a gin middleware that handles CORS.
// Allowed origins come from runtime config.
// If unset, defaults to allowing all origins.
func CORSMiddleware() gin.HandlerFunc {
	runtimeCfg := loadRuntimeConfigOrDefault()
	cfg := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	if len(runtimeCfg.API.CORSOrigins) == 0 {
		cfg.AllowAllOrigins = true
		cfg.AllowCredentials = false
	} else {
		cfg.AllowOrigins = runtimeCfg.API.CORSOrigins
	}
	return cors.New(cfg)
}

// APIKeyAuth returns a gin middleware that checks the X-API-Key header.
// Valid keys come from runtime config.
// If API_KEYS is empty, authentication is disabled (all requests pass).
func APIKeyAuth() gin.HandlerFunc {
	runtimeCfg := loadRuntimeConfigOrDefault()
	if len(runtimeCfg.API.APIKeys) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	keys := make(map[string]struct{}, len(runtimeCfg.API.APIKeys))
	for _, key := range runtimeCfg.API.APIKeys {
		keys[key] = struct{}{}
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
// Rate is configured via runtime config (requests per second, default 50).
// Burst is 2× the RPS. Keyed by X-API-Key header or remote IP.
// Stale buckets are evicted every 5 minutes.
func RateLimitMiddleware() gin.HandlerFunc {
	rps := loadRuntimeConfigOrDefault().API.RateLimitRPS
	burst := rps * 2

	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)

	// Evict stale buckets periodically.
	const evictInterval = 5 * time.Minute
	const bucketTTL = 10 * time.Minute
	go func() {
		ticker := time.NewTicker(evictInterval)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for k, b := range buckets {
				if now.Sub(b.lastRefill) > bucketTTL {
					delete(buckets, k)
				}
			}
			mu.Unlock()
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

func loadRuntimeConfigOrDefault() config.Runtime {
	runtimeCfg, err := config.LoadRuntime()
	if err != nil {
		return config.DefaultRuntime()
	}
	return runtimeCfg
}

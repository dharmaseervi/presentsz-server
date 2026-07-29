package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
	limit   int
	window  time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		limit:   limit,
		window:  window,
	}
	go rl.cleanup()
	return rl
}

// cleanup periodically evicts expired entries so the map doesn't grow
// unbounded over the life of the process.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for k, v := range rl.entries {
			if now.After(v.resetAt) {
				delete(rl.entries, k)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow reports whether `key` is still under the limit for the current
// window, incrementing its counter as a side effect.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[key]

	if !exists || now.After(entry.resetAt) {
		rl.entries[key] = &rateLimitEntry{count: 1, resetAt: now.Add(rl.window)}
		return true
	}

	if entry.count >= rl.limit {
		return false
	}

	entry.count++
	return true
}

// Middleware returns Gin middleware that rate-limits by client IP,
// combined with the route path so different endpoints get independent limits.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.FullPath()
		if !rl.Allow(key) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please wait a few minutes and try again.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

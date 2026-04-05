package middleware

import (
	"net/http"
	"srm-academia-scraper/logger"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// allowedCORSOrigins are the only browser Origins that receive Access-Control-Allow-Origin.
// Impact: other web apps cannot read responses from this API in the browser; server-to-server calls are unchanged.
var allowedCORSOrigins = map[string]struct{}{
	"http://localhost:3000":         {},
	"https://sdashsrm.vercel.app": {},
}

func corsAllowOrigin(origin string) bool {
	_, ok := allowedCORSOrigins[strings.TrimSpace(origin)]
	return ok
}

// RateLimiter stores rate limiters for each IP
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.Mutex
	rate     rate.Limit
	burst    int
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     r,
		burst:    b,
	}
}

// GetLimiter returns the rate limiter for a given IP
func (rl *RateLimiter) GetLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.limiters[ip] = limiter
	}

	return limiter
}

// Middleware returns a rate limiting middleware
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		ip := r.RemoteAddr
		
		// Get rate limiter for this IP
		limiter := rl.GetLimiter(ip)

		if !limiter.Allow() {
			logger.Warn("rate_limit", "Rate limit exceeded", map[string]interface{}{
				"ip":     ip,
				"path":   r.URL.Path,
				"method": r.Method,
			})

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			// Message aligned with login cooldown UX so clients can show one consistent hint.
			w.Write([]byte(`{"error": "Please try again after a few minutes"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CleanupOldLimiters removes old limiters periodically
func (rl *RateLimiter) CleanupOldLimiters(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			rl.mu.Lock()
			// Clear all limiters (simple cleanup)
			rl.limiters = make(map[string]*rate.Limiter)
			rl.mu.Unlock()
			logger.Debug("rate_limit_cleanup", "Cleaned up old rate limiters", nil)
		}
	}()
}

// CORS middleware: reflect Allow-Origin only for sdash dev and production; omit for others so browsers block reads.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && corsAllowOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-Id, X-Email, X-Password, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging middleware
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logger.Info("http_request", "Incoming request", map[string]interface{}{
			"method": r.Method,
			"path":   r.URL.Path,
			"ip":     r.RemoteAddr,
		})

		next.ServeHTTP(w, r)

		logger.Info("http_response", "Request completed", map[string]interface{}{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start).Milliseconds(),
		})
	})
}

package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/response"
)

type clientRate struct {
	tokens   float64
	lastSeen time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*clientRate
	rate     float64
	capacity float64
}

func NewRateLimiter(requestsPerMinute int) *RateLimiter {
	rl := &RateLimiter{
		clients:  make(map[string]*clientRate),
		rate:     float64(requestsPerMinute) / 60.0,
		capacity: float64(requestsPerMinute),
	}

	go rl.cleanup(5 * time.Minute)
	return rl
}

func (rl *RateLimiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, client := range rl.clients {
			if now.Sub(client.lastSeen) > 3*time.Minute {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	client, exists := rl.clients[ip]
	if !exists {
		rl.clients[ip] = &clientRate{
			tokens:   rl.capacity - 1,
			lastSeen: now,
		}
		return true
	}

	elapsed := now.Sub(client.lastSeen).Seconds()
	client.lastSeen = now
	client.tokens += elapsed * rl.rate
	if client.tokens > rl.capacity {
		client.tokens = rl.capacity
	}

	if client.tokens >= 1 {
		client.tokens -= 1
		return true
	}

	return false
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func RateLimit(requestsPerMinute int) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(requestsPerMinute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			ip := getClientIP(r)
			if !limiter.allow(ip) {
				w.Header().Set("Retry-After", "1")
				response.SetError(r.Context(), fmt.Errorf("rate limit exceeded for ip %s", ip))
				response.TooManyRequests(w, r, "")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/tecsharp/posterndb/internal/config"
)

// RateLimiter is a per-client-IP token bucket used on the login endpoint to
// slow down password guessing.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*entry
	limit    rate.Limit
	burst    int
	idleTTL  time.Duration
	now      func() time.Time
}

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter allows perMinute requests per IP with the given burst.
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: map[string]*entry{},
		limit:    rate.Every(time.Minute / time.Duration(perMinute)),
		burst:    burst,
		idleTTL:  15 * time.Minute,
		now:      time.Now,
	}
}

// Allow reports whether a request from key may proceed.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.evict(now)
	e, ok := l.limiters[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.limiters[key] = e
	}
	e.lastSeen = now
	return e.limiter.AllowN(now, 1)
}

// evict drops idle buckets so the map cannot grow without bound.
func (l *RateLimiter) evict(now time.Time) {
	for k, e := range l.limiters {
		if now.Sub(e.lastSeen) > l.idleTTL {
			delete(l.limiters, k)
		}
	}
}

// Middleware answers 429 when the caller exceeded its budget.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(ClientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP returns the caller IP. CF-Connecting-IP is trusted because the
// app port is never published to the internet: the only external path is
// cloudflared, which always overwrites this header with the real client IP.
// (The optional loopback port for local access is reachable only from the
// host itself.)
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get(config.HeaderCFConnectIP)); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

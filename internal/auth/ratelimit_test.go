package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tecsharp/posterndb/internal/config"
)

func TestRateLimiterBurstThenBlock(t *testing.T) {
	l := NewRateLimiter(5, 3)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := range 3 {
		assert.True(t, l.Allow("1.1.1.1"), "request %d", i)
	}
	assert.False(t, l.Allow("1.1.1.1"))
	assert.True(t, l.Allow("2.2.2.2"), "other IPs have their own bucket")

	now = now.Add(12 * time.Second) // 5/min refills one token every 12s
	assert.True(t, l.Allow("1.1.1.1"))
}

func TestRateLimiterEvictsIdleBuckets(t *testing.T) {
	l := NewRateLimiter(5, 1)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.Allow("1.1.1.1")
	now = now.Add(time.Hour)
	l.Allow("2.2.2.2")
	assert.Len(t, l.limiters, 1)
}

func TestRateLimiterMiddleware(t *testing.T) {
	l := NewRateLimiter(1, 1)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "60", rec.Header().Get("Retry-After"))
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:5555"
	assert.Equal(t, "10.0.0.5", ClientIP(r))

	r.Header.Set(config.HeaderCFConnectIP, " 203.0.113.9 ")
	assert.Equal(t, "203.0.113.9", ClientIP(r))

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "weird"
	assert.Equal(t, "weird", ClientIP(r))
}

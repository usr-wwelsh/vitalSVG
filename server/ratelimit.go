package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Per-IP token bucket. Burst is generous because one page load fans out to many
// badge requests at once; sustained rate is what actually throttles hammering.
const (
	rlRate  = 40.0  // tokens (requests) refilled per second per IP
	rlBurst = 120.0 // max tokens, i.e. peak burst a single IP can spend at once
)

type bucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
}

func newRateLimiter(rate, burst float64) *rateLimiter {
	rl := &rateLimiter{buckets: make(map[string]*bucket), rate: rate, burst: burst}
	go rl.cleanup()
	return rl
}

// allow consumes a token for key, refilling based on elapsed time. Returns
// false when the bucket is empty.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b := rl.buckets[key]
	if b == nil {
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}

	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// cleanup evicts idle buckets so the map can't grow unbounded.
func (rl *rateLimiter) cleanup() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if time.Since(b.last) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP resolves the real caller. The server only receives traffic via the
// Cloudflare Tunnel, so RemoteAddr is the tunnel's local address and is the same
// for everyone; CF-Connecting-IP is set by Cloudflare and cannot be spoofed by
// the client. X-Forwarded-For is a weaker fallback for other proxy setups.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

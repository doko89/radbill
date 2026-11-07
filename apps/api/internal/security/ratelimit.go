// Package security hosts cross-cutting HTTP security middleware for the
// billing API (G4 hardening, agent E2).
//
// The rate limiter keys on the direct TCP peer address (fasthttp RemoteIP)
// and deliberately ignores X-Forwarded-For / ProxyHeader: those headers are
// client-controlled and trivially spoofable, so trusting them would let an
// attacker rotate identities per request and bypass the limit. When the API
// runs behind a reverse proxy, the proxy / ingress must enforce its own
// limit upstream; this middleware remains a last-resort per-peer guard.
//
// Panel login already has a dedicated username+IP limiter (jwtauth
// LoginLimiter: 5 failures per 15 minutes). Limit is for generic per-IP
// throttling of public endpoints; E1 wires it into server.go.
package security

import (
	"math"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Defaults applied when Limit receives non-positive arguments.
const (
	defaultRPM   = 60
	defaultBurst = 1
)

// cleanupInterval is how often idle buckets are swept.
const cleanupInterval = time.Minute

// bucket is one token bucket: tokens available as of last.
type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is the token-bucket state shared by one Limit middleware.
// The zero refill math is pure (elapsed * rate, capped at burst); only
// allow/cleanup touch the mutex.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64 // max tokens
	ttl     time.Duration
	now     func() time.Time
}

// newLimiter builds a limiter, clamping non-positive inputs to defaults.
func newLimiter(rpm, burst int) *limiter {
	if rpm < 1 {
		rpm = defaultRPM
	}
	if burst < 1 {
		burst = defaultBurst
	}
	rate := float64(rpm) / 60.0
	// Idle TTL: time to fully refill from empty plus one cleanup interval
	// of margin, so active clients are never swept mid-window.
	ttl := time.Duration(float64(burst)/rate*float64(time.Second)) + cleanupInterval
	return &limiter{
		buckets: map[string]*bucket{},
		rate:    rate,
		burst:   float64(burst),
		ttl:     ttl,
		now:     time.Now,
	}
}

// Limit returns a per-IP token-bucket rate-limiting middleware.
//
// rpm is the sustained refill rate (requests/minute) and burst the maximum
// burst. The first burst requests pass immediately; further requests are
// rejected with 429 + Retry-After until tokens refill. Buckets are keyed by
// client IP (see package doc) and idle buckets are swept periodically.
//
// The rejection body uses the billing error envelope:
//
//	{error:{code:"rate_limited",message:"..."}}
//
// matching the jwtauth login limiter's code.
func Limit(rpm, burst int) fiber.Handler {
	l := newLimiter(rpm, burst)
	go func() {
		t := time.NewTicker(cleanupInterval)
		defer t.Stop()
		for range t.C {
			l.cleanup()
		}
	}()
	return func(c fiber.Ctx) error {
		ok, retryAfter := l.allow(clientIP(c))
		if ok {
			return c.Next()
		}
		secs := int64(1)
		if s := int64(math.Ceil(retryAfter.Seconds())); s > secs {
			secs = s
		}
		c.Set("Retry-After", strconv.FormatInt(secs, 10))
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": fiber.Map{"code": "rate_limited", "message": "too many requests, try again later"},
		})
	}
}

// allow consumes one token for ip. ok=false means limited; retryAfter is how
// long until one token is available.
func (l *limiter) allow(ip string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, found := l.buckets[ip]
	if !found {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	if wait < time.Second {
		wait = time.Second
	}
	return false, wait
}

// cleanup drops buckets idle longer than the TTL. Called periodically.
func (l *limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for ip, b := range l.buckets {
		if now.Sub(b.last) > l.ttl {
			delete(l.buckets, ip)
		}
	}
}

// clientIP returns the direct TCP peer IP, ignoring proxy headers (see
// package doc). It never returns "".
func clientIP(c fiber.Ctx) string {
	if rip := c.RequestCtx().RemoteIP(); rip != nil && rip.String() != "" {
		return rip.String()
	}
	if host, _, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil && host != "" {
		return host
	}
	if s := c.RemoteAddr().String(); s != "" {
		return s
	}
	return "unknown"
}

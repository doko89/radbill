package application

import (
	"sync"
	"time"

	authdomain "boilerplate/api/internal/jwtauth/domain"
)

// loginAttempt tracks consecutive failures inside one window.
type loginAttempt struct {
	count int
	first time.Time
}

// LoginLimiter is an in-memory rate limiter: max failures per window
// for each username+IP pair, then 429 until the window passes.
type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	max      int
	window   time.Duration
	now      func() time.Time
}

// NewLoginLimiter builds a limiter (max failures per window).
func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{attempts: map[string]*loginAttempt{}, max: max, window: window, now: time.Now}
}

// Check reports ErrTooManyAttempts when the pair is currently locked.
func (l *LoginLimiter) Check(username, ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[username+"\x00"+ip]
	if !ok {
		return nil
	}
	if l.now().Sub(a.first) >= l.window {
		delete(l.attempts, username+"\x00"+ip)
		return nil
	}
	if a.count >= l.max {
		return authdomain.ErrTooManyAttempts
	}
	return nil
}

// Fail records one failure for the pair.
func (l *LoginLimiter) Fail(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := username + "\x00" + ip
	a, ok := l.attempts[key]
	if !ok || l.now().Sub(a.first) >= l.window {
		l.attempts[key] = &loginAttempt{count: 1, first: l.now()}
		l.gc()
		return
	}
	a.count++
}

// Reset clears failures for the pair after a successful login.
func (l *LoginLimiter) Reset(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, username+"\x00"+ip)
}

// gc drops expired entries when the map grows large. Caller holds mu.
func (l *LoginLimiter) gc() {
	if len(l.attempts) < 10000 {
		return
	}
	now := l.now()
	for k, a := range l.attempts {
		if now.Sub(a.first) >= l.window {
			delete(l.attempts, k)
		}
	}
}

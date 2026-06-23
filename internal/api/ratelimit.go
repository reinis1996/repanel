package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// loginLimiter throttles failed authentication attempts per client IP to blunt
// online password guessing (see SECURITY_AUDIT F-08). It is a small in-memory
// sliding counter — good enough for a single-binary panel; a fail2ban filter on
// the panel log is the recommended belt-and-suspenders for distributed attacks.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptState
	max      int           // failures allowed within the window before lockout
	window   time.Duration // lockout / counting window
}

type attemptState struct {
	count   int
	resetAt time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{attempts: map[string]*attemptState{}, max: max, window: window}
}

// allowed reports whether key may attempt a login now, and the retry-after
// duration when it is locked out.
func (l *loginLimiter) allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.attempts[key]
	if st == nil || time.Now().After(st.resetAt) {
		return true, 0
	}
	if st.count >= l.max {
		return false, time.Until(st.resetAt)
	}
	return true, 0
}

// fail records a failed attempt for key.
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	st := l.attempts[key]
	if st == nil || now.After(st.resetAt) {
		st = &attemptState{resetAt: now.Add(l.window)}
		l.attempts[key] = st
	}
	st.count++
	// Opportunistically evict stale entries so the map can't grow unbounded.
	if len(l.attempts) > 10000 {
		for k, v := range l.attempts {
			if now.After(v.resetAt) {
				delete(l.attempts, k)
			}
		}
	}
}

// success clears the counter for key after a valid login.
func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

// clientIP extracts the best-effort client IP for rate-limiting purposes.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

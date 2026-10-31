package access

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) AllowRequest(r *http.Request) bool {
	if s == nil || s.anonymousLimiter == nil || r == nil {
		return true
	}
	if isLabPublicPath(r) {
		return true
	}
	settings := s.currentSettings(r.Context())
	return s.anonymousLimiter.Allow(accessRateLimitKey(r, s.cfg.TrustedProxies), settings.GlobalRateLimitRequests, settings.GlobalRateLimitWindow)
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{
		now:      func() time.Time { return time.Now().UTC() },
		requests: map[string][]time.Time{},
	}
}

func (l *requestLimiter) Allow(key string, max int, window time.Duration) bool {
	if l == nil || max <= 0 || window <= 0 {
		return true
	}
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-window)
	items := l.requests[key][:0]
	for _, ts := range l.requests[key] {
		if ts.After(cutoff) {
			items = append(items, ts)
		}
	}
	if len(items) >= max {
		l.requests[key] = items
		return false
	}
	items = append(items, now)
	l.requests[key] = items
	return true
}

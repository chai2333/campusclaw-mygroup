package auth

import (
	"sync"
	"time"
)

type attempt struct {
	count              int
	start, lockedUntil time.Time
}
type Limiter struct {
	mu            sync.Mutex
	entries       map[string]attempt
	Max, Capacity int
	Window, Lock  time.Duration
}

func NewLimiter(max int, window, lock time.Duration) *Limiter {
	return &Limiter{entries: make(map[string]attempt), Max: max, Capacity: 10000, Window: window, Lock: lock}
}
func (l *Limiter) prune(now time.Time) {
	for key, a := range l.entries {
		if !now.Before(a.lockedUntil) && !now.Before(a.start.Add(l.Window)) {
			delete(l.entries, key)
		}
	}
}
func (l *Limiter) Blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	a, ok := l.entries[key]
	return (ok && now.Before(a.lockedUntil)) || (!ok && len(l.entries) >= l.Capacity)
}
func (l *Limiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	a, ok := l.entries[key]
	if !ok && len(l.entries) >= l.Capacity {
		return
	}
	if now.Before(a.lockedUntil) {
		return
	}
	if !ok || !now.Before(a.start.Add(l.Window)) {
		a = attempt{start: now}
	}
	a.count++
	if a.count >= l.Max {
		a.lockedUntil = now.Add(l.Lock)
	}
	l.entries[key] = a
}
func (l *Limiter) Reset(key string) { l.mu.Lock(); defer l.mu.Unlock(); delete(l.entries, key) }

package auth

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := NewLimiter(2, time.Minute, time.Minute)
	now := time.Unix(1000, 0)
	l.Fail("a", now)
	if l.Blocked("a", now) {
		t.Fatal("premature lock")
	}
	l.Fail("a", now)
	if !l.Blocked("a", now) || l.Blocked("b", now) {
		t.Fatal("incorrect isolation")
	}
	if l.Blocked("a", now.Add(time.Minute)) {
		t.Fatal("lock not expired")
	}
	l.Capacity = 1
	l.Fail("a", now)
	if !l.Blocked("b", now) {
		t.Fatal("capacity not enforced")
	}
	l.Reset("a")
	if l.Blocked("b", now) {
		t.Fatal("reset failed")
	}
}

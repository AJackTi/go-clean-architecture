package ratelimit

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.RWMutex
	now time.Time
}

func newTestClock(now time.Time) *testClock { return &testClock{now: now} }

func (c *testClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func TestConfigValidateRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "zero capacity", cfg: Config{Capacity: 0, RefillRate: 1, MaxClients: 1}},
		{name: "negative capacity", cfg: Config{Capacity: -1, RefillRate: 1, MaxClients: 1}},
		{name: "oversized capacity", cfg: Config{Capacity: MaxCapacity + 1, RefillRate: 1, MaxClients: 1}},
		{name: "zero refill rate", cfg: Config{Capacity: 1, RefillRate: 0, MaxClients: 1}},
		{name: "negative refill rate", cfg: Config{Capacity: 1, RefillRate: -1, MaxClients: 1}},
		{name: "NaN refill rate", cfg: Config{Capacity: 1, RefillRate: math.NaN(), MaxClients: 1}},
		{name: "infinite refill rate", cfg: Config{Capacity: 1, RefillRate: math.Inf(1), MaxClients: 1}},
		{name: "oversized refill rate", cfg: Config{Capacity: 1, RefillRate: MaxRefillRate + 1, MaxClients: 1}},
		{name: "zero max clients", cfg: Config{Capacity: 1, RefillRate: 1, MaxClients: 0}},
		{name: "oversized max clients", cfg: Config{Capacity: 1, RefillRate: 1, MaxClients: MaxClients + 1}},
		{name: "negative idle TTL", cfg: Config{Capacity: 1, RefillRate: 1, MaxClients: 1, IdleTTL: -time.Second}},
		{name: "oversized idle TTL", cfg: Config{Capacity: 1, RefillRate: 1, MaxClients: 1, IdleTTL: MaxIdleTTL + time.Second}},
		{name: "unknown overflow policy", cfg: Config{Capacity: 1, RefillRate: 1, MaxClients: 1, Overflow: OverflowPolicy(99)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.cfg.Validate(); err == nil {
				t.Fatal("Config.Validate() succeeded; want validation error")
			}
			if _, err := New(test.cfg); err == nil {
				t.Fatal("New() succeeded; want validation error")
			}
		})
	}
}

func TestNewUsesSystemClockWhenClockIsOmitted(t *testing.T) {
	limiter, err := New(Config{Capacity: 1, RefillRate: 1, MaxClients: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if decision := limiter.Allow("client"); !decision.Allowed {
		t.Fatalf("first Allow() = %#v, want allowed", decision)
	}
}

func TestAllowRefillsAndReportsRemainingRetryAfterAndReset(t *testing.T) {
	start := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := newTestClock(start)
	limiter, err := New(Config{
		Capacity:   3,
		RefillRate: 1,
		MaxClients: 2,
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	decision := limiter.Allow("client")
	assertDecision(t, decision, true, 2, 0, start.Add(time.Second), ReasonAllowed)
	decision = limiter.Allow("client")
	assertDecision(t, decision, true, 1, 0, start.Add(2*time.Second), ReasonAllowed)
	decision = limiter.Allow("client")
	assertDecision(t, decision, true, 0, 0, start.Add(3*time.Second), ReasonAllowed)

	decision = limiter.Allow("client")
	assertDecision(t, decision, false, 0, time.Second, start.Add(3*time.Second), ReasonRateLimited)

	clock.Advance(500 * time.Millisecond)
	decision = limiter.Allow("client")
	assertDecision(t, decision, false, 0, 500*time.Millisecond, start.Add(3*time.Second), ReasonRateLimited)

	clock.Advance(500 * time.Millisecond)
	decision = limiter.Allow("client")
	assertDecision(t, decision, true, 0, 0, start.Add(4*time.Second), ReasonAllowed)
}

func TestAllowNAndInvalidInputs(t *testing.T) {
	clock := newTestClock(time.Unix(0, 0).UTC())
	limiter, err := New(Config{Capacity: 5, RefillRate: 5, MaxClients: 2, Clock: clock})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	assertDecision(t, limiter.AllowN("client", 3), true, 2, 0, time.Unix(0, 0).UTC().Add(600*time.Millisecond), ReasonAllowed)
	assertDecision(t, limiter.AllowN("client", 3), false, 2, 200*time.Millisecond, time.Unix(0, 0).UTC().Add(600*time.Millisecond), ReasonRateLimited)

	for _, key := range []string{"", "has space", "has\nnewline", string([]byte{0xff})} {
		decision := limiter.Allow(key)
		if decision.Allowed || decision.Reason != ReasonInvalidKey {
			t.Errorf("Allow(%q) = %#v, want invalid-key denial", key, decision)
		}
	}
	longKey := strings.Repeat("k", MaxKeyBytes+1)
	if decision := limiter.Allow(longKey); decision.Reason != ReasonInvalidKey {
		t.Errorf("long-key decision = %#v, want invalid-key denial", decision)
	}
	for _, cost := range []int{0, -1, 6} {
		if decision := limiter.AllowN("other", cost); decision.Reason != ReasonInvalidCost {
			t.Errorf("AllowN(cost=%d) = %#v, want invalid-cost denial", cost, decision)
		}
	}
}

func TestIdleCleanupAndRejectOverflow(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	clock := newTestClock(start)
	limiter, err := New(Config{
		Capacity:   1,
		RefillRate: 1,
		MaxClients: 2,
		IdleTTL:    time.Second,
		Overflow:   RejectNew,
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !limiter.Allow("a").Allowed || !limiter.Allow("b").Allowed {
		t.Fatal("initial clients were not admitted")
	}
	if got := limiter.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	if decision := limiter.Allow("c"); decision.Reason != ReasonCapacity || decision.Allowed {
		t.Fatalf("overflow decision = %#v, want capacity denial", decision)
	}
	clock.Advance(time.Second - time.Nanosecond)
	if removed := limiter.Cleanup(); removed != 0 {
		t.Fatalf("early Cleanup() removed %d buckets, want 0", removed)
	}
	clock.Advance(time.Nanosecond)
	if removed := limiter.Cleanup(); removed != 2 {
		t.Fatalf("expired Cleanup() removed %d buckets, want 2", removed)
	}
	if !limiter.Allow("c").Allowed {
		t.Fatal("new client was not admitted after cleanup")
	}
}

func TestEvictOldestOverflowIsDeterministic(t *testing.T) {
	start := time.Unix(200, 0).UTC()
	clock := newTestClock(start)
	limiter, err := New(Config{
		Capacity:   1,
		RefillRate: 1,
		MaxClients: 2,
		Overflow:   EvictOldest,
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !limiter.Allow("b").Allowed || !limiter.Allow("a").Allowed {
		t.Fatal("initial clients were not admitted")
	}
	clock.Advance(time.Nanosecond)
	if !limiter.Allow("c").Allowed {
		t.Fatal("overflow client was not admitted with eviction policy")
	}
	if got := limiter.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	// b and a were tied on lastSeen; lexical order evicts a first.
	if decision := limiter.Allow("a"); !decision.Allowed {
		t.Fatalf("evicted key a was not admitted as a fresh bucket: %#v", decision)
	}
}

func TestClockMovingBackwardDoesNotMintTokens(t *testing.T) {
	clock := newTestClock(time.Unix(100, 0).UTC())
	limiter, err := New(Config{Capacity: 1, RefillRate: 1, MaxClients: 1, Clock: clock})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !limiter.Allow("client").Allowed {
		t.Fatal("initial request was not allowed")
	}
	clock.Advance(-500 * time.Millisecond)
	decision := limiter.Allow("client")
	if decision.Allowed || decision.RetryAfter != time.Second {
		t.Fatalf("backward-clock decision = %#v, want one-second denial", decision)
	}
}

func TestLimiterIsSafeForConcurrentUse(t *testing.T) {
	clock := newTestClock(time.Unix(300, 0).UTC())
	limiter, err := New(Config{Capacity: 100, RefillRate: 1, MaxClients: 200, Clock: clock})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const callers = 1000
	var allowed atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer waitGroup.Done()
			if limiter.Allow("shared").Allowed {
				allowed.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	if got := allowed.Load(); got != 100 {
		t.Fatalf("allowed concurrent requests = %d, want capacity 100", got)
	}
	if got := limiter.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}
}

func TestConcurrentUniqueClientsRespectMaxClients(t *testing.T) {
	clock := newTestClock(time.Unix(400, 0).UTC())
	limiter, err := New(Config{Capacity: 1, RefillRate: 1, MaxClients: 8, Clock: clock})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const callers = 100
	var allowed atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for index := 0; index < callers; index++ {
		go func(index int) {
			defer waitGroup.Done()
			if limiter.Allow("client-" + strconv.Itoa(index)).Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	waitGroup.Wait()
	if got := allowed.Load(); got != 8 {
		t.Fatalf("allowed unique clients = %d, want 8", got)
	}
	if got := limiter.Len(); got != 8 {
		t.Fatalf("Len() = %d, want 8", got)
	}
}

func TestNilLimiterIsSafe(t *testing.T) {
	var limiter *Limiter
	if decision := limiter.Allow("client"); decision.Reason != ReasonUnavailable {
		t.Fatalf("nil Allow() = %#v, want unavailable", decision)
	}
	if decision := limiter.AllowN("client", 1); decision.Reason != ReasonUnavailable {
		t.Fatalf("nil AllowN() = %#v, want unavailable", decision)
	}
	if got := limiter.Cleanup(); got != 0 {
		t.Fatalf("nil Cleanup() = %d, want 0", got)
	}
	if got := limiter.Len(); got != 0 {
		t.Fatalf("nil Len() = %d, want 0", got)
	}
}

func assertDecision(t *testing.T, got Decision, allowed bool, remaining int, retryAfter time.Duration, reset time.Time, reason DenyReason) {
	t.Helper()
	if got.Allowed != allowed || got.Remaining != remaining || got.RetryAfter != retryAfter || !got.Reset.Equal(reset) || got.Reason != reason {
		t.Fatalf("decision = %#v, want allowed=%t remaining=%d retry_after=%s reset=%s reason=%q", got, allowed, remaining, retryAfter, reset, reason)
	}
}

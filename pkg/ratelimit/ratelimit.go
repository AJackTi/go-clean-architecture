// Package ratelimit provides a bounded, in-process token-bucket limiter.
//
// A Limiter keeps one bucket per caller key.  The map is capped so an
// unbounded stream of attacker-controlled keys cannot grow process memory
// forever.  Callers can either reject new keys at the cap or explicitly opt in
// to evicting the least-recently-used idle/active key.
package ratelimit

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxKeyBytes is the largest caller key accepted by Allow.  Keeping keys
	// bounded protects both memory use and downstream metric/log labels.
	MaxKeyBytes = 256

	// MaxCapacity, MaxRefillRate, MaxClients, and MaxIdleTTL keep standalone
	// users of this package from bypassing the conservative configuration
	// bounds enforced by the repository's environment loader.
	MaxCapacity   = 100000
	MaxRefillRate = 100000.0
	MaxClients    = 1000000
	MaxIdleTTL    = 24 * time.Hour

	// maxRetryDuration is a defensive cap for a finite but extremely small
	// refill rate whose computed duration exceeds int64 nanoseconds.
	maxRetryDuration = time.Duration(math.MaxInt64)
)

// Clock supplies time to a Limiter.  Injecting it makes refill and expiry
// behavior deterministic in tests and lets applications share a monotonic
// time source when needed.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to Clock.
type ClockFunc func() time.Time

// Now implements Clock.
func (f ClockFunc) Now() time.Time {
	if f == nil {
		return time.Time{}
	}
	return f()
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// OverflowPolicy controls what happens when MaxClients is reached and a new
// key arrives.  Expired entries are always removed first, regardless of this
// policy.
type OverflowPolicy uint8

const (
	// RejectNew leaves existing buckets untouched and rejects the new key.
	RejectNew OverflowPolicy = iota
	// EvictOldest removes the least-recently-used bucket to admit the new key.
	// This is useful for bounded best-effort protection, but can reset an active
	// caller's bucket; choose it deliberately.
	EvictOldest
)

// Compatibility aliases make the policy intent explicit at call sites.
const (
	OverflowReject      = RejectNew
	OverflowEvictOldest = EvictOldest
)

// DenyReason explains a rejected decision.  ReasonAllowed is the empty value
// so a zero-value reason does not add noise to callers that only inspect
// Decision.Allowed.
type DenyReason string

const (
	ReasonAllowed     DenyReason = ""
	ReasonRateLimited DenyReason = "rate_limited"
	ReasonCapacity    DenyReason = "capacity"
	ReasonInvalidKey  DenyReason = "invalid_key"
	ReasonInvalidCost DenyReason = "invalid_cost"
	ReasonUnavailable DenyReason = "unavailable"
)

// Config defines a token bucket.
//
// RefillRate is measured in tokens per second. Capacity is both the maximum
// burst and the maximum number of tokens stored for each key. IdleTTL zero
// disables expiry; a positive TTL removes buckets that have not been touched
// for at least that duration. Clock nil uses the system clock.
type Config struct {
	Capacity   int
	RefillRate float64
	MaxClients int
	IdleTTL    time.Duration
	Overflow   OverflowPolicy
	Clock      Clock
}

// Validate checks a configuration without allocating a limiter.
func (c Config) Validate() error {
	if c.Capacity <= 0 {
		return fmt.Errorf("ratelimit: capacity must be greater than zero (got %d)", c.Capacity)
	}
	if c.Capacity > MaxCapacity {
		return fmt.Errorf("ratelimit: capacity must be at most %d (got %d)", MaxCapacity, c.Capacity)
	}
	if math.IsNaN(c.RefillRate) || math.IsInf(c.RefillRate, 0) || c.RefillRate <= 0 {
		return fmt.Errorf("ratelimit: refill rate must be finite and greater than zero (got %v)", c.RefillRate)
	}
	if c.RefillRate > MaxRefillRate {
		return fmt.Errorf("ratelimit: refill rate must be at most %g (got %v)", MaxRefillRate, c.RefillRate)
	}
	if c.MaxClients <= 0 {
		return fmt.Errorf("ratelimit: max clients must be greater than zero (got %d)", c.MaxClients)
	}
	if c.MaxClients > MaxClients {
		return fmt.Errorf("ratelimit: max clients must be at most %d (got %d)", MaxClients, c.MaxClients)
	}
	if c.IdleTTL < 0 {
		return fmt.Errorf("ratelimit: idle TTL must not be negative (got %s)", c.IdleTTL)
	}
	if c.IdleTTL > MaxIdleTTL {
		return fmt.Errorf("ratelimit: idle TTL must be at most %s (got %s)", MaxIdleTTL, c.IdleTTL)
	}
	if c.Overflow != RejectNew && c.Overflow != EvictOldest {
		return fmt.Errorf("ratelimit: unknown overflow policy %d", c.Overflow)
	}
	return nil
}

// Decision is the result of Allow or AllowN.
//
// Remaining is the whole-token count available immediately after the
// decision. RetryAfter is the minimum duration before a request of the same
// cost can be admitted; it is zero for an allowed request and for structural
// rejections (invalid key or client-capacity overflow). Reset is the absolute
// time at which the bucket would be full if no further requests arrive. It is
// zero when no bucket exists for the key.
type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
	Reset      time.Time
	Reason     DenyReason
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

// Limiter is safe for concurrent use.
type Limiter struct {
	mu         sync.Mutex
	capacity   int
	refillRate float64
	maxClients int
	idleTTL    time.Duration
	overflow   OverflowPolicy
	clock      Clock
	buckets    map[string]*bucket
}

// New constructs a bounded token-bucket limiter.
func New(cfg Config) (*Limiter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}
	return &Limiter{
		capacity:   cfg.Capacity,
		refillRate: cfg.RefillRate,
		maxClients: cfg.MaxClients,
		idleTTL:    cfg.IdleTTL,
		overflow:   cfg.Overflow,
		clock:      clock,
		// Do not preallocate MaxClients entries: configuration is trusted but
		// may intentionally use a large bound, and an empty limiter should not
		// reserve that memory before its first request.
		buckets: make(map[string]*bucket),
	}, nil
}

// NewLimiter is an explicit alias for callers that prefer constructor names
// which include the type.
func NewLimiter(cfg Config) (*Limiter, error) { return New(cfg) }

// Allow consumes one token for key.
func (l *Limiter) Allow(key string) Decision { return l.AllowN(key, 1) }

// AllowN consumes cost tokens for key. Cost must be between one and the
// configured capacity, inclusive.
func (l *Limiter) AllowN(key string, cost int) Decision {
	if l == nil {
		return Decision{Reason: ReasonUnavailable}
	}
	if !validKey(key) {
		return Decision{Reason: ReasonInvalidKey}
	}
	if cost < 1 || cost > l.capacity {
		return Decision{Reason: ReasonInvalidCost}
	}

	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.buckets[key]
	if exists && l.isExpired(now, entry) {
		delete(l.buckets, key)
		entry = nil
		exists = false
	}
	if !exists {
		if len(l.buckets) >= l.maxClients {
			// Only scan the bounded map when admission pressure requires a
			// slot; hot keys stay O(1) even with a large client bound.
			l.cleanupLocked(now)
			if len(l.buckets) >= l.maxClients {
				if l.overflow == RejectNew {
					return Decision{Reason: ReasonCapacity}
				}
				l.evictOldestLocked()
			}
		}
		entry = &bucket{
			tokens:     float64(l.capacity),
			lastRefill: now,
			lastSeen:   now,
		}
		l.buckets[key] = entry
	}

	effectiveNow := now
	if effectiveNow.Before(entry.lastRefill) {
		effectiveNow = entry.lastRefill
	}
	if effectiveNow.After(entry.lastRefill) {
		elapsed := effectiveNow.Sub(entry.lastRefill).Seconds()
		entry.tokens += elapsed * l.refillRate
		if entry.tokens > float64(l.capacity) {
			entry.tokens = float64(l.capacity)
		}
		entry.lastRefill = effectiveNow
	}
	if effectiveNow.After(entry.lastSeen) {
		entry.lastSeen = effectiveNow
	}

	if entry.tokens >= float64(cost) {
		entry.tokens -= float64(cost)
		if entry.tokens < 0 { // defensive guard for floating-point round-off.
			entry.tokens = 0
		}
		return Decision{
			Allowed:   true,
			Remaining: wholeTokens(entry.tokens, l.capacity),
			Reset:     resetAt(effectiveNow, entry.tokens, l.capacity, l.refillRate),
			Reason:    ReasonAllowed,
		}
	}

	return Decision{
		Remaining:  wholeTokens(entry.tokens, l.capacity),
		RetryAfter: durationForTokens(float64(cost)-entry.tokens, l.refillRate),
		Reset:      resetAt(effectiveNow, entry.tokens, l.capacity, l.refillRate),
		Reason:     ReasonRateLimited,
	}
}

// Cleanup removes buckets idle for at least IdleTTL and returns the number
// removed. It is safe to call periodically from a maintenance goroutine; Allow
// also performs opportunistic cleanup before admitting a new key.
func (l *Limiter) Cleanup() int {
	if l == nil || l.idleTTL <= 0 {
		return 0
	}
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cleanupLocked(now)
}

// Len reports the number of resident client buckets.
func (l *Limiter) Len() int {
	if l == nil {
		return 0
	}
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)
	return len(l.buckets)
}

func (l *Limiter) isExpired(now time.Time, entry *bucket) bool {
	return l.idleTTL > 0 && !now.Before(entry.lastSeen.Add(l.idleTTL))
}

func (l *Limiter) cleanupLocked(now time.Time) int {
	if l.idleTTL <= 0 {
		return 0
	}
	removed := 0
	for key, entry := range l.buckets {
		if l.isExpired(now, entry) {
			delete(l.buckets, key)
			removed++
		}
	}
	return removed
}

func (l *Limiter) evictOldestLocked() {
	var oldestKey string
	var oldest *bucket
	for key, entry := range l.buckets {
		if oldest == nil || entry.lastSeen.Before(oldest.lastSeen) ||
			(entry.lastSeen.Equal(oldest.lastSeen) && key < oldestKey) {
			oldestKey = key
			oldest = entry
		}
	}
	if oldest != nil {
		delete(l.buckets, oldestKey)
	}
}

func validKey(key string) bool {
	if key == "" || len(key) > MaxKeyBytes || !utf8.ValidString(key) {
		return false
	}
	if strings.IndexFunc(key, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return false
	}
	return true
}

func wholeTokens(tokens float64, capacity int) int {
	if tokens <= 0 {
		return 0
	}
	value := int(math.Floor(tokens + 1e-9))
	if value < 0 {
		return 0
	}
	if value > capacity {
		return capacity
	}
	return value
}

func durationForTokens(tokens, refillRate float64) time.Duration {
	if tokens <= 0 {
		return 0
	}
	nanos := tokens / refillRate * float64(time.Second)
	if math.IsInf(nanos, 0) || nanos >= float64(maxRetryDuration) {
		return maxRetryDuration
	}
	return time.Duration(math.Ceil(nanos))
}

func resetAt(now time.Time, tokens float64, capacity int, refillRate float64) time.Time {
	return now.Add(durationForTokens(float64(capacity)-tokens, refillRate))
}

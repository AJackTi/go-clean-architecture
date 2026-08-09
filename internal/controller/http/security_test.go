package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/AJackTi/go-clean-architecture/internal/item/httpapi"
	"github.com/AJackTi/go-clean-architecture/pkg/auth"
	"github.com/AJackTi/go-clean-architecture/pkg/ratelimit"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const securityTestToken = "security-test-token-2bfV8Q9wDqgFJ6mS1R4uZ0Ny"

func TestAuthenticationIsOptInAndLeavesHealthProbesPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &securityServiceFake{}
	verifier, err := auth.NewBearerSHA256WithSubject(digestToken(securityTestToken), "service-a")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(service, nil, WithAuthenticator(verifier))

	unauthorized := securityRequest(t, router, http.MethodGet, "/api/v1/items", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.Code)
	}
	if got := unauthorized.Header().Get("WWW-Authenticate"); got != challengeHeaderValue {
		t.Errorf("WWW-Authenticate = %q, want %q", got, challengeHeaderValue)
	}
	if got := unauthorized.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := unauthorized.Body.String(); got != `{"error":{"code":"unauthorized","message":"valid bearer credentials are required"}}` {
		t.Errorf("unauthorized body = %q", got)
	}
	assertUUIDRequestID(t, unauthorized.Header().Get(requestIDHeader))
	if calls := service.callCount(); calls != 0 {
		t.Errorf("service calls after unauthorized request = %d, want 0", calls)
	}

	health := securityRequest(t, router, http.MethodGet, livenessPath, "")
	if health.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200", health.Code)
	}
	unknown := securityRequest(t, router, http.MethodGet, "/api/v1/unknown", "")
	if unknown.Code != http.StatusUnauthorized {
		t.Fatalf("unknown API path status = %d, want 401 before route dispatch", unknown.Code)
	}
}

func TestAuthenticationInjectsPrincipalIntoRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &securityServiceFake{}
	verifier, err := auth.NewBearerSHA256WithSubject(digestToken(securityTestToken), "service-a")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(service, nil, WithAuthenticator(verifier))

	response := securityRequest(t, router, http.MethodGet, "/api/v1/items", securityTestToken)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", response.Code)
	}
	if got := service.principal.Subject; got != "service-a" {
		t.Errorf("service principal subject = %q, want service-a", got)
	}
	if !service.principalSeen {
		t.Error("service did not receive an authenticated principal")
	}
	if calls := service.callCount(); calls != 1 {
		t.Errorf("service calls = %d, want 1", calls)
	}
}

func TestRateLimiterReturnsStable429AndKeepsHealthPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &securityServiceFake{}
	clock := &securityTestClock{now: time.Unix(100, 0).UTC()}
	limiter, err := ratelimit.New(ratelimit.Config{
		Capacity:   1,
		RefillRate: 1,
		MaxClients: 4,
		Clock:      clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(service, nil, WithRateLimiter(limiter))

	first := securityRequest(t, router, http.MethodGet, "/api/v1/items", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first rate-limited request = %d, want 200", first.Code)
	}
	second := securityRequest(t, router, http.MethodGet, "/api/v1/items", "")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second rate-limited request = %d, want 429", second.Code)
	}
	if got := second.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want 1", got)
	}
	if got := second.Header().Get("RateLimit-Remaining"); got != "0" {
		t.Errorf("RateLimit-Remaining = %q, want 0", got)
	}
	if got := second.Body.String(); got != `{"error":{"code":"rate_limited","message":"request rate limit exceeded"}}` {
		t.Errorf("429 body = %q", got)
	}
	assertUUIDRequestID(t, second.Header().Get(requestIDHeader))
	if calls := service.callCount(); calls != 1 {
		t.Errorf("service calls after rate limit = %d, want 1", calls)
	}

	health := securityRequest(t, router, http.MethodGet, livenessPath, "")
	if health.Code != http.StatusOK {
		t.Fatalf("health while limited = %d, want 200", health.Code)
	}
}

func TestInvalidCredentialsShareAnonymousRateBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &securityServiceFake{}
	verifier, err := auth.NewBearerSHA256(digestToken(securityTestToken))
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.New(ratelimit.Config{Capacity: 1, RefillRate: 1, MaxClients: 4})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(service, nil, WithAuthenticator(verifier), WithRateLimiter(limiter))

	first := securityRequest(t, router, http.MethodGet, "/api/v1/items", "wrong-token")
	second := securityRequest(t, router, http.MethodGet, "/api/v1/items", "another-wrong-token")
	if first.Code != http.StatusUnauthorized || second.Code != http.StatusTooManyRequests {
		t.Fatalf("invalid credential statuses = %d, %d; want 401 then 429", first.Code, second.Code)
	}
	if strings.Contains(first.Body.String(), "wrong-token") || strings.Contains(second.Body.String(), "another-wrong-token") {
		t.Fatal("authentication response leaked a supplied credential")
	}
}

func TestIdempotencyScopeUsesDirectPeerAndNotForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &scopeServiceFake{base: &securityServiceFake{}}
	router := NewRouter(service, nil)

	first := scopedCreateRequest(t, router, "192.0.2.10:1000", "198.51.100.10", "same-key")
	second := scopedCreateRequest(t, router, "192.0.2.11:1000", "198.51.100.10", "same-key")
	third := scopedCreateRequest(t, router, "192.0.2.10:1000", "203.0.113.10", "same-key")
	for name, response := range map[string]*httptest.ResponseRecorder{"first": first, "second": second, "third": third} {
		if response.Code != http.StatusCreated {
			t.Errorf("%s status = %d, want 201; body=%s", name, response.Code, response.Body.String())
		}
	}
	service.mu.Lock()
	scopes := append([]string(nil), service.scopes...)
	service.mu.Unlock()
	if len(scopes) != 3 {
		t.Fatalf("captured scopes = %d, want 3", len(scopes))
	}
	if scopes[0] == scopes[1] {
		t.Errorf("different direct peers share idempotency scope %q", scopes[0])
	}
	if scopes[0] != scopes[2] {
		t.Errorf("X-Forwarded-For changed direct-peer scope: %q vs %q", scopes[0], scopes[2])
	}
}

func securityRequest(t *testing.T, router http.Handler, method, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func scopedCreateRequest(t *testing.T, router http.Handler, remoteAddr, forwardedFor, key string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items", strings.NewReader(`{"name":"keyboard"}`))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(httpapi.IdempotencyKeyHeader, key)
	request.Header.Set("X-Forwarded-For", forwardedFor)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func digestToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type securityServiceFake struct {
	mu            sync.Mutex
	calls         int
	principal     auth.Principal
	principalSeen bool
}

type scopeServiceFake struct {
	base   *securityServiceFake
	mu     sync.Mutex
	scopes []string
}

func (s *scopeServiceFake) Create(ctx context.Context, input item.CreateInput) (item.Item, error) {
	return s.base.Create(ctx, input)
}

func (s *scopeServiceFake) CreateIdempotent(ctx context.Context, _ item.CreateInput, _ string) (item.Item, bool, error) {
	s.mu.Lock()
	s.scopes = append(s.scopes, item.IdempotencyScopeFromContext(ctx))
	s.mu.Unlock()
	return item.Item{ID: uuid.New(), Name: "keyboard"}, false, nil
}

func (s *scopeServiceFake) Get(ctx context.Context, id uuid.UUID) (item.Item, error) {
	return s.base.Get(ctx, id)
}

func (s *scopeServiceFake) List(ctx context.Context, params item.ListParams) (item.Page, error) {
	return s.base.List(ctx, params)
}

func (s *securityServiceFake) Create(context.Context, item.CreateInput) (item.Item, error) {
	s.recordPrincipal(context.Background())
	return item.Item{ID: uuid.New(), Name: "item"}, nil
}

func (s *securityServiceFake) Get(context.Context, uuid.UUID) (item.Item, error) {
	s.recordPrincipal(context.Background())
	return item.Item{ID: uuid.New(), Name: "item"}, nil
}

func (s *securityServiceFake) List(ctx context.Context, _ item.ListParams) (item.Page, error) {
	s.mu.Lock()
	s.calls++
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		s.principal = principal
		s.principalSeen = true
	}
	s.mu.Unlock()
	return item.Page{Items: []item.Item{}, Limit: item.DefaultPageSize}, nil
}

func (s *securityServiceFake) recordPrincipal(ctx context.Context) {
	s.mu.Lock()
	s.calls++
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		s.principal = principal
		s.principalSeen = true
	}
	s.mu.Unlock()
}

func (s *securityServiceFake) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type securityTestClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *securityTestClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

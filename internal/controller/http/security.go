package http

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AJackTi/go-clean-architecture/pkg/auth"
	"github.com/AJackTi/go-clean-architecture/pkg/ratelimit"
	"github.com/gin-gonic/gin"
)

const (
	challengeHeaderValue = `Bearer realm="api"`
	anonymousRateKey     = "anonymous"
	principalKeyPrefix   = "principal:"
	addressKeyPrefix     = "address:"
)

type securityErrorResponse struct {
	Error securityError `json:"error"`
}

type securityError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// securityMiddleware is mounted only on the versioned API group. It verifies
// credentials before choosing a bounded limiter key, while failed credentials
// intentionally share one anonymous bucket so attacker-controlled tokens
// cannot allocate limiter state.
func securityMiddleware(authenticator auth.Authenticator, limiter *ratelimit.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := auth.Principal{}
		authenticated := false

		if authenticator != nil {
			if c.Request == nil {
				if !allowSecurityRequest(c, limiter, anonymousRateKey) {
					return
				}
				writeUnauthorized(c)
				return
			}

			verified, err := authenticator.Authenticate(c.Request.Context(), c.Request)
			if err != nil {
				if !allowSecurityRequest(c, limiter, anonymousRateKey) {
					return
				}
				writeUnauthorized(c)
				return
			}
			requestContext, err := auth.ContextWithPrincipal(c.Request.Context(), verified)
			if err != nil {
				// An authenticator returning an invalid principal is a server-side
				// contract violation. Keep the public response sanitized.
				writeSecurityError(c, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}
			c.Request = c.Request.WithContext(requestContext)
			principal = verified
			authenticated = true
		}

		key := clientRateKey(c)
		if authenticated {
			key = principalRateKey(principal.Subject)
		}
		if !allowSecurityRequest(c, limiter, key) {
			return
		}
		c.Next()
	}
}

// apiSecurityMiddleware applies the policy before route dispatch so even an
// unknown `/api/v1/...` path cannot bypass authentication or consume an
// unbounded stream of unauthenticated requests outside the limiter.
func apiSecurityMiddleware(authenticator auth.Authenticator, limiter *ratelimit.Limiter) gin.HandlerFunc {
	protect := securityMiddleware(authenticator, limiter)
	return func(c *gin.Context) {
		if c != nil && c.Request != nil && isVersionedAPIPath(c.Request.URL.Path) {
			protect(c)
			return
		}
		if c == nil {
			return
		}
		c.Next()
	}
}

func isVersionedAPIPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func principalRateKey(subject string) string {
	digest := sha256.Sum256([]byte(subject))
	return principalKeyPrefix + hex.EncodeToString(digest[:])
}

func allowSecurityRequest(c *gin.Context, limiter *ratelimit.Limiter, key string) bool {
	if limiter == nil {
		return true
	}
	decision := limiter.Allow(key)
	if decision.Allowed {
		c.Header("RateLimit-Remaining", strconv.Itoa(decision.Remaining))
		if !decision.Reset.IsZero() {
			c.Header("RateLimit-Reset", strconv.FormatInt(decision.Reset.Unix(), 10))
		}
		return true
	}

	// Invalid/capacity decisions should not normally occur because this
	// middleware supplies bounded keys. Fail closed if they do occur.
	retrySeconds := retryAfterSeconds(decision.RetryAfter)
	c.Header("Retry-After", strconv.FormatInt(retrySeconds, 10))
	c.Header("RateLimit-Remaining", "0")
	if !decision.Reset.IsZero() {
		c.Header("RateLimit-Reset", strconv.FormatInt(decision.Reset.Unix(), 10))
	}
	writeSecurityError(c, http.StatusTooManyRequests, "rate_limited", "request rate limit exceeded")
	return false
}

func retryAfterSeconds(value time.Duration) int64 {
	if value <= 0 {
		return 1
	}
	seconds := int64(value / time.Second)
	if value%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func writeUnauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", challengeHeaderValue)
	writeSecurityError(c, http.StatusUnauthorized, "unauthorized", "valid bearer credentials are required")
}

func writeSecurityError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, securityErrorResponse{
		Error: securityError{Code: code, Message: message},
	})
}

func clientRateKey(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return anonymousRateKey
	}
	address := strings.TrimSpace(c.Request.RemoteAddr)
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	if parsed := net.ParseIP(address); parsed != nil {
		return addressKeyPrefix + parsed.String()
	}
	return anonymousRateKey
}

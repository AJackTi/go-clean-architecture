// Package auth provides small, transport-neutral authentication primitives.
//
// The package intentionally does not decide how an application authorizes a
// principal. It only parses an HTTP Bearer credential, verifies an
// operator-provided SHA-256 digest, and provides a bounded principal value for
// downstream middleware and use cases. The digest verifier is useful as a
// minimal service-to-service option; applications with end-user identity
// should provide an Authenticator backed by their OIDC/JWT provider instead.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxBearerTokenBytes bounds work and memory spent processing a bearer
	// credential. A random 256-bit token encoded as hexadecimal is only 64
	// bytes, while this limit still accommodates common JWTs and opaque tokens.
	MaxBearerTokenBytes = 4096

	// MaxPrincipalSubjectBytes keeps identity values safe to copy into request
	// contexts, logs, and future bounded telemetry labels.
	MaxPrincipalSubjectBytes = 256

	// MaxPrincipalScopes is the maximum number of scopes accepted by
	// ContextWithPrincipal. Scope support is deliberately generic so a future
	// authenticator can carry provider claims without changing this seam.
	MaxPrincipalScopes = 128

	// MaxPrincipalScopeBytes bounds each individual scope value.
	MaxPrincipalScopeBytes = 256

	// MaxPrincipalScopeTotalBytes bounds the aggregate scope memory retained in
	// a request context.
	MaxPrincipalScopeTotalBytes = 4096

	// DefaultBearerSubject is used by NewBearerSHA256 when the configured
	// digest represents one service credential rather than a user identity.
	DefaultBearerSubject = "configured-bearer"
)

var (
	// ErrMissingCredentials means that no Authorization value was supplied.
	// It wraps ErrInvalidCredentials so middleware can map all malformed bearer
	// values to one public 401 response while still testing the specific cause.
	ErrMissingCredentials = fmt.Errorf("%w: missing credentials", ErrInvalidCredentials)

	// ErrDuplicateCredentials means that more than one Authorization header
	// value was supplied. Combining duplicate credentials is unsafe and is
	// rejected instead of selecting an arbitrary value.
	ErrDuplicateCredentials = fmt.Errorf("%w: duplicate credentials", ErrInvalidCredentials)

	// ErrUnsupportedScheme means that the Authorization scheme is not Bearer.
	ErrUnsupportedScheme = fmt.Errorf("%w: unsupported scheme", ErrInvalidCredentials)

	// ErrCredentialsTooLong means that the header or token exceeds the bounded
	// parser limit.
	ErrCredentialsTooLong = fmt.Errorf("%w: credentials too long", ErrInvalidCredentials)

	// ErrInvalidCredentials is the common authentication failure sentinel. Its
	// error text never contains the supplied token or any other request data.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrNilContext means that a caller passed a nil context to an operation
	// whose cancellation and deadline semantics are part of its contract.
	ErrNilContext = errors.New("auth: context must not be nil")

	// ErrNilRequest means that an authenticator was asked to inspect no HTTP
	// request.
	ErrNilRequest = errors.New("auth: request must not be nil")

	// ErrInvalidDigest means that a configured SHA-256 digest is not exactly
	// 32 bytes represented by 64 hexadecimal characters.
	ErrInvalidDigest = errors.New("auth: invalid SHA-256 digest")

	// ErrInvalidPrincipal means that a principal would violate the package's
	// size, encoding, or token-character bounds.
	ErrInvalidPrincipal = errors.New("auth: invalid principal")
)

// Authenticator verifies the credentials on request and returns the caller's
// principal. Implementations must not retain request pointers or credential
// strings. A caller should pass request.Context() so cancellation propagates
// through network-backed authenticators as well as the built-in verifier.
type Authenticator interface {
	Authenticate(context.Context, *http.Request) (Principal, error)
}

// Principal is the authenticated caller identity made available to
// authorization policy. Subject and Scopes are copied when entering or
// leaving a context; callers cannot mutate the stored value through the slice
// they supplied.
type Principal struct {
	Subject string
	Scopes  []string
}

// Validate checks the bounded, provider-neutral principal representation.
// Subject and scope values are not normalized: changing an identity by
// trimming or case-folding here could join two distinct provider identities.
func (p Principal) Validate() error {
	if err := validatePrincipalText(p.Subject, MaxPrincipalSubjectBytes, true); err != nil {
		return err
	}
	if len(p.Scopes) > MaxPrincipalScopes {
		return ErrInvalidPrincipal
	}
	totalBytes := 0
	for _, scope := range p.Scopes {
		if err := validatePrincipalText(scope, MaxPrincipalScopeBytes, false); err != nil {
			return err
		}
		totalBytes += len(scope)
		if totalBytes > MaxPrincipalScopeTotalBytes {
			return ErrInvalidPrincipal
		}
	}
	return nil
}

// HasScope reports whether p contains the exact scope string. Empty scopes
// never match, which keeps accidental blank configuration from granting
// policy.
func (p Principal) HasScope(scope string) bool {
	if scope == "" {
		return false
	}
	for _, candidate := range p.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

// ContextWithPrincipal stores a validated copy of principal in ctx.
func ContextWithPrincipal(ctx context.Context, principal Principal) (context.Context, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, principalContextKey{}, clonePrincipal(principal)), nil
}

// WithPrincipal stores principal in ctx when both values are valid. It is the
// ergonomic helper for middleware that only handles principals returned by an
// Authenticator. Code accepting arbitrary principals should use
// ContextWithPrincipal so it can handle validation errors explicitly. A nil
// context or invalid principal is returned unchanged and never causes a panic.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	updated, err := ContextWithPrincipal(ctx, principal)
	if err != nil {
		return ctx
	}
	return updated
}

// PrincipalFromContext retrieves a defensive copy of the stored principal.
// It returns false for a nil context or when no principal has been attached.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok {
		return Principal{}, false
	}
	return clonePrincipal(principal), true
}

// BearerSHA256 verifies one opaque Bearer credential against a configured
// SHA-256 digest. The verifier stores only the digest and a bounded principal;
// it never stores the raw bearer token. It is safe for concurrent use after
// construction.
type BearerSHA256 struct {
	digest    [sha256.Size]byte
	principal Principal
}

var _ Authenticator = (*BearerSHA256)(nil)

// NewBearerSHA256 constructs a verifier with DefaultBearerSubject.
// digestHex must contain exactly 64 hexadecimal characters. Leading/trailing
// whitespace is rejected rather than silently normalized.
func NewBearerSHA256(digestHex string) (*BearerSHA256, error) {
	return NewBearerSHA256WithSubject(digestHex, DefaultBearerSubject)
}

// NewBearerSHA256WithSubject constructs a verifier with an explicit principal
// subject. The subject is validated and copied before the verifier is
// returned. The configured digest is a verifier equivalent, not the bearer
// secret itself; generate it from a high-entropy secret before deployment.
func NewBearerSHA256WithSubject(digestHex, subject string) (*BearerSHA256, error) {
	digest, err := parseSHA256Digest(digestHex)
	if err != nil {
		return nil, err
	}
	principal := Principal{Subject: subject}
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	return &BearerSHA256{digest: digest, principal: principal}, nil
}

// NewBearerSHA256FromDigest constructs a verifier from an already decoded
// digest. It is useful when configuration parsing has already validated a
// fixed-size value. The subject must still be valid.
func NewBearerSHA256FromDigest(digest [sha256.Size]byte, subject string) (*BearerSHA256, error) {
	principal := Principal{Subject: subject}
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	return &BearerSHA256{digest: digest, principal: principal}, nil
}

// Authenticate parses and verifies the request's Bearer credential. Missing,
// malformed, unsupported, and mismatching credentials intentionally return
// errors that do not include the supplied value. A canceled or deadline-
// exceeded context is returned unchanged so callers can distinguish request
// cancellation from a 401 authentication failure.
func (v *BearerSHA256) Authenticate(ctx context.Context, request *http.Request) (Principal, error) {
	if ctx == nil {
		return Principal{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	if request == nil {
		return Principal{}, ErrNilRequest
	}
	if v == nil {
		return Principal{}, ErrInvalidCredentials
	}

	token, err := ParseBearerToken(request)
	if err != nil {
		return Principal{}, err
	}
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}

	// Hashing always produces a fixed-size value. ConstantTimeCompare avoids
	// an early-exit comparison that could disclose the first differing byte of
	// the configured digest over many observations.
	provided := sha256.Sum256([]byte(token))
	if !constantTimeDigestEqual(provided, v.digest) {
		return Principal{}, ErrInvalidCredentials
	}
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	return clonePrincipal(v.principal), nil
}

// ParseBearerToken extracts a strict Bearer token from request. Exactly one
// Authorization header value is required. The parser accepts the
// case-insensitive Bearer scheme and one ASCII space before an RFC 6750
// b64token value; all other whitespace, duplicate values, delimiters, and
// control/non-ASCII characters are rejected.
func ParseBearerToken(request *http.Request) (string, error) {
	if request == nil {
		return "", ErrNilRequest
	}
	// Header.Values performs canonical lookup, which is correct for headers
	// parsed by net/http. A manually constructed request can nevertheless hold
	// differently cased map keys; collect those too so duplicate credentials
	// cannot be hidden by map spelling at an adapter seam.
	var values []string
	for key, entries := range request.Header {
		if strings.EqualFold(key, "Authorization") {
			values = append(values, entries...)
		}
	}
	return ParseBearerHeader(values)
}

// ParseBearerHeader parses Authorization header values without requiring an
// *http.Request. It is useful for adapters that expose headers directly and
// makes duplicate-header handling explicit in tests.
func ParseBearerHeader(values []string) (string, error) {
	switch len(values) {
	case 0:
		return "", ErrMissingCredentials
	case 1:
		// Continue below.
	default:
		return "", ErrDuplicateCredentials
	}

	raw := values[0]
	if len(raw) > MaxBearerTokenBytes+len("Bearer ") || raw == "" {
		if len(raw) > MaxBearerTokenBytes+len("Bearer ") {
			return "", ErrCredentialsTooLong
		}
		return "", ErrInvalidCredentials
	}
	// Strictly reject leading/trailing or repeated whitespace. HTTP parsers
	// already remove field-line framing whitespace; accepting more here would
	// make proxy and application parsing disagree.
	if raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\t\r\n") {
		return "", ErrInvalidCredentials
	}
	separator := strings.IndexByte(raw, ' ')
	if separator <= 0 || separator == len(raw)-1 || strings.IndexByte(raw[separator+1:], ' ') >= 0 {
		return "", ErrInvalidCredentials
	}
	scheme := raw[:separator]
	if !strings.EqualFold(scheme, "Bearer") {
		return "", ErrUnsupportedScheme
	}
	token := raw[separator+1:]
	if len(token) == 0 {
		return "", ErrInvalidCredentials
	}
	if len(token) > MaxBearerTokenBytes {
		return "", ErrCredentialsTooLong
	}
	if !validBearerToken(token) {
		return "", ErrInvalidCredentials
	}
	return token, nil
}

func parseSHA256Digest(raw string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if len(raw) != sha256.Size*2 || raw != strings.TrimSpace(raw) {
		return digest, ErrInvalidDigest
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size {
		return digest, ErrInvalidDigest
	}
	copy(digest[:], decoded)
	return digest, nil
}

func validBearerToken(token string) bool {
	if token == "" || token[0] == '=' {
		return false
	}
	for index := 0; index < len(token); index++ {
		character := token[index]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			strings.ContainsRune("-._~+/", rune(character)):
			continue
		case character == '=':
			// RFC 6750 permits padding only at the end of b64token.
			for padding := index; padding < len(token); padding++ {
				if token[padding] != '=' {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	return true
}

func constantTimeDigestEqual(left, right [sha256.Size]byte) bool {
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}

func validatePrincipalText(value string, maxBytes int, requireNonEmpty bool) error {
	if (requireNonEmpty && value == "") || len(value) > maxBytes || !utf8.ValidString(value) ||
		value != strings.TrimSpace(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ErrInvalidPrincipal
	}
	if !requireNonEmpty && value == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func clonePrincipal(principal Principal) Principal {
	if principal.Scopes == nil {
		return principal
	}
	principal.Scopes = append([]string(nil), principal.Scopes...)
	return principal
}

type principalContextKey struct{}

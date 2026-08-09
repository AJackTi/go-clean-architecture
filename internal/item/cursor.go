package item

// This file owns the domain-side cursor contract. A cursor is a position in
// the Item ordering, not an offset and not a database-specific handle. The
// Service owns opaque-token decoding/encoding; stores only receive a
// validated position through CursorStore.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// DefaultCursorTTL bounds how long a cursor remains usable when callers do
	// not provide an explicit retention policy.  Cursors are stateless, so a
	// bounded lifetime also limits how long an old ordering contract remains
	// valid after a deployment.
	DefaultCursorTTL = 24 * time.Hour

	// MaxCursorTTL prevents an accidental configuration value from turning a
	// cursor into an effectively permanent bearer token.  A zero TTL is allowed
	// and explicitly means that the codec does not expire tokens.
	MaxCursorTTL = 30 * 24 * time.Hour

	// MinCursorSigningKeyBytes is the minimum secret size accepted by the
	// signed codec.  HMAC itself accepts shorter values, but accepting a weak
	// deployment secret would undermine the integrity guarantee.
	MinCursorSigningKeyBytes = 32

	// MaxCursorBytes bounds both parsing work and the size of a value that a
	// caller can place in a URL.  The fixed v1 wire format is considerably
	// smaller; the larger bound leaves room for a future version while keeping
	// malformed requests cheap to reject.
	MaxCursorBytes = 512

	cursorVersion  byte = 1
	cursorPrefix        = "v1_"
	cursorMACBytes      = sha256.Size
	// version (1) + two time.MarshalBinary values (15 bytes each) + UUID (16).
	// MarshalBinary is used instead of integer casts so dates outside the Unix
	// epoch remain representable and the wire format passes integer-overflow
	// checks in security linters.
	cursorTimeBytes    = 15
	cursorPayloadBytes = 1 + cursorTimeBytes + cursorTimeBytes + 16
	cursorWireBytes    = cursorPayloadBytes + cursorMACBytes
	cursorPurpose      = "item-list-cursor/v1"
)

var (
	// ErrInvalidCursor means that a supplied cursor is malformed, expired, or
	// was not authenticated by the configured codec.  The error deliberately
	// does not include the supplied token.
	ErrInvalidCursor = errors.New("item: invalid cursor")

	// ErrCursorUnavailable means that a caller requested cursor pagination but
	// the configured Store does not implement the cohesive cursor capability or
	// the adapter cannot currently provide it.
	ErrCursorUnavailable = errors.New("item: cursor store unavailable")

	// ErrCursorState means that an adapter returned a value from which a safe
	// continuation position cannot be produced.  It is kept distinct from a
	// caller's malformed cursor so transports can fail closed with 503.
	ErrCursorState = errors.New("item: invalid cursor state")
)

// CursorError identifies a cursor decoding/validation failure without
// retaining or echoing the untrusted token.  It unwraps to ErrInvalidCursor
// so transports can map all malformed, forged, and expired cursors together.
type CursorError struct {
	Reason string
}

func (e *CursorError) Error() string {
	if e == nil || e.Reason == "" {
		return ErrInvalidCursor.Error()
	}
	return fmt.Sprintf("%s: %s", ErrInvalidCursor, e.Reason)
}

func (e *CursorError) Unwrap() error { return ErrInvalidCursor }

// CursorPosition is the last Item position included in a page.  Item pages
// are ordered by CreatedAt descending and ID descending; a subsequent page
// therefore selects rows strictly after this position in that descending
// order (created_at,id) < (position.created_at,position.id).
//
// CreatedAt and ID are immutable Item fields.  Callers should obtain a
// position from CursorPositionForItem or from CursorCodec.Decode rather than
// constructing one from arbitrary transport data.
type CursorPosition struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Validate checks the invariant needed by every cursor store.  Time zones and
// monotonic clock readings are normalized by the codec; validation itself does
// not reject a legitimate pre-epoch instant.
func (position CursorPosition) Validate() error {
	if position.ID == uuid.Nil {
		return invalidCursor("position id is required")
	}
	if position.CreatedAt.IsZero() {
		return invalidCursor("position time is required")
	}
	canonical := position.CreatedAt.UTC().Round(0)
	seconds := canonical.Unix()
	nanos := canonical.Nanosecond()
	// time.Unix normalizes its arguments.  Comparing the round trip catches
	// values outside the representable Unix range before they reach the fixed
	// wire format.
	if !time.Unix(seconds, int64(nanos)).UTC().Equal(canonical) {
		return invalidCursor("position time is out of range")
	}
	return nil
}

// CursorPositionForItem derives a validated continuation position from an
// Item.  It is useful to an HTTP adapter when it wants to expose a cursor for
// a first page produced by the legacy offset path.
func CursorPositionForItem(value Item) (CursorPosition, error) {
	position := CursorPosition{CreatedAt: value.CreatedAt, ID: value.ID}
	if err := position.Validate(); err != nil {
		return CursorPosition{}, errors.Join(ErrCursorState, err)
	}
	return position, nil
}

// CursorListParams controls a keyset list operation.  A nil After requests
// the first page.  Service normalizes Limit and asks the Store for one extra
// row to calculate HasMore; adapters must return deterministic newest-first
// order and honor the supplied context.
type CursorListParams struct {
	Limit int
	After *CursorPosition
}

// CursorRequest is the transport-neutral request for a keyset list
// operation. Cursor is the raw opaque token received from a transport; the
// Service decodes and authenticates it before it reaches a Store. An empty
// Cursor requests the first page.
type CursorRequest struct {
	Limit  int
	Cursor string
}

// CursorPage is the transport-neutral result of a keyset list operation. The
// Service encodes the continuation position, so outer adapters only need to
// copy NextCursor to their response envelope. NextCursor is empty when
// HasMore is false or the page contains no rows.
type CursorPage struct {
	Items      []Item
	Limit      int
	HasMore    bool
	NextCursor string
}

// CursorStore is the optional persistence capability for keyset pagination.
// It must be implemented by the same concrete Store that persists Items; a
// Service must never combine an Item Store with a separate cursor backend.
// Implementations return at most params.Limit rows and apply the strict
// (created_at,id) boundary when After is non-nil.
type CursorStore interface {
	ListAfter(ctx context.Context, params CursorListParams) ([]Item, error)
}

// CursorLister is the optional application capability consumed by transport
// adapters. Keeping it separate from Service's original List method lets
// existing offset callers and small fakes continue compiling while clients
// migrate to cursors. The alias name is retained for readability at HTTP
// seams.
type CursorLister interface {
	ListCursor(ctx context.Context, request CursorRequest) (CursorPage, error)
}

// CursorService is a descriptive alias for callers that prefer to make the
// optional application capability explicit.
type CursorService = CursorLister

// CursorCodec transports a validated position as an opaque token.  The
// built-in implementation is signed; applications may provide another codec
// only when it preserves the same validation and expiry guarantees.
type CursorCodec interface {
	Encode(position CursorPosition) (string, error)
	Decode(token string) (CursorPosition, error)
}

// CursorCodecOption configures SignedCursorCodec.
type CursorCodecOption func(*SignedCursorCodec)

// WithCursorTTL sets token retention.  Zero disables expiry; positive values
// must not exceed MaxCursorTTL.  Invalid values are reported by the
// constructor after all options are applied.
func WithCursorTTL(ttl time.Duration) CursorCodecOption {
	return func(codec *SignedCursorCodec) {
		if codec != nil {
			codec.ttl = ttl
		}
	}
}

// WithCursorClock injects the clock used for token issuance and validation.
// It is intended for deterministic tests.  A nil value restores time.Now.
func WithCursorClock(clock Clock) CursorCodecOption {
	return func(codec *SignedCursorCodec) {
		if codec == nil {
			return
		}
		if clock == nil {
			codec.clock = time.Now
			return
		}
		codec.clock = clock
	}
}

// WithCursorPurpose domain-separates tokens issued for different collection
// contracts.  A token from one resource or sort order cannot be replayed at a
// codec configured for another purpose.  Purpose is not stored in the token;
// it is covered by the MAC.
func WithCursorPurpose(purpose string) CursorCodecOption {
	return func(codec *SignedCursorCodec) {
		if codec != nil {
			codec.purpose = purpose
		}
	}
}

// SignedCursorCodec is a stateless, URL-safe, authenticated cursor codec.
// The secret is copied into a fixed-size HMAC key and is never exposed after
// construction.  Tokens contain only a version, ordering position, and an
// optional expiry; they do not contain names, descriptions, caller identity,
// or raw query data.
type SignedCursorCodec struct {
	key     [sha256.Size]byte
	ttl     time.Duration
	clock   Clock
	purpose string
}

var _ CursorCodec = (*SignedCursorCodec)(nil)

// NewSignedCursorCodec constructs the v1 HMAC-SHA256 codec.  A minimum
// 256-bit secret is required.  The supplied bytes are hashed once to obtain a
// fixed-size key, so callers may safely pass a high-entropy encoded secret.
func NewSignedCursorCodec(secret []byte, options ...CursorCodecOption) (*SignedCursorCodec, error) {
	if len(secret) < MinCursorSigningKeyBytes {
		return nil, fmt.Errorf("%w: signing key must contain at least %d bytes", ErrInvalidCursor, MinCursorSigningKeyBytes)
	}
	codec := &SignedCursorCodec{
		ttl:     DefaultCursorTTL,
		clock:   time.Now,
		purpose: cursorPurpose,
	}
	for _, option := range options {
		if option != nil {
			option(codec)
		}
	}
	if err := validateCursorCodecConfig(codec); err != nil {
		return nil, err
	}
	codec.key = sha256.Sum256(secret)
	// Do not retain references to caller-owned option data through a mutable
	// string. Strings are immutable; copying here also makes the ownership
	// intent explicit.
	codec.purpose = strings.Clone(codec.purpose)
	return codec, nil
}

// NewCursorCodec is the concise constructor used by composition roots.
func NewCursorCodec(secret []byte, options ...CursorCodecOption) (*SignedCursorCodec, error) {
	return NewSignedCursorCodec(secret, options...)
}

func validateCursorCodecConfig(codec *SignedCursorCodec) error {
	if codec == nil {
		return fmt.Errorf("%w: nil codec", ErrInvalidCursor)
	}
	if codec.ttl < 0 || codec.ttl > MaxCursorTTL {
		return fmt.Errorf("%w: cursor TTL must be zero or at most %s", ErrInvalidCursor, MaxCursorTTL)
	}
	if codec.clock == nil {
		return fmt.Errorf("%w: nil cursor clock", ErrInvalidCursor)
	}
	if codec.purpose == "" || len(codec.purpose) > 128 || !utf8.ValidString(codec.purpose) || strings.IndexFunc(codec.purpose, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: invalid cursor purpose", ErrInvalidCursor)
	}
	return nil
}

// Encode signs a position and returns a raw-base64url token.  It never emits
// padding, which keeps the token safe in query strings without additional
// escaping.
func (codec *SignedCursorCodec) Encode(position CursorPosition) (string, error) {
	if codec == nil {
		return "", ErrCursorUnavailable
	}
	if err := position.Validate(); err != nil {
		return "", err
	}
	now := codec.currentTime()
	if now.IsZero() {
		return "", errors.Join(ErrCursorUnavailable, fmt.Errorf("cursor clock returned zero time"))
	}
	payload := make([]byte, cursorPayloadBytes)
	payload[0] = cursorVersion
	if err := putTime(payload[1:1+cursorTimeBytes], position.CreatedAt); err != nil {
		return "", errors.Join(ErrCursorState, err)
	}
	if codec.ttl > 0 {
		expires := now.Add(codec.ttl)
		if !expires.After(now) {
			return "", errors.Join(ErrCursorUnavailable, fmt.Errorf("cursor expiry overflow"))
		}
		if !expires.After(position.CreatedAt) {
			return "", errors.Join(ErrCursorState, fmt.Errorf("cursor expiry precedes position"))
		}
		if err := putTime(payload[1+cursorTimeBytes:1+2*cursorTimeBytes], expires); err != nil {
			return "", errors.Join(ErrCursorUnavailable, err)
		}
	}
	copy(payload[1+2*cursorTimeBytes:], position.ID[:])

	wire := make([]byte, 0, cursorWireBytes)
	wire = append(wire, payload...)
	wire = append(wire, codec.mac(payload)...)
	token := cursorPrefix + base64.RawURLEncoding.EncodeToString(wire)
	if len(token) > MaxCursorBytes {
		return "", errors.Join(ErrCursorUnavailable, fmt.Errorf("encoded cursor exceeds size bound"))
	}
	return token, nil
}

// Decode authenticates and validates a token.  All malformed, forged, and
// expired values unwrap to ErrInvalidCursor; the token itself is never put in
// the returned error.
func (codec *SignedCursorCodec) Decode(token string) (CursorPosition, error) {
	if codec == nil {
		return CursorPosition{}, ErrCursorUnavailable
	}
	if token == "" || len(token) > MaxCursorBytes || !strings.HasPrefix(token, cursorPrefix) {
		return CursorPosition{}, invalidCursor("malformed token")
	}
	encoded := token[len(cursorPrefix):]
	if encoded == "" {
		return CursorPosition{}, invalidCursor("malformed token")
	}
	wire, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(wire) != cursorWireBytes {
		return CursorPosition{}, invalidCursor("malformed token")
	}
	// RawURLEncoding accepts a few alternate textual representations. Requiring
	// the canonical re-encoding keeps one position from having multiple cache
	// keys and makes token validation deterministic across proxies.
	if base64.RawURLEncoding.EncodeToString(wire) != encoded {
		return CursorPosition{}, invalidCursor("non-canonical token")
	}
	payload := wire[:cursorPayloadBytes]
	providedMAC := wire[cursorPayloadBytes:]
	expectedMAC := codec.mac(payload)
	if !hmac.Equal(providedMAC, expectedMAC) {
		return CursorPosition{}, invalidCursor("authentication failed")
	}
	if payload[0] != cursorVersion {
		return CursorPosition{}, invalidCursor("unsupported version")
	}
	createdAt, ok := readTime(payload[1 : 1+cursorTimeBytes])
	if !ok {
		return CursorPosition{}, invalidCursor("invalid position time")
	}
	expiresAt, ok := readOptionalTime(payload[1+cursorTimeBytes : 1+2*cursorTimeBytes])
	if !ok {
		return CursorPosition{}, invalidCursor("invalid expiry time")
	}
	var id uuid.UUID
	copy(id[:], payload[1+2*cursorTimeBytes:])
	position := CursorPosition{CreatedAt: createdAt, ID: id}
	if err := position.Validate(); err != nil {
		return CursorPosition{}, err
	}
	if !expiresAt.IsZero() {
		now := codec.currentTime()
		if now.IsZero() {
			return CursorPosition{}, errors.Join(ErrCursorUnavailable, fmt.Errorf("cursor clock returned zero time"))
		}
		if !now.Before(expiresAt) {
			return CursorPosition{}, invalidCursor("expired token")
		}
	}
	return position, nil
}

func (codec *SignedCursorCodec) currentTime() time.Time {
	clock := codec.clock
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC().Round(0)
}

func (codec *SignedCursorCodec) mac(payload []byte) []byte {
	mac := hmac.New(sha256.New, codec.key[:])
	_, _ = mac.Write([]byte(codec.purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

// putTime writes a canonical UTC time.MarshalBinary value in a fixed-width
// field. The standard library's range checks avoid lossy integer conversion.
func putTime(destination []byte, value time.Time) error {
	if len(destination) != cursorTimeBytes {
		return fmt.Errorf("cursor time field has invalid size")
	}
	encoded, err := value.UTC().Round(0).MarshalBinary()
	if err != nil || len(encoded) != cursorTimeBytes {
		if err == nil {
			err = errors.New("unexpected cursor time encoding size")
		}
		return err
	}
	copy(destination, encoded)
	return nil
}

func readOptionalTime(source []byte) (time.Time, bool) {
	if len(source) != cursorTimeBytes {
		return time.Time{}, false
	}
	if bytes.Equal(source, make([]byte, cursorTimeBytes)) {
		return time.Time{}, true
	}
	return readTime(source)
}

func readTime(source []byte) (time.Time, bool) {
	if len(source) != cursorTimeBytes {
		return time.Time{}, false
	}
	var value time.Time
	if err := value.UnmarshalBinary(source); err != nil {
		return time.Time{}, false
	}
	canonical := value.UTC().Round(0)
	canonicalWire, err := canonical.MarshalBinary()
	if err != nil || len(canonicalWire) != cursorTimeBytes || !bytes.Equal(canonicalWire, source) {
		return time.Time{}, false
	}
	return canonical, true
}

func invalidCursor(reason string) error {
	return &CursorError{Reason: reason}
}

// ListCursor executes one keyset page through the same concrete Store used by
// ordinary Item operations. It deliberately remains separate from List so
// existing offset callers and fakes stay source-compatible during migration.
// The configured codec is the only component that sees the raw token; stores
// receive a validated CursorPosition and never parse transport data.
func (service *Service) ListCursor(ctx context.Context, request CursorRequest) (CursorPage, error) {
	if err := contextError(ctx); err != nil {
		return CursorPage{}, err
	}
	if request.Limit == 0 {
		request.Limit = DefaultPageSize
	}
	if request.Limit < 0 || request.Limit > MaxPageSize {
		return CursorPage{}, &ValidationError{Field: "limit", Reason: fmt.Sprintf("must be between 0 and %d", MaxPageSize)}
	}
	if service == nil || service.store == nil || service.cursorCodec == nil {
		return CursorPage{}, ErrCursorUnavailable
	}
	var after *CursorPosition
	if request.Cursor != "" {
		// Keep the application seam bounded even when a custom codec is
		// injected. The HTTP adapter applies the same limit, but direct callers
		// must not be able to hand an unbounded token to an arbitrary decoder.
		if len(request.Cursor) > MaxCursorBytes {
			return CursorPage{}, invalidCursor("token exceeds size bound")
		}
		position, err := service.cursorCodec.Decode(request.Cursor)
		if err != nil {
			return CursorPage{}, err
		}
		// CursorCodec is an extension seam. Do not rely solely on the built-in
		// codec's validation: a custom implementation that returns a malformed
		// position must fail closed before the value reaches a store predicate.
		if err := position.Validate(); err != nil {
			return CursorPage{}, errors.Join(ErrCursorState, err)
		}
		after = &position
	}
	store, ok := service.store.(CursorStore)
	if !ok || store == nil {
		return CursorPage{}, ErrCursorUnavailable
	}
	rows, err := store.ListAfter(ctx, CursorListParams{
		Limit: request.Limit + 1,
		After: after,
	})
	if err != nil {
		return CursorPage{}, err
	}
	if err := contextError(ctx); err != nil {
		return CursorPage{}, err
	}
	if len(rows) > request.Limit+1 {
		return CursorPage{}, fmt.Errorf("%w: store returned too many rows", ErrCursorState)
	}
	hasMore := len(rows) > request.Limit
	if hasMore {
		rows = rows[:request.Limit]
	}
	items := make([]Item, len(rows))
	copy(items, rows)
	page := CursorPage{Items: items, Limit: request.Limit, HasMore: hasMore}
	if hasMore && len(items) > 0 {
		position, positionErr := CursorPositionForItem(items[len(items)-1])
		if positionErr != nil {
			return CursorPage{}, positionErr
		}
		page.NextCursor, err = service.cursorCodec.Encode(position)
		if err != nil {
			return CursorPage{}, errors.Join(ErrCursorState, err)
		}
	}
	if err := contextError(ctx); err != nil {
		return CursorPage{}, err
	}
	return page, nil
}

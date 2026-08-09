package item

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// MaxIdempotencyKeyBytes bounds the client supplied HTTP token. Keeping the
	// value small makes validation, hashing, and transport headers predictable.
	MaxIdempotencyKeyBytes = 255

	// IdempotencyRecordRetention is the replay window for a completed create.
	// Adapters may remove records after this duration; retries outside the window
	// are treated as a new request.
	IdempotencyRecordRetention = 24 * time.Hour

	// MaxIdempotencyEntries is the hard resident-entry bound for adapters that
	// keep idempotency records in process memory. Durable adapters should enforce
	// an equivalent operational retention/cleanup policy.
	MaxIdempotencyEntries = 10_000

	defaultIdempotencyScope = "items:create"
)

var (
	// ErrIdempotencyConflict means a key was already used with another request
	// fingerprint. The original response is never overwritten.
	ErrIdempotencyConflict = errors.New("item: idempotency key conflict")
	// ErrIdempotencyInProgress means another request currently owns the key.
	ErrIdempotencyInProgress = errors.New("item: idempotency request in progress")
	// ErrIdempotencyUnavailable means the caller requested idempotency but the
	// composition root has no cohesive atomic idempotency implementation, or the
	// implementation cannot accept another bounded record.
	ErrIdempotencyUnavailable = errors.New("item: idempotency store unavailable")
	// ErrIdempotencyState means a durable reservation references invalid state.
	ErrIdempotencyState = errors.New("item: invalid idempotency state")
)

// IdempotentCreateStore is the persistence seam for an idempotent create. The
// operation must make the Item insert and its idempotency record one atomic
// unit. It returns replayed=true only when the stored response is replayed.
// Implementations must hash key/fingerprint before durable storage, bound
// memory/retention, serialize the same key across replicas, and honor ctx.
type IdempotentCreateStore interface {
	CreateIdempotent(ctx context.Context, value Item, key, fingerprint string) (created Item, replayed bool, err error)
}

// IdempotencyStore is retained as a descriptive alias for callers that prefer
// the shorter name. It deliberately represents the atomic seam, not a
// multi-step reservation protocol.
type IdempotencyStore = IdempotentCreateStore

// IdempotentCreator is the optional service capability consumed by the HTTP
// adapter when an Idempotency-Key header is present. replayed is true only for
// an earlier successful response.
type IdempotentCreator interface {
	CreateIdempotent(ctx context.Context, input CreateInput, key string) (value Item, replayed bool, err error)
}

// ValidateIdempotencyKey enforces an HTTP-token-shaped key. Restricting the
// alphabet avoids ambiguity from folded/quoted header values and prevents
// control characters from reaching logs or response headers.
func ValidateIdempotencyKey(key string) error {
	if key == "" || len(key) > MaxIdempotencyKeyBytes {
		return &ValidationError{Field: "idempotency_key", Reason: fmt.Sprintf("must contain 1-%d ASCII token bytes", MaxIdempotencyKeyBytes)}
	}
	for index := 0; index < len(key); index++ {
		character := key[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return &ValidationError{Field: "idempotency_key", Reason: "must be an HTTP token"}
	}
	return nil
}

// FingerprintCreateInput returns a stable SHA-256 fingerprint of canonical
// create fields. Equivalent edge padding therefore replays safely, while a
// changed name or description conflicts.
func FingerprintCreateInput(input CreateInput) (string, error) {
	canonical, err := ValidateCreateInput(input)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}{Name: canonical.Name, Description: canonical.Description})
	if err != nil {
		return "", fmt.Errorf("item: fingerprint input: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// WithIdempotencyScope attaches a bounded caller scope to ctx. HTTP adapters
// should set this to a route plus an authenticated principal (or a trusted
// direct peer identity) so two clients cannot replay one another's response.
// The scope is hashed before it reaches an adapter; it is never persisted.
func WithIdempotencyScope(ctx context.Context, scope string) context.Context {
	if ctx == nil {
		return nil
	}
	if scope == "" || len(scope) > 512 || !utf8.ValidString(scope) || strings.IndexFunc(scope, unicode.IsControl) >= 0 {
		scope = defaultIdempotencyScope
	}
	return context.WithValue(ctx, idempotencyScopeKey{}, scope)
}

// IdempotencyScopeFromContext returns the configured scope or the stable
// default used by direct (non-HTTP) callers.
func IdempotencyScopeFromContext(ctx context.Context) string {
	if ctx == nil {
		return defaultIdempotencyScope
	}
	if scope, ok := ctx.Value(idempotencyScopeKey{}).(string); ok && scope != "" {
		return scope
	}
	return defaultIdempotencyScope
}

type idempotencyScopeKey struct{}

// storageIdempotencyKey produces a token-safe, opaque key for the adapter.
// Neither the caller's raw key nor its scope is retained by memory or SQL
// stores. The version prefix permits a future hash/scoping migration.
func storageIdempotencyKey(ctx context.Context, key string) string {
	scope := IdempotencyScopeFromContext(ctx)
	// JSON's length/escaping rules make the scope/key pair unambiguous even if
	// a future caller supplies delimiter-like bytes. The serialized payload is
	// hashed immediately and is never retained.
	payload, _ := json.Marshal(struct {
		Scope string `json:"scope"`
		Key   string `json:"key"`
	}{Scope: scope, Key: key})
	digest := sha256.Sum256(append([]byte("go-clean-architecture/idempotency/v1:"), payload...))
	return "v1_" + hex.EncodeToString(digest[:])
}

// CreateIdempotent validates and canonicalises input, then delegates one
// atomic persistence operation. A service only advertises idempotency when its
// Store itself implements IdempotentCreateStore; this prevents split-brain
// item/idempotency backends.
func (service *Service) CreateIdempotent(ctx context.Context, input CreateInput, key string) (Item, bool, error) {
	if err := contextError(ctx); err != nil {
		return Item{}, false, err
	}
	if err := ValidateIdempotencyKey(key); err != nil {
		return Item{}, false, err
	}
	canonical, err := ValidateCreateInput(input)
	if err != nil {
		return Item{}, false, err
	}
	if service == nil || service.store == nil {
		return Item{}, false, ErrIdempotencyUnavailable
	}
	store, ok := service.store.(IdempotentCreateStore)
	if !ok || store == nil {
		return Item{}, false, ErrIdempotencyUnavailable
	}
	fingerprint, err := FingerprintCreateInput(canonical)
	if err != nil {
		return Item{}, false, err
	}

	id, err := service.generateID()
	if err != nil {
		return Item{}, false, errors.Join(ErrIDGeneration, err)
	}
	if id == uuid.Nil || id.Version() != uuid.Version(4) || id.Variant() != uuid.RFC4122 {
		return Item{}, false, errors.Join(ErrIDGeneration, ErrInvalidInput,
			fmt.Errorf("generated UUID must be RFC4122 version 4"))
	}
	createdAt := service.clock()
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	createdAt = createdAt.UTC()
	value := Item{ID: id, Name: canonical.Name, Description: canonical.Description, CreatedAt: createdAt}

	created, replayed, err := store.CreateIdempotent(ctx, value, storageIdempotencyKey(ctx, key), fingerprint)
	if err != nil {
		return Item{}, false, err
	}
	if created.ID == uuid.Nil {
		return Item{}, false, ErrIdempotencyState
	}
	return created, replayed, nil
}

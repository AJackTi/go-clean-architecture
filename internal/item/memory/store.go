// Package memory provides a race-safe in-memory item Store.
//
// It is useful for tests, examples, and local deployments that do not need
// durable storage.  The zero value is ready to use.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/google/uuid"
)

var _ item.Store = (*Store)(nil)

var _ item.IdempotentCreateStore = (*Store)(nil)

// Store keeps independent value copies behind a mutex.  Item currently has
// value-only fields; copying on reads also prevents future slice operations
// from aliasing the store's internal state.
type Store struct {
	mu            sync.RWMutex
	items         map[uuid.UUID]item.Item
	idempotencyMu sync.Mutex
	idempotency   map[[32]byte]idempotencyEntry
	inflight      map[[32]byte]struct{}
	maxEntries    int
	clockMu       sync.RWMutex
	clock         func() time.Time
}

// Option configures adapter-only behaviour. Domain policy remains in item;
// the clock option exists so retention cleanup can be tested deterministically.
type Option func(*Store)

// WithClock replaces the wall clock used for idempotency retention timestamps.
// A nil clock restores the production wall clock.
func WithClock(clock func() time.Time) Option {
	return func(store *Store) {
		store.clockMu.Lock()
		store.clock = clock
		store.clockMu.Unlock()
	}
}

// NewStore returns an empty Store.  The Store's zero value is also usable.
func NewStore(options ...Option) *Store {
	store := &Store{
		items:       make(map[uuid.UUID]item.Item),
		idempotency: make(map[[32]byte]idempotencyEntry),
		inflight:    make(map[[32]byte]struct{}),
		maxEntries:  item.MaxIdempotencyEntries,
	}
	for _, option := range options {
		if option != nil {
			option(store)
		}
	}
	return store
}

// New is a concise alias for NewStore, convenient in small examples.
func New() *Store { return NewStore() }

// Create inserts an item and rejects an existing UUID with item.ErrConflict.
func (store *Store) Create(ctx context.Context, value item.Item) (item.Item, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	if store == nil {
		return item.Item{}, item.ErrStoreUnavailable
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	if store.items == nil {
		store.items = make(map[uuid.UUID]item.Item)
	}
	if _, exists := store.items[value.ID]; exists {
		return item.Item{}, fmt.Errorf("%w: id %s", item.ErrConflict, value.ID)
	}
	store.items[value.ID] = value
	return value, nil
}

// Get returns a value copy or item.ErrNotFound when the UUID does not exist.
func (store *Store) Get(ctx context.Context, id uuid.UUID) (item.Item, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	if store == nil {
		return item.Item{}, item.ErrStoreUnavailable
	}
	if id == uuid.Nil {
		return item.Item{}, &item.ValidationError{Field: "id", Reason: "must not be nil"}
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	value, exists := store.items[id]
	if !exists {
		return item.Item{}, fmt.Errorf("%w: id %s", item.ErrNotFound, id)
	}
	return value, nil
}

// List returns at most params.Limit values, ordered by CreatedAt descending
// and then ID descending.  It intentionally does not apply the service's
// MaxPageSize because Service requests MaxPageSize+1 to calculate HasMore.
func (store *Store) List(ctx context.Context, params item.ListParams) ([]item.Item, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, item.ErrStoreUnavailable
	}
	if params.Limit < 0 || params.Limit > item.MaxPageSize+1 {
		return nil, &item.ValidationError{Field: "limit", Reason: "is outside the supported range"}
	}
	if params.Offset < 0 {
		return nil, &item.ValidationError{Field: "offset", Reason: "must be zero or positive"}
	}

	store.mu.RLock()
	values := make([]item.Item, 0, len(store.items))
	for _, value := range store.items {
		values = append(values, value)
	}
	store.mu.RUnlock()

	sort.Slice(values, func(left, right int) bool {
		if values[left].CreatedAt.Equal(values[right].CreatedAt) {
			return values[left].ID.String() > values[right].ID.String()
		}
		return values[left].CreatedAt.After(values[right].CreatedAt)
	})
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	if params.Limit == 0 || params.Offset >= len(values) {
		return []item.Item{}, nil
	}
	remaining := len(values) - params.Offset
	count := params.Limit
	if count > remaining {
		count = remaining
	}
	page := make([]item.Item, count)
	copy(page, values[params.Offset:params.Offset+count])
	return page, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return &item.ValidationError{Field: "context", Reason: "must not be nil"}
	}
	return ctx.Err()
}

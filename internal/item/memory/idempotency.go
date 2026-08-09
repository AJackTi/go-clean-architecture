package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/google/uuid"
)

type idempotencyEntry struct {
	fingerprint [32]byte
	itemID      uuid.UUID
	expiresAt   time.Time
}

// CreateIdempotent atomically coordinates an Item insert and its replay
// record. A per-key in-flight marker makes concurrent duplicates fail fast,
// while the item mutex keeps the memory Store's ordinary operations race-safe.
// Only SHA-256 digests are retained; raw client keys never live in memory.
func (store *Store) CreateIdempotent(ctx context.Context, value item.Item, key, fingerprint string) (item.Item, bool, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, false, err
	}
	if err := validateRequest(key, fingerprint, value); err != nil {
		return item.Item{}, false, err
	}
	if store == nil {
		return item.Item{}, false, item.ErrIdempotencyUnavailable
	}

	keyHash := sha256.Sum256([]byte(key))
	requestHash, _ := decodeDigest(fingerprint)
	now := store.now()

	store.idempotencyMu.Lock()
	store.ensureIdempotencyMapsLocked()
	store.cleanupIdempotencyLocked(now)
	if existing, ok := store.idempotency[keyHash]; ok {
		if existing.fingerprint != requestHash {
			store.idempotencyMu.Unlock()
			return item.Item{}, false, item.ErrIdempotencyConflict
		}
		value, err := store.getItem(ctx, existing.itemID)
		store.idempotencyMu.Unlock()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return item.Item{}, false, err
			}
			return item.Item{}, false, item.ErrIdempotencyState
		}
		return value, true, nil
	}
	if _, inFlight := store.inflight[keyHash]; inFlight {
		store.idempotencyMu.Unlock()
		return item.Item{}, false, item.ErrIdempotencyInProgress
	}
	// Reserve a slot for this operation as well as completed entries. This
	// prevents a burst of unique keys from exceeding the hard bound while the
	// underlying Item writes are in progress.
	if len(store.idempotency)+len(store.inflight) >= store.maxIdempotencyEntries() {
		store.idempotencyMu.Unlock()
		return item.Item{}, false, item.ErrIdempotencyUnavailable
	}
	store.inflight[keyHash] = struct{}{}
	store.idempotencyMu.Unlock()

	created, err := store.Create(ctx, value)
	if err != nil {
		store.clearInFlight(keyHash)
		return item.Item{}, false, err
	}
	if created.ID == uuid.Nil {
		store.clearInFlight(keyHash)
		return item.Item{}, false, item.ErrIdempotencyState
	}

	// The memory write has completed. Record the replay identity even if the
	// request context was cancelled immediately afterwards; a client retry must
	// not create a second Item.
	store.idempotencyMu.Lock()
	store.ensureIdempotencyMapsLocked()
	delete(store.inflight, keyHash)
	store.idempotency[keyHash] = idempotencyEntry{
		fingerprint: requestHash,
		itemID:      created.ID,
		expiresAt:   store.now().Add(item.IdempotencyRecordRetention),
	}
	store.idempotencyMu.Unlock()

	if err := contextError(ctx); err != nil {
		return item.Item{}, false, err
	}
	return created, false, nil
}

func (store *Store) maxIdempotencyEntries() int {
	if store.maxEntries > 0 {
		return store.maxEntries
	}
	return item.MaxIdempotencyEntries
}

func validateRequest(key, fingerprint string, value item.Item) error {
	if err := item.ValidateIdempotencyKey(key); err != nil {
		return err
	}
	if value.ID == uuid.Nil {
		return item.ErrIdempotencyState
	}
	if _, ok := decodeDigest(fingerprint); !ok {
		return item.ErrIdempotencyState
	}
	return nil
}

func decodeDigest(value string) ([32]byte, bool) {
	var digest [32]byte
	if len(value) != hex.EncodedLen(len(digest)) {
		return digest, false
	}
	decoded, err := hex.Decode(digest[:], []byte(value))
	if err != nil || decoded != len(digest) {
		return digest, false
	}
	return digest, true
}

func (store *Store) clearInFlight(keyHash [32]byte) {
	store.idempotencyMu.Lock()
	delete(store.inflight, keyHash)
	store.idempotencyMu.Unlock()
}

func (store *Store) ensureIdempotencyMapsLocked() {
	if store.idempotency == nil {
		store.idempotency = make(map[[32]byte]idempotencyEntry)
	}
	if store.inflight == nil {
		store.inflight = make(map[[32]byte]struct{})
	}
}

func (store *Store) cleanupIdempotencyLocked(now time.Time) {
	for key, entry := range store.idempotency {
		if !now.Before(entry.expiresAt) {
			delete(store.idempotency, key)
		}
	}
}

func (store *Store) now() time.Time {
	store.clockMu.RLock()
	clock := store.clock
	store.clockMu.RUnlock()
	if clock == nil {
		return time.Now().UTC()
	}
	value := clock()
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func (store *Store) getItem(ctx context.Context, id uuid.UUID) (item.Item, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	value, ok := store.items[id]
	if !ok {
		return item.Item{}, item.ErrNotFound
	}
	return value, nil
}

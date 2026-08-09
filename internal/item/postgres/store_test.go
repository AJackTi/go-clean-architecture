package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTranslateError(t *testing.T) {
	if !errors.Is(translateError("get", pgx.ErrNoRows), item.ErrNotFound) {
		t.Fatal("pgx.ErrNoRows was not mapped to ErrNotFound")
	}
	unique := &pgconn.PgError{Code: "23505", ConstraintName: "items_pkey"}
	if !errors.Is(translateError("create", unique), item.ErrConflict) {
		t.Fatal("unique violation was not mapped to ErrConflict")
	}
	underlying := errors.New("connection reset")
	if errors.Is(translateError("list", underlying), item.ErrConflict) {
		t.Fatal("ordinary provider error was mapped to ErrConflict")
	}
}

func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE items (
			id UUID PRIMARY KEY,
			name VARCHAR(120) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL
		)`)
	if err != nil {
		t.Fatalf("create temporary items table: %v", err)
	}
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE item_idempotency_keys (
			key_hash BYTEA PRIMARY KEY CHECK (octet_length(key_hash) = 32),
			request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
			item_id UUID NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at)
		)`)
	if err != nil {
		t.Fatalf("create temporary idempotency table: %v", err)
	}

	store := &Store{db: tx}
	first := item.Item{
		ID:          uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Name:        "first",
		Description: "one",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("ICT", 7*60*60)),
	}
	second := item.Item{
		ID:        uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		Name:      "second",
		CreatedAt: first.CreatedAt.Add(time.Hour),
	}
	if _, err := store.Create(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := store.Create(ctx, second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	got, err := store.Get(ctx, first.ID)
	if err != nil || got.Name != first.Name || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("get = %#v, %v", got, err)
	}
	if _, err := store.Get(ctx, uuid.MustParse("33333333-3333-4333-8333-333333333333")); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("missing get = %v, want ErrNotFound", err)
	}
	values, err := store.List(ctx, item.ListParams{Limit: 3})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(values) != 2 || values[0].ID != second.ID || values[1].ID != first.ID {
		t.Fatalf("list order = %#v", values)
	}

	idempotentInput := item.CreateInput{Name: "idempotent", Description: "once"}
	fingerprint, err := item.FingerprintCreateInput(idempotentInput)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	firstCandidate := item.Item{
		ID:          uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		Name:        idempotentInput.Name,
		Description: idempotentInput.Description,
		CreatedAt:   first.CreatedAt,
	}
	created, replayed, err := store.CreateIdempotent(ctx, firstCandidate, "integration-key", fingerprint)
	if err != nil || replayed || created.ID != firstCandidate.ID {
		t.Fatalf("first idempotent create = %#v, replayed=%t, err=%v", created, replayed, err)
	}
	replayCandidate := firstCandidate
	replayCandidate.ID = uuid.MustParse("55555555-5555-4555-8555-555555555555")
	replayedItem, replayed, err := store.CreateIdempotent(ctx, replayCandidate, "integration-key", fingerprint)
	if err != nil || !replayed || replayedItem != created {
		t.Fatalf("idempotent replay = %#v, replayed=%t, err=%v; want %#v", replayedItem, replayed, err, created)
	}
	otherFingerprint := sha256.Sum256([]byte("different request"))
	if _, _, err := store.CreateIdempotent(ctx, replayCandidate, "integration-key", fmtFingerprint(otherFingerprint)); !errors.Is(err, item.ErrIdempotencyConflict) {
		t.Fatalf("idempotency mismatch = %v, want ErrIdempotencyConflict", err)
	}

	failedKey := "failed-key"
	if _, _, err := store.CreateIdempotent(ctx, first, failedKey, fingerprint); !errors.Is(err, item.ErrConflict) {
		t.Fatalf("failed atomic create = %v, want ErrConflict", err)
	}
	afterFailure := firstCandidate
	afterFailure.ID = uuid.MustParse("66666666-6666-4666-8666-666666666666")
	if _, replayed, err := store.CreateIdempotent(ctx, afterFailure, failedKey, fingerprint); err != nil || replayed {
		t.Fatalf("retry after rolled-back create = replayed=%t, err=%v", replayed, err)
	}

	expiredKey := "expired-key"
	expiredKeyHash := sha256.Sum256([]byte(expiredKey))
	requestHash, ok := decodeDigest(fingerprint)
	if !ok {
		t.Fatal("test fingerprint did not decode")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO item_idempotency_keys
			(key_hash, request_hash, item_id, created_at, expires_at)
		VALUES ($1, $2, $3, clock_timestamp() - interval '2 hours',
			clock_timestamp() - interval '1 hour')`, expiredKeyHash[:], requestHash[:], first.ID)
	if err != nil {
		t.Fatalf("seed expired idempotency row: %v", err)
	}
	afterExpiry := firstCandidate
	afterExpiry.ID = uuid.MustParse("77777777-7777-4777-8777-777777777777")
	if _, replayed, err := store.CreateIdempotent(ctx, afterExpiry, expiredKey, fingerprint); err != nil || replayed {
		t.Fatalf("create after expiry = replayed=%t, err=%v", replayed, err)
	}

	// A PostgreSQL transaction is aborted after a constraint violation. Keep
	// this assertion last so the rest of the read-path checks remain useful.
	if _, err := store.Create(ctx, first); !errors.Is(err, item.ErrConflict) {
		t.Fatalf("duplicate create = %v, want ErrConflict", err)
	}
}

func TestStoreIntegrationConcurrentSameKeyHasOneCommittedItem(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	key := "concurrent-" + uuid.NewString()
	fingerprint, err := item.FingerprintCreateInput(item.CreateInput{Name: "concurrent"})
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	keyHash := sha256.Sum256([]byte(key))
	const callers = 16
	ids := make([]uuid.UUID, callers)
	for index := range ids {
		ids[index] = uuid.New()
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM item_idempotency_keys WHERE key_hash = $1`, keyHash[:])
		for _, id := range ids {
			_, _ = pool.Exec(cleanupContext, `DELETE FROM items WHERE id = $1`, id)
		}
	})

	type result struct {
		value    item.Item
		replayed bool
		err      error
	}
	results := make(chan result, callers)
	var group sync.WaitGroup
	for index := 0; index < callers; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			store := NewStore(pool)
			value := item.Item{ID: ids[index], Name: "concurrent"}
			for attempt := 0; attempt < 300; attempt++ {
				created, replayed, createErr := store.CreateIdempotent(ctx, value, key, fingerprint)
				if !errors.Is(createErr, item.ErrIdempotencyInProgress) {
					results <- result{value: created, replayed: replayed, err: createErr}
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			results <- result{err: fmt.Errorf("same-key operation remained in progress")}
		}()
	}
	group.Wait()
	close(results)

	var committed item.Item
	createdCount := 0
	for outcome := range results {
		if outcome.err != nil {
			t.Fatalf("concurrent idempotent create: %v", outcome.err)
		}
		if !outcome.replayed {
			createdCount++
			committed = outcome.value
		}
		if outcome.value.ID == uuid.Nil {
			t.Fatalf("concurrent result has nil Item ID")
		}
		if committed.ID != uuid.Nil && outcome.value.ID != committed.ID {
			t.Fatalf("concurrent result IDs differ: %s and %s", outcome.value.ID, committed.ID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("non-replayed results = %d, want exactly 1", createdCount)
	}
}

func fmtFingerprint(value [sha256.Size]byte) string {
	return hex.EncodeToString(value[:])
}

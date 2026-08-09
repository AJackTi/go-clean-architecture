// Package postgres persists items in PostgreSQL using pgx.
package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createQuery = `
		INSERT INTO items (id, name, description, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, description, created_at`
	getQuery = `
		SELECT id, name, description, created_at
		FROM items
		WHERE id = $1`
	listQuery = `
		SELECT id, name, description, created_at
		FROM items
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`
	idempotencyCleanupQuery = `
		WITH expired AS (
			SELECT key_hash
			FROM item_idempotency_keys
			WHERE expires_at <= clock_timestamp()
			ORDER BY expires_at
			LIMIT 100
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM item_idempotency_keys AS keys
		USING expired
		WHERE keys.key_hash = expired.key_hash`
	idempotencyLockQuery   = `SELECT pg_try_advisory_xact_lock($1::bigint)`
	idempotencyLookupQuery = `
		SELECT request_hash, item_id
		FROM item_idempotency_keys
		WHERE key_hash = $1
		FOR UPDATE`
	idempotencyInsertQuery = `
		INSERT INTO item_idempotency_keys
			(key_hash, request_hash, item_id, created_at, expires_at)
		VALUES ($1, $2, $3, clock_timestamp(),
			clock_timestamp() + ($4::double precision * interval '1 second'))`
)

// Store is an item.Store backed by a pgx connection pool.
type Store struct {
	db querier
}

var _ item.Store = (*Store)(nil)

var _ item.IdempotentCreateStore = (*Store)(nil)

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// NewStore constructs a PostgreSQL item store.
func NewStore(pool *pgxpool.Pool) *Store {
	if pool == nil {
		return &Store{}
	}
	return &Store{db: pool}
}

// Create inserts one item.
func (store *Store) Create(ctx context.Context, value item.Item) (item.Item, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	if store == nil || store.db == nil {
		return item.Item{}, item.ErrStoreUnavailable
	}

	created, err := createWithQuerier(ctx, store.db, value)
	if err != nil {
		return item.Item{}, translateError("create", err)
	}
	return created, nil
}

// CreateIdempotent atomically inserts an Item and its replay record. A
// transaction-scoped advisory lock serializes the same opaque key across all
// application replicas without holding a process-wide lock. A concurrent
// request receives ErrIdempotencyInProgress instead of creating a second Item.
func (store *Store) CreateIdempotent(ctx context.Context, value item.Item, key, fingerprint string) (item.Item, bool, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, false, err
	}
	if err := validateIdempotentRequest(key, fingerprint, value); err != nil {
		return item.Item{}, false, err
	}
	if store == nil || store.db == nil {
		return item.Item{}, false, item.ErrIdempotencyUnavailable
	}
	beginner, ok := store.db.(txBeginner)
	if !ok {
		return item.Item{}, false, item.ErrIdempotencyUnavailable
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return item.Item{}, false, idempotencyUnavailable("begin", err)
	}
	defer func() {
		rollbackContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackContext)
	}()

	keyHash := sha256.Sum256([]byte(key))
	requestHash, _ := decodeDigest(fingerprint)
	var locked bool
	if err := tx.QueryRow(ctx, idempotencyLockQuery, advisoryLockID(keyHash)).Scan(&locked); err != nil {
		return item.Item{}, false, idempotencyUnavailable("advisory lock", err)
	}
	if !locked {
		return item.Item{}, false, item.ErrIdempotencyInProgress
	}
	if _, err := tx.Exec(ctx, idempotencyCleanupQuery); err != nil {
		return item.Item{}, false, idempotencyUnavailable("cleanup", err)
	}

	var storedHash []byte
	var storedID uuid.UUID
	lookupErr := tx.QueryRow(ctx, idempotencyLookupQuery, keyHash[:]).Scan(&storedHash, &storedID)
	switch {
	case lookupErr == nil:
		if len(storedHash) != sha256.Size {
			return item.Item{}, false, item.ErrIdempotencyState
		}
		if subtle.ConstantTimeCompare(storedHash, requestHash[:]) != 1 {
			return item.Item{}, false, item.ErrIdempotencyConflict
		}
		replayed, getErr := getWithQuerier(ctx, tx, storedID)
		if getErr != nil {
			if errors.Is(getErr, item.ErrNotFound) {
				return item.Item{}, false, item.ErrIdempotencyState
			}
			return item.Item{}, false, idempotencyUnavailable("replay", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return item.Item{}, false, idempotencyUnavailable("replay commit", err)
		}
		return replayed, true, nil
	case errors.Is(lookupErr, pgx.ErrNoRows):
		// The key is new after the bounded expiry cleanup below.
	default:
		return item.Item{}, false, idempotencyUnavailable("lookup", lookupErr)
	}

	created, err := createWithQuerier(ctx, tx, value)
	if err != nil {
		translated := translateError("idempotent create", err)
		if errors.Is(translated, item.ErrConflict) {
			return item.Item{}, false, translated
		}
		return item.Item{}, false, idempotencyUnavailable("create", translated)
	}
	if _, err := tx.Exec(ctx, idempotencyInsertQuery,
		keyHash[:], requestHash[:], created.ID, int64(item.IdempotencyRecordRetention/time.Second)); err != nil {
		translated := translateError("idempotency insert", err)
		if errors.Is(translated, item.ErrConflict) {
			return item.Item{}, false, translated
		}
		return item.Item{}, false, idempotencyUnavailable("insert", translated)
	}
	if err := tx.Commit(ctx); err != nil {
		return item.Item{}, false, idempotencyUnavailable("commit", err)
	}
	return created, false, nil
}

// Get retrieves one item by ID.
func (store *Store) Get(ctx context.Context, id uuid.UUID) (item.Item, error) {
	if err := contextError(ctx); err != nil {
		return item.Item{}, err
	}
	if store == nil || store.db == nil {
		return item.Item{}, item.ErrStoreUnavailable
	}
	if id == uuid.Nil {
		return item.Item{}, &item.ValidationError{Field: "id", Reason: "must not be nil"}
	}

	var found item.Item
	err := store.db.QueryRow(ctx, getQuery, id).Scan(
		&found.ID,
		&found.Name,
		&found.Description,
		&found.CreatedAt,
	)
	if err != nil {
		return item.Item{}, translateError("get", err)
	}
	found.CreatedAt = found.CreatedAt.UTC()
	return found, nil
}

// List returns items in deterministic newest-first order.
func (store *Store) List(ctx context.Context, params item.ListParams) ([]item.Item, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if store == nil || store.db == nil {
		return nil, item.ErrStoreUnavailable
	}
	if params.Limit < 0 || params.Limit > item.MaxPageSize+1 {
		return nil, &item.ValidationError{Field: "limit", Reason: "is outside the supported range"}
	}
	if params.Offset < 0 {
		return nil, &item.ValidationError{Field: "offset", Reason: "must be zero or positive"}
	}

	if params.Limit == 0 {
		return []item.Item{}, nil
	}
	rows, err := store.db.Query(ctx, listQuery, params.Limit, params.Offset)
	if err != nil {
		return nil, translateError("list", err)
	}
	defer rows.Close()

	values := make([]item.Item, 0, params.Limit)
	for rows.Next() {
		var value item.Item
		if err := rows.Scan(&value.ID, &value.Name, &value.Description, &value.CreatedAt); err != nil {
			return nil, translateError("list scan", err)
		}
		value.CreatedAt = value.CreatedAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, translateError("list rows", err)
	}
	return values, nil
}

func translateError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return item.ErrNotFound
	}

	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("item postgres %s: %w", operation, errors.Join(item.ErrConflict, err))
	}
	return fmt.Errorf("item postgres %s: %w", operation, err)
}

func createWithQuerier(ctx context.Context, database querier, value item.Item) (item.Item, error) {
	var created item.Item
	err := database.QueryRow(
		ctx,
		createQuery,
		value.ID,
		value.Name,
		value.Description,
		value.CreatedAt,
	).Scan(&created.ID, &created.Name, &created.Description, &created.CreatedAt)
	if err != nil {
		return item.Item{}, err
	}
	created.CreatedAt = created.CreatedAt.UTC()
	return created, nil
}

func getWithQuerier(ctx context.Context, database querier, id uuid.UUID) (item.Item, error) {
	var found item.Item
	err := database.QueryRow(ctx, getQuery, id).Scan(
		&found.ID,
		&found.Name,
		&found.Description,
		&found.CreatedAt,
	)
	if err != nil {
		return item.Item{}, translateError("replay get", err)
	}
	found.CreatedAt = found.CreatedAt.UTC()
	return found, nil
}

func validateIdempotentRequest(key, fingerprint string, value item.Item) error {
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

func decodeDigest(value string) ([sha256.Size]byte, bool) {
	var digest [sha256.Size]byte
	if len(value) != hex.EncodedLen(len(digest)) {
		return digest, false
	}
	decoded, err := hex.Decode(digest[:], []byte(value))
	if err != nil || decoded != len(digest) {
		return digest, false
	}
	return digest, true
}

func advisoryLockID(hash [sha256.Size]byte) int64 {
	// PostgreSQL accepts signed bigint values. Masking the sign bit keeps the
	// conversion within int64's range while retaining a deterministic 63-bit
	// value for the advisory-lock namespace.
	return int64(binary.BigEndian.Uint64(hash[:8]) & (uint64(1)<<63 - 1))
}

func idempotencyUnavailable(operation string, err error) error {
	if err == nil {
		return item.ErrIdempotencyUnavailable
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, item.ErrIdempotencyConflict) || errors.Is(err, item.ErrConflict) {
		return err
	}
	return fmt.Errorf("item postgres idempotency %s: %w", operation, errors.Join(item.ErrIdempotencyUnavailable, err))
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return &item.ValidationError{Field: "context", Reason: "must not be nil"}
	}
	return ctx.Err()
}

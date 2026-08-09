// Package postgres persists items in PostgreSQL using pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"

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
)

// Store is an item.Store backed by a pgx connection pool.
type Store struct {
	db querier
}

var _ item.Store = (*Store)(nil)

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
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

	var created item.Item
	err := store.db.QueryRow(
		ctx,
		createQuery,
		value.ID,
		value.Name,
		value.Description,
		value.CreatedAt,
	).Scan(&created.ID, &created.Name, &created.Description, &created.CreatedAt)
	if err != nil {
		return item.Item{}, translateError("create", err)
	}
	created.CreatedAt = created.CreatedAt.UTC()
	return created, nil
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

func contextError(ctx context.Context) error {
	if ctx == nil {
		return &item.ValidationError{Field: "context", Reason: "must not be nil"}
	}
	return ctx.Err()
}

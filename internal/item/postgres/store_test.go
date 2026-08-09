package postgres

import (
	"context"
	"errors"
	"os"
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
	// A PostgreSQL transaction is aborted after a constraint violation. Keep
	// this assertion last so the rest of the read-path checks remain useful.
	if _, err := store.Create(ctx, first); !errors.Is(err, item.ErrConflict) {
		t.Fatalf("duplicate create = %v, want ErrConflict", err)
	}
}

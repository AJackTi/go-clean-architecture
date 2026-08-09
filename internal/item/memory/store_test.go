package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/google/uuid"
)

func TestStoreCRUDAndDefensiveValues(t *testing.T) {
	store := NewStore()
	ctx := context.Background()
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	want := item.Item{
		ID:          id,
		Name:        "one",
		Description: "description",
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	got, err := store.Create(ctx, want)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got != want {
		t.Fatalf("Create() = %#v, want %#v", got, want)
	}
	if _, err := store.Create(ctx, want); !errors.Is(err, item.ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want ErrConflict", err)
	}
	got, err = store.Get(ctx, id)
	if err != nil || got != want {
		t.Fatalf("Get() = %#v, %v; want %#v", got, err, want)
	}
	if _, err := store.Get(ctx, uuid.New()); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("missing Get() error = %v, want ErrNotFound", err)
	}

	// Returned values are copies.  This assertion also documents that a future
	// pointer/slice field must be copied by the adapter before being exposed.
	got.Name = "caller mutation"
	again, err := store.Get(ctx, id)
	if err != nil || again.Name != want.Name {
		t.Fatalf("store value was mutated through returned value: %#v, %v", again, err)
	}
}

func TestStoreListOrderPaginationAndEmptySlice(t *testing.T) {
	store := NewStore()
	ctx := context.Background()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ids := []uuid.UUID{
		uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		uuid.MustParse("33333333-3333-4333-8333-333333333333"),
	}
	values := []item.Item{
		{ID: ids[0], Name: "old", CreatedAt: base},
		{ID: ids[1], Name: "newer", CreatedAt: base.Add(time.Hour)},
		// Same timestamp as the first item: the larger UUID must sort first.
		{ID: ids[2], Name: "same-time", CreatedAt: base},
	}
	for _, value := range values {
		if _, err := store.Create(ctx, value); err != nil {
			t.Fatalf("Create(%s) error = %v", value.Name, err)
		}
	}

	page, err := store.List(ctx, item.ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page) != 2 || page[0].ID != ids[1] || page[1].ID != ids[2] {
		t.Fatalf("ordered page = %#v", page)
	}
	page, err = store.List(ctx, item.ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List(offset) error = %v", err)
	}
	if len(page) != 1 || page[0].ID != ids[0] {
		t.Fatalf("offset page = %#v", page)
	}
	empty, err := store.List(ctx, item.ListParams{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatalf("List(empty) error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty page = %#v, want non-nil empty slice", empty)
	}
}

func TestStoreContextCancellation(t *testing.T) {
	store := NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := item.Item{ID: uuid.New(), Name: "one"}
	if _, err := store.Create(ctx, value); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create(canceled) error = %v", err)
	}
	if _, err := store.Get(ctx, value.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get(canceled) error = %v", err)
	}
	if _, err := store.List(ctx, item.ListParams{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(canceled) error = %v", err)
	}
	if _, err := store.List(nilContext(), item.ListParams{Limit: 1}); err == nil || !errors.Is(err, item.ErrInvalidInput) {
		t.Fatalf("List(nil context) error = %v, want ErrInvalidInput", err)
	}
}

func nilContext() context.Context { return nil }

func TestStoreRejectsInvalidListParams(t *testing.T) {
	store := NewStore()
	for _, params := range []item.ListParams{{Limit: -1}, {Offset: -1}} {
		if _, err := store.List(context.Background(), params); err == nil || !errors.Is(err, item.ErrInvalidInput) {
			t.Errorf("List(%#v) error = %v, want ErrInvalidInput", params, err)
		}
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	store := NewStore()
	ctx := context.Background()
	const writers = 32
	const readers = 32
	var group sync.WaitGroup
	group.Add(writers + readers)
	for index := 0; index < writers; index++ {
		index := index
		go func() {
			defer group.Done()
			value := item.Item{ID: uuid.New(), Name: fmt.Sprintf("item-%d", index), CreatedAt: time.Now().UTC()}
			if _, err := store.Create(ctx, value); err != nil {
				t.Errorf("concurrent Create() error = %v", err)
			}
			if _, err := store.Get(ctx, value.ID); err != nil {
				t.Errorf("concurrent Get() error = %v", err)
			}
		}()
	}
	for index := 0; index < readers; index++ {
		go func() {
			defer group.Done()
			if _, err := store.List(ctx, item.ListParams{Limit: 100}); err != nil {
				t.Errorf("concurrent List() error = %v", err)
			}
		}()
	}
	group.Wait()
	values, err := store.List(ctx, item.ListParams{Limit: writers})
	if err != nil {
		t.Fatalf("final List() error = %v", err)
	}
	if len(values) != writers {
		t.Fatalf("final item count = %d, want %d", len(values), writers)
	}
}

func TestZeroValueStore(t *testing.T) {
	var store Store
	value := item.Item{ID: uuid.New(), Name: "zero value", CreatedAt: time.Now().UTC()}
	if _, err := store.Create(context.Background(), value); err != nil {
		t.Fatalf("zero-value Create() error = %v", err)
	}
}

func TestNewAliasAndZeroLimit(t *testing.T) {
	store := New()
	if store == nil {
		t.Fatal("New() returned nil")
	}
	value := item.Item{ID: uuid.New(), Name: "one"}
	if _, err := store.Create(context.Background(), value); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	page, err := store.List(context.Background(), item.ListParams{Limit: 0})
	if err != nil {
		t.Fatalf("List(zero limit) error = %v", err)
	}
	if page == nil || len(page) != 0 {
		t.Fatalf("List(zero limit) = %#v, want non-nil empty slice", page)
	}
}

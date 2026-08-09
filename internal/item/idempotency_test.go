package item_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/AJackTi/go-clean-architecture/internal/item/memory"
	"github.com/google/uuid"
)

func TestFingerprintAndKeyPolicy(t *testing.T) {
	t.Parallel()

	fingerprint, err := item.FingerprintCreateInput(item.CreateInput{Name: "  keyboard ", Description: " quiet "})
	if err != nil {
		t.Fatalf("FingerprintCreateInput() error = %v", err)
	}
	canonical, err := item.FingerprintCreateInput(item.CreateInput{Name: "keyboard", Description: "quiet"})
	if err != nil || fingerprint != canonical {
		t.Fatalf("trimmed fingerprints = %q and %q, want equal", fingerprint, canonical)
	}
	if len(fingerprint) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(fingerprint))
	}

	for _, key := range []string{"", "has space", "quoted\"", strings.Repeat("a", item.MaxIdempotencyKeyBytes+1)} {
		if err := item.ValidateIdempotencyKey(key); err == nil {
			t.Errorf("ValidateIdempotencyKey(%q) succeeded, want error", key)
		}
	}
	if err := item.ValidateIdempotencyKey("client-123_ABC.~"); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}

func TestServiceCreateIdempotentReplayConflictAndScope(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(memory.WithClock(func() time.Time {
		return time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	}))
	ids := []uuid.UUID{
		uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		uuid.MustParse("44444444-4444-4444-8444-444444444444"),
	}
	var mu sync.Mutex
	nextID := 0
	service := item.NewService(store, item.WithIDGenerator(func() (uuid.UUID, error) {
		mu.Lock()
		defer mu.Unlock()
		id := ids[nextID%len(ids)]
		nextID++
		return id, nil
	}))
	input := item.CreateInput{Name: "keyboard", Description: "quiet"}
	ctxA := item.WithIdempotencyScope(context.Background(), "principal:a")
	first, replayed, err := service.CreateIdempotent(ctxA, input, "same-key")
	if err != nil || replayed {
		t.Fatalf("first create = %#v, replayed=%t, err=%v", first, replayed, err)
	}
	second, replayed, err := service.CreateIdempotent(ctxA, item.CreateInput{Name: " keyboard ", Description: "quiet "}, "same-key")
	if err != nil || !replayed || second != first {
		t.Fatalf("replay = %#v, replayed=%t, err=%v; want %#v", second, replayed, err, first)
	}
	if _, _, err := service.CreateIdempotent(ctxA, item.CreateInput{Name: "mouse"}, "same-key"); !errors.Is(err, item.ErrIdempotencyConflict) {
		t.Fatalf("payload mismatch = %v, want ErrIdempotencyConflict", err)
	}
	third, replayed, err := service.CreateIdempotent(item.WithIdempotencyScope(context.Background(), "principal:b"), input, "same-key")
	if err != nil || replayed || third.ID == first.ID {
		t.Fatalf("different scope = %#v, replayed=%t, err=%v; want a new Item", third, replayed, err)
	}
	page, err := service.List(context.Background(), item.ListParams{Limit: 10})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("stored item count = %d, err=%v; want 2", len(page.Items), err)
	}
}

func TestServiceCreateIdempotentRequiresCohesiveStore(t *testing.T) {
	t.Parallel()

	service := item.NewService(noIdempotencyStore{})
	if _, _, err := service.CreateIdempotent(context.Background(), item.CreateInput{Name: "keyboard"}, "key"); !errors.Is(err, item.ErrIdempotencyUnavailable) {
		t.Fatalf("missing capability error = %v, want ErrIdempotencyUnavailable", err)
	}
	if _, _, err := service.CreateIdempotent(context.Background(), item.CreateInput{Name: "keyboard"}, "bad key"); !errors.Is(err, item.ErrInvalidInput) {
		t.Fatalf("invalid key error = %v, want ErrInvalidInput", err)
	}
}

type noIdempotencyStore struct{}

func (noIdempotencyStore) Create(context.Context, item.Item) (item.Item, error) {
	return item.Item{}, nil
}

func (noIdempotencyStore) Get(context.Context, uuid.UUID) (item.Item, error) {
	return item.Item{}, item.ErrNotFound
}

func (noIdempotencyStore) List(context.Context, item.ListParams) ([]item.Item, error) {
	return nil, nil
}

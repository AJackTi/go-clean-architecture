package item_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item"
	"github.com/google/uuid"
)

func TestSignedCursorCodecRoundTripAndCanonicalWire(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 123456789, time.FixedZone("ICT", 7*60*60))
	codec, err := item.NewSignedCursorCodec(
		[]byte(strings.Repeat("secret", 8)),
		item.WithCursorClock(func() time.Time { return now }),
		item.WithCursorTTL(time.Hour),
	)
	if err != nil {
		t.Fatalf("NewSignedCursorCodec() error = %v", err)
	}
	position := item.CursorPosition{
		CreatedAt: now.Add(-5 * time.Minute),
		ID:        uuid.MustParse("c80c1043-a6cd-42bf-984e-0191352f4b26"),
	}
	token, err := codec.Encode(position)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !strings.HasPrefix(token, "v1_") || len(token) > item.MaxCursorBytes {
		t.Fatalf("token = %q, want bounded v1 token", token)
	}
	decoded, err := codec.Decode(token)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !decoded.CreatedAt.Equal(position.CreatedAt.UTC()) || decoded.ID != position.ID {
		t.Fatalf("decoded position = %#v, want %#v", decoded, position)
	}

	// The raw URL encoding is deliberately canonical. Adding padding or
	// changing alphabet spelling must not create alternate cache keys.
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "v1_"))
	if err != nil {
		t.Fatalf("decode token payload: %v", err)
	}
	canonical := "v1_" + base64.RawURLEncoding.EncodeToString(raw)
	if canonical != token {
		t.Fatalf("codec emitted non-canonical token %q", token)
	}
	if _, err := codec.Decode(token + "="); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("padded token error = %v, want ErrInvalidCursor", err)
	}
}

func TestSignedCursorCodecRejectsTamperingMalformedAndWrongPurpose(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	secret := []byte(strings.Repeat("k", item.MinCursorSigningKeyBytes))
	codec, err := item.NewCursorCodec(secret, item.WithCursorClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.Encode(item.CursorPosition{CreatedAt: now, ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"",
		"v2_" + strings.TrimPrefix(token, "v1_"),
		"v1_!not-base64",
		"v1_" + strings.Repeat("A", 10),
	}
	for _, malformed := range tests {
		malformed := malformed
		t.Run("malformed", func(t *testing.T) {
			t.Parallel()
			_, decodeErr := codec.Decode(malformed)
			if decodeErr == nil || !errors.Is(decodeErr, item.ErrInvalidCursor) {
				t.Fatalf("Decode(%q) error = %v, want ErrInvalidCursor", malformed, decodeErr)
			}
			if strings.Contains(decodeErr.Error(), malformed) && malformed != "" {
				t.Fatalf("error leaked raw token: %v", decodeErr)
			}
		})
	}

	encoded := strings.TrimPrefix(token, "v1_")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0x01
	forged := "v1_" + base64.RawURLEncoding.EncodeToString(raw)
	if _, err := codec.Decode(forged); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("forged token error = %v, want ErrInvalidCursor", err)
	}

	other, err := item.NewCursorCodec(secret, item.WithCursorPurpose("other-resource/v1"), item.WithCursorClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decode(token); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("wrong-purpose token error = %v, want ErrInvalidCursor", err)
	}
}

func TestSignedCursorCodecTTLAndClock(t *testing.T) {
	t.Parallel()

	var now time.Time
	now = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	codec, err := item.NewCursorCodec([]byte(strings.Repeat("x", item.MinCursorSigningKeyBytes)), item.WithCursorClock(clock), item.WithCursorTTL(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.Encode(item.CursorPosition{CreatedAt: now.Add(-time.Hour), ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(token); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := codec.Decode(token); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("expired token error = %v, want ErrInvalidCursor", err)
	}

	noExpiry, err := item.NewCursorCodec([]byte(strings.Repeat("y", item.MinCursorSigningKeyBytes)), item.WithCursorClock(clock), item.WithCursorTTL(0))
	if err != nil {
		t.Fatal(err)
	}
	token, err = noExpiry.Encode(item.CursorPosition{CreatedAt: time.Unix(0, 0), ID: uuid.New()})
	if err != nil {
		t.Fatalf("epoch Encode() error = %v", err)
	}
	now = now.Add(100 * 365 * 24 * time.Hour)
	if _, err := noExpiry.Decode(token); err != nil {
		t.Fatalf("non-expiring epoch token rejected: %v", err)
	}
}

func TestNewCursorCodecValidatesSecretAndOptions(t *testing.T) {
	t.Parallel()

	if _, err := item.NewCursorCodec([]byte("short")); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("short secret error = %v, want ErrInvalidCursor", err)
	}
	if _, err := item.NewCursorCodec([]byte(strings.Repeat("x", item.MinCursorSigningKeyBytes)), item.WithCursorTTL(-time.Second)); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("negative TTL error = %v, want ErrInvalidCursor", err)
	}
	if _, err := item.NewCursorCodec([]byte(strings.Repeat("x", item.MinCursorSigningKeyBytes)), item.WithCursorPurpose("bad\npurpose")); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("invalid purpose error = %v, want ErrInvalidCursor", err)
	}
}

func TestServiceListCursorUsesLookaheadCodecAndBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	codec, err := item.NewCursorCodec([]byte(strings.Repeat("s", item.MinCursorSigningKeyBytes)), item.WithCursorClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	rows := []item.Item{
		{ID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), CreatedAt: now.Add(-3 * time.Hour)},
		{ID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), CreatedAt: now.Add(-2 * time.Hour)},
		{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), CreatedAt: now.Add(-time.Hour)},
	}
	var received item.CursorListParams
	store := &cursorStoreStub{rows: rows, received: &received}
	service := item.NewService(store, item.WithCursorCodec(codec))
	page, err := service.ListCursor(context.Background(), item.CursorRequest{Limit: 2})
	if err != nil {
		t.Fatalf("ListCursor(first) error = %v", err)
	}
	if received.Limit != 3 || received.After != nil {
		t.Fatalf("store params = %#v, want lookahead=3 and nil boundary", received)
	}
	if len(page.Items) != 2 || !page.HasMore || page.Limit != 2 || page.NextCursor == "" {
		t.Fatalf("page = %#v", page)
	}
	position, err := codec.Decode(page.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	if position.ID != rows[1].ID || !position.CreatedAt.Equal(rows[1].CreatedAt) {
		t.Fatalf("next position = %#v, want last returned row %#v", position, rows[1])
	}

	received = item.CursorListParams{}
	second, err := service.ListCursor(context.Background(), item.CursorRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("ListCursor(second) error = %v", err)
	}
	if received.Limit != 3 || received.After == nil || received.After.ID != rows[1].ID {
		t.Fatalf("second store params = %#v, want decoded boundary", received)
	}
	if len(second.Items) != 1 || second.HasMore || second.NextCursor != "" || second.Items[0].ID != rows[0].ID {
		t.Fatalf("second page = %#v", second)
	}
}

func TestServiceListCursorFailsClosedWithoutCapabilityOrCodec(t *testing.T) {
	t.Parallel()

	request := item.CursorRequest{Limit: 1}
	if _, err := item.NewService(nil).ListCursor(context.Background(), request); !errors.Is(err, item.ErrCursorUnavailable) {
		t.Fatalf("nil store error = %v, want ErrCursorUnavailable", err)
	}
	store := &cursorStoreStub{rows: []item.Item{}}
	if _, err := item.NewService(store).ListCursor(context.Background(), request); !errors.Is(err, item.ErrCursorUnavailable) {
		t.Fatalf("nil codec error = %v, want ErrCursorUnavailable", err)
	}
	codec, err := item.NewCursorCodec([]byte(strings.Repeat("z", item.MinCursorSigningKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := item.NewService(&offsetOnlyStore{}, item.WithCursorCodec(codec)).ListCursor(context.Background(), request); !errors.Is(err, item.ErrCursorUnavailable) {
		t.Fatalf("missing store capability error = %v, want ErrCursorUnavailable", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := item.NewService(store, item.WithCursorCodec(codec)).ListCursor(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}
}

func TestServiceListCursorRejectsInvalidRequestAndStoreState(t *testing.T) {
	t.Parallel()

	codec, err := item.NewCursorCodec([]byte(strings.Repeat("a", item.MinCursorSigningKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := item.NewService(&cursorStoreStub{rows: []item.Item{
		{ID: uuid.Nil, CreatedAt: now},
		{ID: uuid.New(), CreatedAt: now.Add(-time.Second)},
	}}, item.WithCursorCodec(codec))
	if _, err := service.ListCursor(context.Background(), item.CursorRequest{Limit: -1}); err == nil || !errors.Is(err, item.ErrInvalidInput) {
		t.Fatalf("negative limit error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.ListCursor(context.Background(), item.CursorRequest{Limit: 1, Cursor: "v1_bad"}); err == nil || !errors.Is(err, item.ErrInvalidCursor) {
		t.Fatalf("invalid token error = %v, want ErrInvalidCursor", err)
	}
	// A malformed row is an adapter state failure, not a caller's bad token.
	if _, err := service.ListCursor(context.Background(), item.CursorRequest{Limit: 1}); err == nil || !errors.Is(err, item.ErrCursorState) {
		t.Fatalf("invalid store row error = %v, want ErrCursorState", err)
	}
}

type cursorStoreStub struct {
	rows     []item.Item
	received *item.CursorListParams
}

func (stub *cursorStoreStub) Create(context.Context, item.Item) (item.Item, error) {
	return item.Item{}, nil
}

func (stub *cursorStoreStub) Get(context.Context, uuid.UUID) (item.Item, error) {
	return item.Item{}, item.ErrNotFound
}

func (stub *cursorStoreStub) List(context.Context, item.ListParams) ([]item.Item, error) {
	return nil, nil
}

func (stub *cursorStoreStub) ListAfter(_ context.Context, params item.CursorListParams) ([]item.Item, error) {
	if stub.received != nil {
		*stub.received = params
	}
	rows := append([]item.Item(nil), stub.rows...)
	if params.After != nil {
		boundary := *params.After
		filtered := rows[:0]
		for _, row := range rows {
			if row.CreatedAt.Before(boundary.CreatedAt) ||
				(row.CreatedAt.Equal(boundary.CreatedAt) && strings.Compare(row.ID.String(), boundary.ID.String()) < 0) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].CreatedAt.Equal(rows[right].CreatedAt) {
			return rows[left].ID.String() > rows[right].ID.String()
		}
		return rows[left].CreatedAt.After(rows[right].CreatedAt)
	})
	if params.Limit >= 0 && len(rows) > params.Limit {
		rows = rows[:params.Limit]
	}
	return rows, nil
}

type offsetOnlyStore struct{}

func (*offsetOnlyStore) Create(context.Context, item.Item) (item.Item, error) {
	return item.Item{}, nil
}

func (*offsetOnlyStore) Get(context.Context, uuid.UUID) (item.Item, error) {
	return item.Item{}, item.ErrNotFound
}

func (*offsetOnlyStore) List(context.Context, item.ListParams) ([]item.Item, error) {
	return nil, nil
}

package item

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type serviceStoreStub struct {
	createFn func(context.Context, Item) (Item, error)
	getFn    func(context.Context, uuid.UUID) (Item, error)
	listFn   func(context.Context, ListParams) ([]Item, error)
}

func (stub serviceStoreStub) Create(ctx context.Context, value Item) (Item, error) {
	if stub.createFn != nil {
		return stub.createFn(ctx, value)
	}
	return value, nil
}

func (stub serviceStoreStub) Get(ctx context.Context, id uuid.UUID) (Item, error) {
	if stub.getFn != nil {
		return stub.getFn(ctx, id)
	}
	return Item{}, ErrNotFound
}

func (stub serviceStoreStub) List(ctx context.Context, params ListParams) ([]Item, error) {
	if stub.listFn != nil {
		return stub.listFn(ctx, params)
	}
	return []Item{}, nil
}

func TestValidateCreateInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     CreateInput
		wantError bool
		field     string
	}{
		{name: "required name", input: CreateInput{}, wantError: true, field: "name"},
		{name: "whitespace name", input: CreateInput{Name: " \t\n"}, wantError: true, field: "name"},
		{name: "name is measured in Unicode runes", input: CreateInput{Name: strings.Repeat("界", 120)}, wantError: false},
		{name: "name over limit", input: CreateInput{Name: strings.Repeat("界", 121)}, wantError: true, field: "name"},
		{name: "description at limit", input: CreateInput{Name: "ok", Description: strings.Repeat("é", 2000)}, wantError: false},
		{name: "description over limit", input: CreateInput{Name: "ok", Description: strings.Repeat("é", 2001)}, wantError: true, field: "description"},
		{name: "invalid name UTF-8", input: CreateInput{Name: string([]byte{0xff})}, wantError: true, field: "name"},
		{name: "invalid description UTF-8", input: CreateInput{Name: "ok", Description: string([]byte{0xff})}, wantError: true, field: "description"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateCreateInput(test.input)
			if test.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error does not unwrap to ErrInvalidInput: %v", err)
				}
				var validationErr *ValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("error is not ValidationError: %T", err)
				}
				if validationErr.Field != test.field {
					t.Fatalf("field = %q, want %q", validationErr.Field, test.field)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.input.Name == strings.Repeat("界", 120) && got.Name != test.input.Name {
				t.Fatalf("name changed unexpectedly")
			}
		})
	}

	input := CreateInput{Name: "  Café  ", Description: "  notes here \n"}
	got, err := ValidateCreateInput(input)
	if err != nil {
		t.Fatalf("unexpected trim error: %v", err)
	}
	if got.Name != "Café" || got.Description != "notes here" {
		t.Fatalf("canonical input = %#v, want trimmed fields", got)
	}
	if input.Name != "  Café  " || input.Description != "  notes here \n" {
		t.Fatal("validation mutated caller input")
	}
}

func TestValidationErrorFormatting(t *testing.T) {
	t.Parallel()

	var nilError *ValidationError
	if got := nilError.Error(); got != ErrInvalidInput.Error() {
		t.Fatalf("nil ValidationError = %q", got)
	}
	for _, validationErr := range []*ValidationError{
		{Reason: "request is invalid"},
		{Field: "name"},
		{Field: "name", Reason: "is required"},
	} {
		if got := validationErr.Error(); !strings.Contains(got, ErrInvalidInput.Error()) {
			t.Errorf("ValidationError.Error() = %q, missing sentinel text", got)
		}
		if !errors.Is(validationErr, ErrInvalidInput) {
			t.Errorf("ValidationError does not unwrap to ErrInvalidInput")
		}
	}
}

func TestNormalizeListParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     ListParams
		want      ListParams
		wantError bool
	}{
		{name: "default", input: ListParams{}, want: ListParams{Limit: DefaultPageSize}},
		{name: "explicit", input: ListParams{Limit: 7, Offset: 12}, want: ListParams{Limit: 7, Offset: 12}},
		{name: "negative limit", input: ListParams{Limit: -1}, wantError: true},
		{name: "over max", input: ListParams{Limit: MaxPageSize + 1}, wantError: true},
		{name: "negative offset", input: ListParams{Offset: -1}, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeListParams(test.input)
			if test.wantError {
				if err == nil || !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("params = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestServiceCreateAssignsIdentityAndCanonicalFields(t *testing.T) {
	t.Parallel()

	wantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	wantTime := time.Date(2026, time.August, 9, 7, 8, 9, 123, time.FixedZone("ICT", 7*60*60))
	var persisted Item
	store := serviceStoreStub{createFn: func(ctx context.Context, value Item) (Item, error) {
		if ctx == nil {
			t.Fatal("store received nil context")
		}
		persisted = value
		return value, nil
	}}
	service := NewService(store,
		WithIDGenerator(func() (uuid.UUID, error) { return wantID, nil }),
		WithClock(func() time.Time { return wantTime }),
	)

	got, err := service.Create(context.Background(), CreateInput{
		Name:        "  名前  ",
		Description: "  mô tả  ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.ID != wantID || got.Name != "名前" || got.Description != "mô tả" {
		t.Fatalf("created item = %#v", got)
	}
	if !got.CreatedAt.Equal(wantTime) || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v, want UTC instant %v", got.CreatedAt, wantTime)
	}
	if persisted != got {
		t.Fatalf("store received %#v, returned %#v", persisted, got)
	}
}

func TestServiceCreateValidationAndContext(t *testing.T) {
	t.Parallel()

	called := false
	store := serviceStoreStub{createFn: func(context.Context, Item) (Item, error) {
		called = true
		return Item{}, nil
	}}
	service := NewService(store)

	if _, err := service.Create(context.Background(), CreateInput{Name: ""}); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() error = %v, want ErrInvalidInput", err)
	}
	if called {
		t.Fatal("store called for invalid input")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Create(ctx, CreateInput{Name: "valid"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create(canceled) error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("store called for canceled context")
	}
}

func TestServiceCreatePropagatesStoreAndIDErrors(t *testing.T) {
	t.Parallel()

	storeErr := errors.Join(ErrConflict, errors.New("duplicate"))
	service := NewService(serviceStoreStub{createFn: func(context.Context, Item) (Item, error) {
		return Item{}, storeErr
	}})
	if _, err := service.Create(context.Background(), CreateInput{Name: "valid"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() error = %v, want ErrConflict", err)
	}

	generationErr := errors.New("entropy unavailable")
	service = NewService(serviceStoreStub{}, WithIDGenerator(func() (uuid.UUID, error) {
		return uuid.Nil, generationErr
	}))
	if _, err := service.Create(context.Background(), CreateInput{Name: "valid"}); !errors.Is(err, ErrIDGeneration) || !errors.Is(err, generationErr) {
		t.Fatalf("Create() error = %v, want joined ID error", err)
	}

	service = NewService(serviceStoreStub{}, WithIDGenerator(func() (uuid.UUID, error) {
		return uuid.MustParse("11111111-1111-1111-8111-111111111111"), nil
	}))
	if _, err := service.Create(context.Background(), CreateInput{Name: "valid"}); !errors.Is(err, ErrIDGeneration) || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create(non-v4 ID) error = %v, want ID generation validation error", err)
	}
}

func TestServiceCreateUsesUTCNowWhenClockReturnsZero(t *testing.T) {
	t.Parallel()

	service := NewService(serviceStoreStub{},
		WithIDGenerator(func() (uuid.UUID, error) {
			return uuid.MustParse("11111111-1111-4111-8111-111111111111"), nil
		}),
		WithClock(func() time.Time { return time.Time{} }),
	)
	created, err := service.Create(context.Background(), CreateInput{Name: "valid"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CreatedAt.IsZero() || created.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v, want non-zero UTC value", created.CreatedAt)
	}
}

func TestServiceGet(t *testing.T) {
	t.Parallel()

	wantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	want := Item{ID: wantID, Name: "one"}
	var receivedID uuid.UUID
	service := NewService(serviceStoreStub{getFn: func(ctx context.Context, id uuid.UUID) (Item, error) {
		receivedID = id
		return want, nil
	}})
	got, err := service.Get(context.Background(), wantID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != want || receivedID != wantID {
		t.Fatalf("Get() = %#v, id = %s", got, receivedID)
	}

	if _, err := service.Get(context.Background(), uuid.Nil); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Get(nil) error = %v, want ErrInvalidInput", err)
	}

	service = NewService(serviceStoreStub{getFn: func(context.Context, uuid.UUID) (Item, error) {
		return Item{}, ErrNotFound
	}})
	if _, err := service.Get(context.Background(), wantID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrNotFound", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Get(ctx, wantID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get(canceled) error = %v, want context.Canceled", err)
	}
}

func TestServiceListUsesLookaheadAndReportsPage(t *testing.T) {
	t.Parallel()

	rows := make([]Item, 21)
	for index := range rows {
		rows[index].ID = uuid.New()
		rows[index].Name = "item"
	}
	var received ListParams
	service := NewService(serviceStoreStub{listFn: func(ctx context.Context, params ListParams) ([]Item, error) {
		received = params
		return rows, nil
	}})

	page, err := service.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if received != (ListParams{Limit: DefaultPageSize + 1, Offset: 0}) {
		t.Fatalf("store params = %#v, want lookahead", received)
	}
	if page.Limit != DefaultPageSize || page.Offset != 0 || !page.HasMore || len(page.Items) != DefaultPageSize {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0] != rows[0] {
		t.Fatal("page item changed while copying store result")
	}
	rows[0].Name = "mutated"
	if page.Items[0].Name == "mutated" {
		t.Fatal("page aliases store result")
	}
}

func TestServiceListNoLookaheadWhenShortAndValidatesParams(t *testing.T) {
	t.Parallel()

	called := false
	service := NewService(serviceStoreStub{listFn: func(context.Context, ListParams) ([]Item, error) {
		called = true
		return []Item{{ID: uuid.New(), Name: "one"}}, nil
	}})
	page, err := service.List(context.Background(), ListParams{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !called || page.HasMore || len(page.Items) != 1 || page.Limit != 2 || page.Offset != 4 {
		t.Fatalf("page = %#v, called=%v", page, called)
	}

	for _, params := range []ListParams{{Limit: -1}, {Limit: MaxPageSize + 1}, {Offset: -1}} {
		if _, err := service.List(context.Background(), params); err == nil || !errors.Is(err, ErrInvalidInput) {
			t.Errorf("List(%#v) error = %v, want ErrInvalidInput", params, err)
		}
	}

	storeErr := errors.New("list failed")
	service = NewService(serviceStoreStub{listFn: func(context.Context, ListParams) ([]Item, error) {
		return nil, storeErr
	}})
	if _, err := service.List(context.Background(), ListParams{}); !errors.Is(err, storeErr) {
		t.Fatalf("List(store error) = %v, want store error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.List(ctx, ListParams{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(canceled) error = %v, want context.Canceled", err)
	}
}

func TestServiceNilStoreAndNilContext(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	if _, err := service.Create(context.Background(), CreateInput{Name: "valid"}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Create(nil store) error = %v", err)
	}
	if _, err := service.List(context.Background(), ListParams{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("List(nil store) error = %v", err)
	}
	if _, err := service.Get(context.Background(), uuid.New()); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Get(nil store) error = %v", err)
	}
	if _, err := service.Create(nilContext(), CreateInput{Name: "valid"}); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create(nil context) error = %v", err)
	}

	var nilService *Service
	if _, err := nilService.Create(context.Background(), CreateInput{Name: "valid"}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil Service Create() error = %v", err)
	}
	if _, err := nilService.Get(context.Background(), uuid.New()); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil Service Get() error = %v", err)
	}
	if _, err := nilService.List(context.Background(), ListParams{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil Service List() error = %v", err)
	}
}

func nilContext() context.Context { return nil }

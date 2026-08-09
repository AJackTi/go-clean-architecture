package item

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Store is the persistence seam used by Service.  Implementations must honor
// the supplied context and return list results in deterministic order:
// CreatedAt descending, then ID descending.  List returns at most params.Limit
// rows; Service requests one extra row to calculate Page.HasMore.
type Store interface {
	Create(ctx context.Context, value Item) (Item, error)
	Get(ctx context.Context, id uuid.UUID) (Item, error)
	List(ctx context.Context, params ListParams) ([]Item, error)
}

// IDGenerator allows tests and applications that need deterministic identity
// generation to provide their own source.  Production uses uuid.NewRandom.
type IDGenerator func() (uuid.UUID, error)

// Clock supplies creation times.  Production uses time.Now; tests can inject
// a fixed clock without changing domain code.
type Clock func() time.Time

// Option configures a Service.  Options are intentionally small: adapters
// should not be able to alter validation or pagination policy.
type Option func(*Service)

// WithIDGenerator overrides the UUID source used by Create.
func WithIDGenerator(generator IDGenerator) Option {
	return func(service *Service) {
		if generator != nil {
			service.generateID = generator
		}
	}
}

// WithClock overrides the source used for CreatedAt.
func WithClock(clock Clock) Option {
	return func(service *Service) {
		if clock != nil {
			service.clock = clock
		}
	}
}

// Service owns Item use cases and domain policy.  It is safe for concurrent
// use as long as its Store is safe for concurrent use (the memory adapter is).
type Service struct {
	store      Store
	generateID IDGenerator
	clock      Clock
}

// NewService constructs an Item service.  A nil Store is accepted so that
// configuration errors can be reported as ErrStoreUnavailable at call time
// rather than causing a process-start panic.
func NewService(store Store, options ...Option) *Service {
	service := &Service{
		store:      store,
		generateID: uuid.NewRandom,
		clock:      time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Create validates input, assigns a UUIDv4 and UTC creation timestamp, and
// persists the resulting item.
func (service *Service) Create(ctx context.Context, input CreateInput) (Item, error) {
	if err := contextError(ctx); err != nil {
		return Item{}, err
	}
	canonical, err := ValidateCreateInput(input)
	if err != nil {
		return Item{}, err
	}
	if service == nil || service.store == nil {
		return Item{}, ErrStoreUnavailable
	}

	id, err := service.generateID()
	if err != nil {
		return Item{}, errors.Join(ErrIDGeneration, err)
	}
	if id == uuid.Nil || id.Version() != uuid.Version(4) || id.Variant() != uuid.RFC4122 {
		return Item{}, errors.Join(ErrIDGeneration, ErrInvalidInput,
			fmt.Errorf("generated UUID must be RFC4122 version 4"))
	}

	createdAt := service.clock()
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	createdAt = createdAt.UTC()
	value := Item{
		ID:          id,
		Name:        canonical.Name,
		Description: canonical.Description,
		CreatedAt:   createdAt,
	}

	created, err := service.store.Create(ctx, value)
	if err != nil {
		return Item{}, err
	}
	return created, nil
}

// Get retrieves one item by UUID.
func (service *Service) Get(ctx context.Context, id uuid.UUID) (Item, error) {
	if err := contextError(ctx); err != nil {
		return Item{}, err
	}
	if id == uuid.Nil {
		return Item{}, &ValidationError{Field: "id", Reason: "must not be nil"}
	}
	if service == nil || service.store == nil {
		return Item{}, ErrStoreUnavailable
	}

	value, err := service.store.Get(ctx, id)
	if err != nil {
		return Item{}, err
	}
	return value, nil
}

// List returns a deterministic page.  The service asks the Store for one
// additional row so HasMore remains correct when the result count equals the
// requested page size.
func (service *Service) List(ctx context.Context, params ListParams) (Page, error) {
	if err := contextError(ctx); err != nil {
		return Page{}, err
	}
	normalized, err := NormalizeListParams(params)
	if err != nil {
		return Page{}, err
	}
	if service == nil || service.store == nil {
		return Page{}, ErrStoreUnavailable
	}
	rows, err := service.store.List(ctx, ListParams{
		Limit:  normalized.Limit + 1,
		Offset: normalized.Offset,
	})
	if err != nil {
		return Page{}, err
	}

	hasMore := len(rows) > normalized.Limit
	if hasMore {
		rows = rows[:normalized.Limit]
	}
	items := make([]Item, len(rows))
	copy(items, rows)
	return Page{
		Items:   items,
		Limit:   normalized.Limit,
		Offset:  normalized.Offset,
		HasMore: hasMore,
	}, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return &ValidationError{Field: "context", Reason: "must not be nil"}
	}
	return ctx.Err()
}

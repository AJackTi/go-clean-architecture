// Package item contains the business model and application service for items.
//
// The package deliberately has no knowledge of HTTP, SQL, or any other
// delivery/persistence mechanism.  Those concerns are adapters around the
// Store interface defined in service.go.
package item

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// DefaultPageSize is used when a list request does not specify a limit.
	DefaultPageSize = 20
	// MaxPageSize is the largest page a caller may request.
	MaxPageSize = 100

	// MinNameLength is the minimum number of Unicode characters in a name.
	MinNameLength = 1
	// MaxNameLength is the maximum number of Unicode characters in a name.
	MaxNameLength = 120
	// MaxDescriptionLength is the maximum number of Unicode characters in a description.
	MaxDescriptionLength = 2000
)

// Item is the business representation persisted by a Store.
//
// ID and CreatedAt are assigned by Service.Create.  CreatedAt is always UTC
// for items produced by the service.  The memory adapter also preserves the
// value exactly as supplied by a caller so that persistence adapters can own
// their usual round-trip semantics.
type Item struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateInput contains client-controlled fields for creating an Item.
// Identity and creation time are intentionally not accepted from callers.
type CreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListParams controls a paginated list operation.  A zero Limit means
// DefaultPageSize.  Negative values and values greater than MaxPageSize are
// invalid at the service boundary.  Offset is zero based.
type ListParams struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Page is the stable, transport-neutral result of a list operation.
type Page struct {
	Items      []Item `json:"items"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ValidateCreateInput validates and canonicalises client input.  Name and
// Description are trimmed at the edges; internal whitespace and Unicode are
// preserved.  A copy is returned so callers' input is never mutated.
func ValidateCreateInput(input CreateInput) (CreateInput, error) {
	name := strings.TrimSpace(input.Name)
	description := strings.TrimSpace(input.Description)

	if !utf8.ValidString(name) {
		return CreateInput{}, &ValidationError{Field: "name", Reason: "must be valid UTF-8"}
	}
	nameLength := utf8.RuneCountInString(name)
	if nameLength < MinNameLength {
		return CreateInput{}, &ValidationError{Field: "name", Reason: "is required"}
	}
	if nameLength > MaxNameLength {
		return CreateInput{}, &ValidationError{Field: "name", Reason: fmt.Sprintf("must contain at most %d Unicode characters", MaxNameLength)}
	}

	if !utf8.ValidString(description) {
		return CreateInput{}, &ValidationError{Field: "description", Reason: "must be valid UTF-8"}
	}
	if utf8.RuneCountInString(description) > MaxDescriptionLength {
		return CreateInput{}, &ValidationError{Field: "description", Reason: fmt.Sprintf("must contain at most %d Unicode characters", MaxDescriptionLength)}
	}

	return CreateInput{Name: name, Description: description}, nil
}

// NormalizeListParams validates and fills defaults for a list request.
// Limits above MaxPageSize are rejected rather than silently changed, making
// an accidentally expensive request visible to callers.
func NormalizeListParams(params ListParams) (ListParams, error) {
	if params.Limit == 0 {
		params.Limit = DefaultPageSize
	}
	if params.Limit < 0 {
		return ListParams{}, &ValidationError{Field: "limit", Reason: "must be zero or positive"}
	}
	if params.Limit > MaxPageSize {
		return ListParams{}, &ValidationError{Field: "limit", Reason: fmt.Sprintf("must be at most %d", MaxPageSize)}
	}
	if params.Offset < 0 {
		return ListParams{}, &ValidationError{Field: "offset", Reason: "must be zero or positive"}
	}
	return params, nil
}

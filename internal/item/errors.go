package item

import (
	"errors"
	"fmt"
)

// Sentinel errors are stable across adapters.  Callers should use
// errors.Is/errors.As instead of matching error strings.
var (
	ErrInvalidInput     = errors.New("item: invalid input")
	ErrNotFound         = errors.New("item: not found")
	ErrConflict         = errors.New("item: conflict")
	ErrStoreUnavailable = errors.New("item: store unavailable")
	ErrIDGeneration     = errors.New("item: generate id")
)

// ValidationError identifies a request field that violated a domain rule.
// It unwraps to ErrInvalidInput so transport adapters can map all validation
// failures consistently while still exposing a useful field/message locally.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrInvalidInput.Error()
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", ErrInvalidInput, e.Reason)
	}
	if e.Reason == "" {
		return fmt.Sprintf("%s: %s is invalid", ErrInvalidInput, e.Field)
	}
	return fmt.Sprintf("%s: %s %s", ErrInvalidInput, e.Field, e.Reason)
}

func (e *ValidationError) Unwrap() error { return ErrInvalidInput }

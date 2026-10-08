package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors. Application services return these (wrapped with context) and
// the HTTP layer maps them onto status codes. Domain and application code never
// depends on HTTP status codes.
var (
	// ErrNotFound is returned when an aggregate does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when a request contradicts current state, for
	// example an illegal state transition or a duplicate name.
	ErrConflict = errors.New("conflict")
	// ErrInvalid marks a validation failure of user supplied input.
	ErrInvalid = errors.New("invalid input")
	// ErrUnavailable is returned when the orchestration cannot proceed because
	// no suitable agent, no capacity, or no workspace is currently available.
	ErrUnavailable = errors.New("unavailable")
	// ErrDependencyCycle is returned when a task dependency would create a cycle.
	ErrDependencyCycle = errors.New("dependency cycle")
	// ErrNotImplemented marks capabilities that are intentionally absent.
	ErrNotImplemented = errors.New("not implemented")
)

// ValidationError describes why an input was rejected. Field is a stable
// machine readable path such as "priority" or "steps[1].role".
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", ErrInvalid, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", ErrInvalid, e.Field, e.Message)
}

// Unwrap lets callers use errors.Is(err, ErrInvalid).
func (e *ValidationError) Unwrap() error { return ErrInvalid }

// Invalidf builds a *ValidationError.
func Invalidf(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// NotFoundf wraps ErrNotFound with an entity description.
func NotFoundf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotFound, fmt.Sprintf(format, args...))
}

// Conflictf wraps ErrConflict with a reason.
func Conflictf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, args...))
}

// Unavailablef wraps ErrUnavailable with a reason.
func Unavailablef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnavailable, fmt.Sprintf(format, args...))
}

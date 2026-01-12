package source

import (
	"errors"
	"fmt"
)

// Common errors for source operations.
var (
	// ErrNoMatch indicates no source could extract hostnames from the labels.
	ErrNoMatch = errors.New("no source matched the provided labels")
)

// DuplicateSourceError indicates a source with the same name already exists.
type DuplicateSourceError struct {
	Name string
}

func (e *DuplicateSourceError) Error() string {
	return fmt.Sprintf("source %q already registered", e.Name)
}

// ErrDuplicateSource creates an error for duplicate source registration.
func ErrDuplicateSource(name string) error {
	return &DuplicateSourceError{Name: name}
}

// SourceNotFoundError indicates the requested source does not exist.
type SourceNotFoundError struct {
	Name string
}

func (e *SourceNotFoundError) Error() string {
	return fmt.Sprintf("source %q not found", e.Name)
}

// ErrSourceNotFound creates an error for a missing source.
func ErrSourceNotFound(name string) error {
	return &SourceNotFoundError{Name: name}
}

// ExtractionError indicates a problem parsing labels for hostname extraction.
type ExtractionError struct {
	Source  string
	Message string
	Err     error
}

func (e *ExtractionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("source %s: %s: %v", e.Source, e.Message, e.Err)
	}
	return fmt.Sprintf("source %s: %s", e.Source, e.Message)
}

func (e *ExtractionError) Unwrap() error {
	return e.Err
}

// WrapExtractionError wraps an error with source context.
func WrapExtractionError(source, message string, err error) error {
	return &ExtractionError{
		Source:  source,
		Message: message,
		Err:     err,
	}
}

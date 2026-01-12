package provider

import (
	"errors"
	"fmt"
)

// Common errors for provider operations.
var (
	// ErrNotFound indicates a record was not found.
	ErrNotFound = errors.New("record not found")

	// ErrConflict indicates a record already exists with the same hostname, type, and target.
	ErrConflict = errors.New("record already exists")

	// ErrTypeConflict indicates a record exists with a different type that conflicts.
	// For example, a CNAME cannot coexist with an A record at the same hostname.
	ErrTypeConflict = errors.New("record type conflict")

	// ErrUnauthorized indicates authentication failed.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrProviderUnavailable indicates the provider API is unreachable.
	ErrProviderUnavailable = errors.New("provider unavailable")
)

// ConfigError represents a configuration error.
type ConfigError struct {
	Field   string
	Value   string
	Message string
}

func (e *ConfigError) Error() string {
	if e.Value != "" {
		return fmt.Sprintf("configuration error: %s=%q: %s", e.Field, e.Value, e.Message)
	}
	return fmt.Sprintf("configuration error: %s: %s", e.Field, e.Message)
}

// ErrConfigMissing creates an error for a missing required configuration field.
func ErrConfigMissing(field string) error {
	return &ConfigError{
		Field:   field,
		Message: "required but not set",
	}
}

// ErrConfigInvalid creates an error for an invalid configuration value.
func ErrConfigInvalid(field, value, message string) error {
	return &ConfigError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// ProviderError wraps an error with provider context.
type ProviderError struct {
	Provider  string
	Operation string
	Err       error
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s: %s: %v", e.Provider, e.Operation, e.Err)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// WrapError wraps an error with provider context.
func WrapError(provider, operation string, err error) error {
	if err == nil {
		return nil
	}
	return &ProviderError{
		Provider:  provider,
		Operation: operation,
		Err:       err,
	}
}

// IsNotFound returns true if the error indicates a record was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsConflict returns true if the error indicates a record already exists.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// IsTypeConflict returns true if the error indicates a record type conflict.
// This occurs when trying to create a record that conflicts with an existing
// record of a different type (e.g., CNAME cannot coexist with A/AAAA).
func IsTypeConflict(err error) bool {
	return errors.Is(err, ErrTypeConflict)
}

// IsUnauthorized returns true if the error indicates authentication failed.
func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

// IsProviderUnavailable returns true if the error indicates the provider is unreachable.
func IsProviderUnavailable(err error) bool {
	return errors.Is(err, ErrProviderUnavailable)
}

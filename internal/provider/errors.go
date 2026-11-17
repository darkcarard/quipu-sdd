package provider

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotImplemented is returned by adapters that are not built yet.
var ErrNotImplemented = errors.New("provider: not implemented")

// ErrBudgetExceeded is returned when the run's token budget is spent.
var ErrBudgetExceeded = errors.New("provider: token budget exceeded")

// APIError is a failure reported by a provider or the transport to it.
type APIError struct {
	Provider   string
	StatusCode int    // HTTP status; 0 for transport errors and in-stream errors without one
	Type       string // provider error type, e.g. rate_limit_error, overloaded_error
	Message    string
	RequestID  string
	// RetryAfter is the delay the provider asked for (0 if none).
	RetryAfter time.Duration
	// Retryable marks transient failures: network errors, 408, 409, 429, 5xx, 529, overloads.
	Retryable bool
	Err       error // underlying transport error, if any
}

func (e *APIError) Error() string {
	var s string
	if e.Provider != "" {
		s = e.Provider + ": "
	}
	switch {
	case e.StatusCode != 0 && e.Type != "":
		s += fmt.Sprintf("%d %s: %s", e.StatusCode, e.Type, e.Message)
	case e.StatusCode != 0:
		s += fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
	case e.Type != "":
		s += fmt.Sprintf("%s: %s", e.Type, e.Message)
	default:
		s += e.Message
	}
	if e.RequestID != "" {
		s += " (request " + e.RequestID + ")"
	}
	return s
}

func (e *APIError) Unwrap() error { return e.Err }

// IsRetryable reports whether err is transient.
func IsRetryable(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Retryable
}

// RetryableStatus reports whether an HTTP status is worth retrying.
func RetryableStatus(code int) bool {
	return code == 408 || code == 409 || code == 429 || code >= 500
}

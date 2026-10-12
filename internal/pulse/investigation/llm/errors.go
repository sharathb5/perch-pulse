package llm

import "fmt"

// UnavailableError means LLM mode cannot run (disabled / missing credentials).
type UnavailableError struct {
	Reason string
}

func (e *UnavailableError) Error() string {
	if e == nil || e.Reason == "" {
		return "llm investigation unavailable"
	}
	return e.Reason
}

// ValidationError is a client/model output validation failure.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "llm validation failed"
	}
	return e.Msg
}

// RateLimitError indicates provider 429.
type RateLimitError struct {
	Msg string
}

func (e *RateLimitError) Error() string {
	if e == nil || e.Msg == "" {
		return "llm provider rate limited"
	}
	return e.Msg
}

// BusyError indicates concurrency limit reached.
type BusyError struct{}

func (e *BusyError) Error() string { return "llm investigation busy; try again" }

func fmtValidation(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

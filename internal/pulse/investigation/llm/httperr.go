package llm

import "fmt"

// ProviderHTTPError is a non-2xx provider response (body never echoed).
type ProviderHTTPError struct {
	StatusCode int
}

func (e *ProviderHTTPError) Error() string {
	if e == nil {
		return "llm: provider HTTP error"
	}
	return fmt.Sprintf("llm: provider HTTP %d", e.StatusCode)
}

// Transient reports whether the status is safe to retry under a bounded budget.
// Retries 429 and 5xx only — never 4xx validation/auth/config failures.
func (e *ProviderHTTPError) Transient() bool {
	if e == nil {
		return false
	}
	if e.StatusCode == 429 {
		return true
	}
	return e.StatusCode >= 500 && e.StatusCode <= 599
}

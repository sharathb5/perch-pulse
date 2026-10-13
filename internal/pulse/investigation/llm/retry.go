package llm

import (
	"context"
	"errors"
	"time"
)

const (
	defaultRetryAttempts = 3
	defaultRetryBackoff  = 200 * time.Millisecond
)

// RetryingChatClient wraps a ChatClient with bounded retries for transient
// provider failures only (429 / 5xx). Validation errors, unsupported
// configuration, and schema failures are never retried.
type RetryingChatClient struct {
	Inner       ChatClient
	MaxAttempts int
	Backoff     time.Duration
	// AfterFail is invoked after a transient failure before the next attempt
	// (tests may set it to a no-op to avoid sleeping).
	AfterFail func(attempt int, wait time.Duration)
}

// Complete performs up to MaxAttempts attempts for transient failures.
func (r *RetryingChatClient) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if r == nil || r.Inner == nil {
		return ChatResponse{}, &UnavailableError{Reason: "LLM provider client is not configured"}
	}
	attempts := r.MaxAttempts
	if attempts < 1 {
		attempts = defaultRetryAttempts
	}
	backoff := r.Backoff
	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}

	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return ChatResponse{}, err
		}
		resp, err := r.Inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		last = err
		if !isTransientProviderErr(err) || i == attempts-1 {
			return ChatResponse{}, err
		}
		wait := backoff * time.Duration(i+1)
		if r.AfterFail != nil {
			r.AfterFail(i, wait)
		} else if err := waitContext(ctx, wait); err != nil {
			return ChatResponse{}, err
		}
	}
	return ChatResponse{}, last
}

func waitContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientProviderErr(err error) bool {
	if err == nil {
		return false
	}
	var rate *RateLimitError
	if errors.As(err, &rate) {
		return true
	}
	var httpErr *ProviderHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Transient()
	}
	return false
}

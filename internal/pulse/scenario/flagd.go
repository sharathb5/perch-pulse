package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout = 10 * time.Second
	defaultFlagPoll    = 3 * time.Second
	flagPollInterval   = 100 * time.Millisecond
)

// FlagdClient talks to the Astronomy Shop flagd-ui HTTP API.
//
// The write endpoint replaces the entire flag document; callers must
// read-modify-write a single flag's defaultVariant. flagd-ui applies writes
// asynchronously (GenServer.cast), so callers should WaitForVariant after Set.
type FlagdClient struct {
	BaseURL    string
	HTTPClient *http.Client
	PollWait   time.Duration
}

// NewFlagdClient returns a client with an explicit HTTP timeout.
func NewFlagdClient(baseURL string) *FlagdClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultFlagdAPIBase
	}
	return &FlagdClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		PollWait: defaultFlagPoll,
	}
}

type flagDocument struct {
	Schema string               `json:"$schema,omitempty"`
	Flags  map[string]flagEntry `json:"flags"`
}

type flagEntry struct {
	DefaultVariant string         `json:"defaultVariant"`
	Description    string         `json:"description,omitempty"`
	State          string         `json:"state,omitempty"`
	Variants       map[string]any `json:"variants,omitempty"`
	Targeting      any            `json:"targeting,omitempty"`
	Metadata       any            `json:"metadata,omitempty"`
}

type writeBody struct {
	Data flagDocument `json:"data"`
}

// ReadFlags fetches the current flag document.
func (c *FlagdClient) ReadFlags(ctx context.Context) (flagDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/read", nil)
	if err != nil {
		return flagDocument{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return flagDocument{}, fmt.Errorf("scenario: flagd read: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return flagDocument{}, fmt.Errorf("scenario: flagd read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return flagDocument{}, fmt.Errorf("scenario: flagd read status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var doc flagDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return flagDocument{}, fmt.Errorf("scenario: flagd read json: %w", err)
	}
	if doc.Flags == nil {
		doc.Flags = map[string]flagEntry{}
	}
	return doc, nil
}

// GetVariant returns the current defaultVariant for flag.
func (c *FlagdClient) GetVariant(ctx context.Context, flag string) (string, error) {
	doc, err := c.ReadFlags(ctx)
	if err != nil {
		return "", err
	}
	entry, ok := doc.Flags[flag]
	if !ok {
		return "", fmt.Errorf("scenario: flag %q not found in flagd", flag)
	}
	return entry.DefaultVariant, nil
}

// WaitForVariant polls until flag shows want or the poll budget expires.
func (c *FlagdClient) WaitForVariant(ctx context.Context, flag, want string) (string, error) {
	wait := c.PollWait
	if wait <= 0 {
		wait = defaultFlagPoll
	}
	deadline := time.Now().Add(wait)
	var last string
	var lastErr error
	for {
		last, lastErr = c.GetVariant(ctx, flag)
		if lastErr == nil && last == want {
			return last, nil
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return last, lastErr
			}
			return last, fmt.Errorf("scenario: timed out waiting for flag %q want %q got %q", flag, want, last)
		}
		timer := time.NewTimer(flagPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}

// SetVariant read-modify-writes a single flag's defaultVariant.
func (c *FlagdClient) SetVariant(ctx context.Context, flag, variant string) error {
	doc, err := c.ReadFlags(ctx)
	if err != nil {
		return err
	}
	entry, ok := doc.Flags[flag]
	if !ok {
		return fmt.Errorf("scenario: flag %q not found in flagd", flag)
	}
	if entry.Variants != nil {
		if _, exists := entry.Variants[variant]; !exists {
			return fmt.Errorf("scenario: flag %q has no variant %q", flag, variant)
		}
	}
	entry.DefaultVariant = variant
	doc.Flags[flag] = entry

	payload, err := json.Marshal(writeBody{Data: doc})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/write", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("scenario: flagd write: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("scenario: flagd write body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("scenario: flagd write status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

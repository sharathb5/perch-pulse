package runpulse_test

import (
	"testing"

	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

func TestValidateAllowlistedHTTPURL_LoopbackOK(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080/jaeger/ui",
		"http://[::1]:8080/",
	} {
		if _, err := runpulse.ValidateAllowlistedHTTPURL(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}

func TestValidateAllowlistedHTTPURL_RejectsRemote(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"http://example.com/",
		"https://8.8.8.8/",
		"ftp://127.0.0.1/",
		"http://user:pass@127.0.0.1/",
		"http://127.0.0.1:8080/?token=secret",
		"http://127.0.0.1:8080/#frag",
		"",
	} {
		if _, err := runpulse.ValidateAllowlistedHTTPURL(raw); err == nil {
			t.Fatalf("expected reject for %q", raw)
		}
	}
}

func TestIsLoopbackRemoteAddr(t *testing.T) {
	t.Parallel()
	if !runpulse.IsLoopbackRemoteAddr("127.0.0.1:3131") {
		t.Fatal("expected loopback")
	}
	if !runpulse.IsLoopbackRemoteAddr("[::1]:80") {
		t.Fatal("expected ::1 loopback")
	}
	if runpulse.IsLoopbackRemoteAddr("8.8.8.8:443") {
		t.Fatal("expected reject")
	}
	if runpulse.IsLoopbackRemoteAddr("") {
		t.Fatal("empty must reject")
	}
}

func TestSafeHTTPClient_RejectsRedirect(t *testing.T) {
	t.Parallel()
	c := runpulse.SafeHTTPClient(nil)
	if c.CheckRedirect == nil {
		t.Fatal("CheckRedirect required")
	}
	if err := c.CheckRedirect(nil, nil); err == nil {
		t.Fatal("redirects must be rejected")
	}
}

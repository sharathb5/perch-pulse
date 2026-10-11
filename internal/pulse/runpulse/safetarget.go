package runpulse

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidateAllowlistedHTTPURL ensures a diagnostic target is an explicit http(s)
// URL whose host is a loopback IP literal or localhost. Query/fragment are
// rejected on configured bases. Dial-time checks re-verify loopback.
func ValidateAllowlistedHTTPURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("scheme %q not allowed (http/https only)", u.Scheme)
	}
	if u.User != nil {
		return nil, fmt.Errorf("userinfo not allowed in diagnostic URL")
	}
	if u.Opaque != "" {
		return nil, fmt.Errorf("opaque URLs not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("query/fragment not allowed on configured diagnostic base URL")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if err := assertLoopbackHost(host); err != nil {
		return nil, err
	}
	return u, nil
}

func assertLoopbackHost(host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() {
			return fmt.Errorf("host %q is not loopback", host)
		}
		return nil
	}
	// Resolve DNS; every address must be loopback (initial check).
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q resolved to no addresses", host)
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return fmt.Errorf("host %q resolved to non-loopback address %s", host, ip)
		}
	}
	return nil
}

// SafeHTTPClient returns an HTTP client that never follows redirects and
// re-validates that the dialed address is loopback (DNS rebinding defense).
func SafeHTTPClient(timeoutClient *http.Client) *http.Client {
	base := timeoutClient
	if base == nil {
		base = &http.Client{}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if err := assertLoopbackHost(host); err != nil {
				return nil, fmt.Errorf("dial blocked: %w", err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var last error
			for _, ipa := range ips {
				if !ipa.IP.IsLoopback() {
					last = fmt.Errorf("dial blocked: resolved non-loopback %s", ipa.IP)
					continue
				}
				target := net.JoinHostPort(ipa.IP.String(), port)
				conn, derr := dialer.DialContext(ctx, network, target)
				if derr == nil {
					return conn, nil
				}
				last = derr
			}
			if last == nil {
				last = fmt.Errorf("no loopback addresses for %s", host)
			}
			return nil, last
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   base.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("redirects are not allowed for Run Pulse diagnostics")
		},
	}
}

// IsLoopbackRemoteAddr reports whether addr (host:port or host) is loopback.
func IsLoopbackRemoteAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

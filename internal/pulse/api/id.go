package api

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxResourceIDLen = 512

// ValidateResourceID rejects empty, oversized, or path-traversal-prone IDs.
// Incident/change IDs may contain '/' and '|' (domain format); those are OK.
// The FileStore sanitizes path separators when writing filenames; this guard
// blocks ".." segments, NUL, and control characters before any filesystem touch.
func ValidateResourceID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("resource id is required")
	}
	if len(id) > maxResourceIDLen {
		return fmt.Errorf("resource id exceeds %d bytes", maxResourceIDLen)
	}
	if !utf8.ValidString(id) {
		return fmt.Errorf("resource id is not valid UTF-8")
	}
	if strings.Contains(id, "\x00") {
		return fmt.Errorf("resource id contains NUL")
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("resource id must not contain \"..\"")
	}
	for _, r := range id {
		if r < 0x20 {
			return fmt.Errorf("resource id contains control character")
		}
	}
	// Reject absolute / Windows drive forms that should never appear in Pulse IDs.
	if strings.HasPrefix(id, "/") || strings.HasPrefix(id, "\\") {
		return fmt.Errorf("resource id must not be absolute")
	}
	if len(id) >= 2 && id[1] == ':' {
		return fmt.Errorf("resource id must not look like a drive path")
	}
	return nil
}

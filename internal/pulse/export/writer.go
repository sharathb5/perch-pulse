package export

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// appendJSONLAtomic appends rows to dataset.jsonl by writing a full replacement
// file (existing + new) to a temp path and renaming. On failure the original
// file and checkpoint are left unchanged.
func appendJSONLAtomic(exportDir, dataset string, rows []any) (int, int64, error) {
	if len(rows) == 0 {
		return 0, 0, nil
	}
	name, err := DatasetJSONLName(dataset)
	if err != nil {
		return 0, 0, err
	}
	path, err := SafeJoin(exportDir, name)
	if err != nil {
		return 0, 0, err
	}
	tmpName := name + ".tmp"
	tmp, err := SafeJoin(exportDir, tmpName)
	if err != nil {
		return 0, 0, err
	}

	var existing []byte
	if raw, err := os.ReadFile(path); err == nil {
		existing = raw
	} else if !os.IsNotExist(err) {
		return 0, 0, fmt.Errorf("export: read %s: %w", name, err)
	}

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, 0, fmt.Errorf("export: open tmp %s: %w", tmpName, err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	w := bufio.NewWriter(f)
	var written int64
	if len(existing) > 0 {
		n, err := w.Write(existing)
		written += int64(n)
		if err != nil {
			return 0, 0, err
		}
		if existing[len(existing)-1] != '\n' {
			if err := w.WriteByte('\n'); err != nil {
				return 0, 0, err
			}
			written++
		}
	}
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return 0, 0, fmt.Errorf("export: marshal row: %w", err)
		}
		n, err := w.Write(line)
		written += int64(n)
		if err != nil {
			return 0, 0, err
		}
		if err := w.WriteByte('\n'); err != nil {
			return 0, 0, err
		}
		written++
	}
	if err := w.Flush(); err != nil {
		return 0, 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, 0, err
	}
	if err := f.Close(); err != nil {
		return 0, 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, 0, fmt.Errorf("export: rename %s: %w", name, err)
	}
	ok = true
	return len(rows), written, nil
}

// loadJSONLKeyIndex returns the latest content_fingerprint per dedupe_key in a JSONL file.
// Used to recover idempotency after a crash between JSONL rename and checkpoint save.
func loadJSONLKeyIndex(exportDir, dataset string) (map[string]string, error) {
	lines, err := readJSONLLines(exportDir, dataset)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range lines {
		var env Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue // validate will flag malformed lines
		}
		if env.DedupeKey == "" || env.ContentFingerprint == "" {
			continue
		}
		out[env.DedupeKey] = env.ContentFingerprint
	}
	return out, nil
}

// readJSONLLines reads non-empty lines from a dataset file (for validation).
func readJSONLLines(exportDir, dataset string) ([][]byte, error) {
	name, err := DatasetJSONLName(dataset)
	if err != nil {
		return nil, err
	}
	path, err := SafeJoin(exportDir, name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, []byte(line))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("export: scan %s line ~%d: %w", name, lineNo, err)
	}
	return lines, nil
}

// sortKeys returns sorted map keys for deterministic iteration.
func sortKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

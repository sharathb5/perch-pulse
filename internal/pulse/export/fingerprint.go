package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func sha256Hex(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func contentFingerprint(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// rowContentFingerprint hashes exported fields excluding run-specific exported_at
// and the fingerprint field itself.
func rowContentFingerprint(row any) (string, error) {
	raw, err := json.Marshal(row)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	delete(m, "exported_at")
	delete(m, "content_fingerprint")
	return contentFingerprint(m)
}

func attachFingerprint(env *Envelope, row any) error {
	if env == nil {
		return fmt.Errorf("export: nil envelope")
	}
	env.ContentFingerprint = ""
	fp, err := rowContentFingerprint(row)
	if err != nil {
		return err
	}
	env.ContentFingerprint = fp
	return nil
}

func statementHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}

func evalRunID(datasetID, generatedAtRFC3339, mode string) string {
	return "evalrun_" + sha256Hex(datasetID, generatedAtRFC3339, mode)[:24]
}

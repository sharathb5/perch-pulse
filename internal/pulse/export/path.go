package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveExportDir validates outDir is absolute or relative, cleans it, and
// ensures the fully symlink-resolved path stays under allowedRoot (or cwd).
func ResolveExportDir(outDir, allowedRoot string) (string, error) {
	if strings.TrimSpace(outDir) == "" {
		return "", fmt.Errorf("export: out-dir is required")
	}
	if strings.Contains(outDir, "\x00") {
		return "", fmt.Errorf("export: out-dir contains NUL")
	}
	clean := filepath.Clean(outDir)
	if !filepath.IsAbs(clean) {
		abs, err := filepath.Abs(clean)
		if err != nil {
			return "", fmt.Errorf("export: resolve out-dir: %w", err)
		}
		clean = abs
	}
	root := strings.TrimSpace(allowedRoot)
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = cwd
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("export: resolve allowed root: %w", err)
	}
	rootResolved, err := evalExisting(rootAbs)
	if err != nil {
		return "", fmt.Errorf("export: resolve allowed root symlinks: %w", err)
	}
	resolved, err := resolveAgainstRoot(clean, rootResolved)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// evalExisting returns the symlink-resolved absolute path for an existing path,
// or Abs for a path that does not exist yet (parents must still be checked).
func evalExisting(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return filepath.Abs(path)
		}
		return "", err
	}
	_ = fi
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func resolveAgainstRoot(candidate, rootResolved string) (string, error) {
	// Walk up to deepest existing ancestor; EvalSymlinks that ancestor so
	// intermediate symlink escapes are detected, then re-join missing parts.
	cur := candidate
	var missing []string
	for {
		_, err := os.Lstat(cur)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("export: lstat %s: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		missing = append([]string{filepath.Base(cur)}, missing...)
		cur = parent
	}
	existingResolved, err := evalExisting(cur)
	if err != nil {
		// Parent may not exist either (brand-new tree under root).
		if !os.IsNotExist(err) {
			// evalExisting uses Abs when not exist — only fail on other errors
			if _, statErr := os.Lstat(cur); !os.IsNotExist(statErr) {
				return "", fmt.Errorf("export: resolve path: %w", err)
			}
			existingResolved, err = filepath.Abs(cur)
			if err != nil {
				return "", err
			}
		}
	}
	for _, part := range missing {
		if part == ".." || part == "." || strings.Contains(part, string(os.PathSeparator)) {
			return "", fmt.Errorf("export: invalid path segment %q", part)
		}
		existingResolved = filepath.Join(existingResolved, part)
	}
	// If the full path already exists, resolve again (handles symlink out-dir).
	if _, err := os.Lstat(existingResolved); err == nil {
		existingResolved, err = evalExisting(existingResolved)
		if err != nil {
			return "", fmt.Errorf("export: resolve out-dir symlinks: %w", err)
		}
	}
	rel, err := filepath.Rel(rootResolved, existingResolved)
	if err != nil {
		return "", fmt.Errorf("export: path not under allowed root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("export: out-dir %q escapes allowed root %q", candidate, rootResolved)
	}
	return existingResolved, nil
}

// SafeJoin joins base and name, rejecting absolute name, .., separators, and
// refusing to follow an existing symlink at the destination.
func SafeJoin(base, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("export: invalid file name %q", name)
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("export: absolute file name rejected")
	}
	if strings.Contains(name, string(os.PathSeparator)) || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("export: file name must not contain path separators")
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("export: file name must not contain ..")
	}
	baseResolved, err := evalExisting(base)
	if err != nil {
		return "", fmt.Errorf("export: resolve base: %w", err)
	}
	joined := filepath.Join(baseResolved, name)
	rel, err := filepath.Rel(baseResolved, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("export: join escapes base")
	}
	if fi, err := os.Lstat(joined); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("export: refusing to write through symlink %s", name)
	}
	return joined, nil
}

// DatasetJSONLName returns <dataset>.jsonl after validating the dataset id.
func DatasetJSONLName(dataset string) (string, error) {
	if !validDataset(dataset) {
		return "", fmt.Errorf("export: unknown dataset %q", dataset)
	}
	return dataset + ".jsonl", nil
}

func validDataset(name string) bool {
	for _, d := range SupportedDatasets {
		if d == name {
			return true
		}
	}
	return false
}

package fsskillprovider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func isWindows() bool { return runtime.GOOS == "windows" }

// joinUnderRoot joins root + rel and ensures rel does not escape root.
// Root should be absolute/canonical-ish.
func joinUnderRoot(root, rel string) (string, error) {
	root = strings.TrimSpace(root)
	rel = strings.TrimSpace(rel)
	if root == "" {
		return "", errors.New("invalid root")
	}
	if rel == "" {
		return "", errors.New("invalid path")
	}
	if strings.ContainsRune(rel, '\x00') {
		return "", errors.New("path contains NUL byte")
	}
	// Windows-specific: reject drive/UNC-like relative paths such as "C:foo".
	if isWindows() && filepath.VolumeName(rel) != "" {
		return "", errors.New("path must be a pure relative path (no volume name)")
	}
	if filepath.IsAbs(rel) {
		return "", errors.New("path must be relative")
	}

	cleanRel := filepath.Clean(rel)
	if cleanRel == "." {
		return "", errors.New("path must not be '.'")
	}
	joined := filepath.Join(root, cleanRel)

	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	ok, err := withinRoot(root, abs)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("path escapes root: %q", rel)
	}
	return abs, nil
}

func withinRoot(root, p string) (bool, error) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false, err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	return true, nil
}

// relIsUnderDir checks that a user-supplied relative path is under a given top-level directory name,
// e.g. rel="scripts/x.sh" underDir="scripts".
func relIsUnderDir(rel, underDir string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	c := filepath.Clean(rel)
	parts := strings.FieldsFunc(c, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return false
	}
	return parts[0] == underDir
}

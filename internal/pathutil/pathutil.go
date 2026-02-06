package pathutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func IsWindows() bool { return runtime.GOOS == "windows" }

// JoinUnderRoot joins root + rel and ensures rel does not escape root.
// Root should be absolute/canonical-ish.
func JoinUnderRoot(root, rel string) (string, error) {
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

// RelIsUnderDir checks that a user-supplied relative path is under a given top-level directory name,
// e.g. rel="scripts/x.sh" underDir="scripts".
func RelIsUnderDir(rel, underDir string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	c := filepath.Clean(rel)
	parts := strings.Split(c, string(os.PathSeparator))
	if len(parts) == 0 {
		return false
	}
	return parts[0] == underDir
}

func POSIXInvoke(program string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, posixQuote(program))
	for _, a := range args {
		parts = append(parts, posixQuote(a))
	}
	return strings.Join(parts, " ")
}

func POSIXInvokeWithInterpreter(interpreter, scriptAbs string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	parts = append(parts, posixQuote(interpreter), posixQuote(scriptAbs))

	for _, a := range args {
		parts = append(parts, posixQuote(a))
	}
	return strings.Join(parts, " ")
}

// PowerShellInvoke uses "&" invocation: & 'C:\path\script.ps1' 'arg1' 'arg2'.
func PowerShellInvoke(scriptAbs string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	parts = append(parts, "&", psQuote(scriptAbs))

	for _, a := range args {
		parts = append(parts, psQuote(a))
	}
	return strings.Join(parts, " ")
}

// CmdInvoke is best-effort for .bat/.cmd. Quoting rules are gnarly; keep it minimal.
func CmdInvoke(scriptAbs string, args []string) string {
	parts := make([]string, 0, 2+len(args))
	parts = append(parts, "call", cmdQuote(scriptAbs))

	for _, a := range args {
		parts = append(parts, cmdQuote(a))
	}
	return strings.Join(parts, " ")
}

func cmdQuote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return `""`
	}
	// Minimal: wrap if spaces; escape embedded quotes by doubling.
	if strings.ContainsAny(s, " \t") || strings.ContainsRune(s, '"') {
		s = strings.ReplaceAll(s, `"`, `""`)
		return `"` + s + `"`
	}
	return s
}

// PowerShell quoting: single quotes, escape by doubling.
func psQuote(s string) string {
	// In PowerShell single-quoted strings escape ' by ''.
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// POSIX quoting: single-quote strategy.
func posixQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsRune(s, '\'') {
		return "'" + s + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

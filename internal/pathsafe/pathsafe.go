// Package pathsafe turns server-supplied names (course codes, folder paths,
// display names, module titles) into paths that cannot escape a root
// directory. Canvas content is instructor-controlled, so every name that
// reaches the filesystem goes through here.
package pathsafe

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrEscapesRoot is returned when a joined path would leave the root.
var ErrEscapesRoot = errors.New("path escapes the sync root")

// Name reduces one path component to a safe file or directory name: path
// separators and characters invalid on Windows/macOS/Linux are replaced,
// runs of spaces collapsed, and "." / ".." / empty become "_".
func Name(name string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", " -",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
		"\x00", "",
	)
	result := replacer.Replace(strings.TrimSpace(name))
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	result = strings.TrimSpace(result)
	// filepath.Base is the last line of defence against anything the
	// replacer did not catch; "." and ".." are never a usable component.
	result = filepath.Base(result)
	if result == "." || result == ".." || result == "" || result == string(filepath.Separator) {
		return "_"
	}
	return result
}

// Components splits a slash-separated server path ("a/b/../c") into safe
// components, dropping empties and any "." or "..".
func Components(serverPath string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(serverPath, func(r rune) bool { return r == '/' || r == '\\' }) {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		out = append(out, Name(part))
	}
	return out
}

// Join builds root/parts... from already-safe parts and then proves the
// result still lies under root (defence in depth; with Name/Components
// applied it always does).
func Join(root string, parts ...string) (string, error) {
	full := filepath.Join(append([]string{root}, parts...)...)
	if err := Within(root, full); err != nil {
		return "", err
	}
	return full, nil
}

// Within reports whether path is root or lies beneath it, lexically.
func Within(root, path string) error {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEscapesRoot, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: %s", ErrEscapesRoot, path)
	}
	return nil
}

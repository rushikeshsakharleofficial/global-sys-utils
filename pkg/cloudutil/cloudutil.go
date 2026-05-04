// Package cloudutil provides shared helpers for the cloud backup/restore tools.
package cloudutil

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var dateRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})|(\d{8})`)

// ExtractDate parses a YYYYMMDD or YYYY-MM-DD date from a filename.
func ExtractDate(name string) (time.Time, bool) {
	m := dateRe.FindString(name)
	if m == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"20060102", "2006-01-02"} {
		if t, err := time.Parse(layout, m); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// GlobMatch reports whether name matches the shell glob pattern.
func GlobMatch(name, pattern string) bool {
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// BuildObjectPath constructs the cloud storage path: [prefix/]hostname/reldir/filename.
func BuildObjectPath(path, prefix, hostname string) string {
	relDir := strings.TrimLeft(filepath.Dir(path), "/")
	var parts []string
	for _, p := range []string{prefix, hostname, relDir, filepath.Base(path)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// LocalPath resolves the local destination path for a downloaded object.
func LocalPath(key, prefix, dest string, flatten bool) string {
	if flatten {
		return filepath.Join(dest, filepath.Base(key))
	}
	relative := strings.TrimLeft(strings.TrimPrefix(key, prefix), "/")
	return filepath.Join(dest, relative)
}

// MultiFlag allows a flag to be specified multiple times.
type MultiFlag []string

func (m *MultiFlag) String() string      { return strings.Join(*m, ",") }
func (m *MultiFlag) Set(v string) error  { *m = append(*m, v); return nil }

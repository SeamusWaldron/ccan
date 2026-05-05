package redact

import (
	"os"
	"strings"
)

var home string

func init() {
	home, _ = os.UserHomeDir()
}

// Path replaces the home directory prefix with ~ in a path string.
func Path(s string) string {
	if home != "" && strings.HasPrefix(s, home) {
		return "~" + s[len(home):]
	}
	return s
}

// Text replaces the home directory in any string (e.g. path snippets in excerpts).
func Text(s string) string {
	if home != "" {
		return strings.ReplaceAll(s, home, "~")
	}
	return s
}

package state

import "strings"

// DecodeProjectDir converts Claude's encoded project dir name back into
// a real filesystem path. Claude replaces every "/" in the absolute path
// with "-". e.g. "-Users-lucasbacelo-foo" -> "/Users/lucasbacelo/foo".
func DecodeProjectDir(s string) string {
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "-", "/")
}

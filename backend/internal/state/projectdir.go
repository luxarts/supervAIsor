package state

import "strings"

// DecodeProjectDir converts Claude's encoded project dir name back into
// a real filesystem path. Claude encodes paths by replacing every "/"
// with "-" (e.g. "-Users-lucasbacelo-foo" -> "/Users/lucasbacelo/foo").
//
// If the input already starts with "/" the poller has already resolved
// the real path against the host filesystem (the only place that can
// disambiguate project names containing "-"), so we return it as-is.
func DecodeProjectDir(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	return strings.ReplaceAll(s, "-", "/")
}

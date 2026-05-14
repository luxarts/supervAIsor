package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveProjectPath converts Claude's encoded project dir name (where every
// "/" in the absolute path has been replaced with "-") back to the real
// filesystem path. Because project names may themselves contain "-", the
// encoded form is ambiguous; we resolve it greedily against the actual
// filesystem, preferring the longest segment that exists at each step.
//
// If resolution fails (filesystem path absent), falls back to naive
// "-"->"/" replacement so the caller still gets a usable string.
func ResolveProjectPath(encoded string) string {
	if encoded == "" {
		return ""
	}
	if !strings.HasPrefix(encoded, "-") {
		return encoded
	}

	parts := strings.Split(encoded, "-")
	current := "/"
	i := 1
	for i < len(parts) {
		bestJ := -1
		for j := i; j < len(parts); j++ {
			name := strings.Join(parts[i:j+1], "-")
			candidate := filepath.Join(current, name)
			info, err := os.Stat(candidate)
			if err == nil && info.IsDir() {
				bestJ = j
			}
		}
		if bestJ < 0 {
			return strings.ReplaceAll(encoded, "-", "/")
		}
		current = filepath.Join(current, strings.Join(parts[i:bestJ+1], "-"))
		i = bestJ + 1
	}
	return current
}

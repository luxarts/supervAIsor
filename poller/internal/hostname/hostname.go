// Package hostname resolves the local machine's hostname and applies macOS
// cleanup (strip trailing ".local") so cards read e.g. "my-mbp" not
// "my-mbp.local".
package hostname

import (
	"errors"
	"os"
	"strings"
)

// Resolve returns the cleaned hostname or an error if the OS call fails or
// returns an empty string.
func Resolve() (string, error) {
	h, err := os.Hostname()
	if err != nil {
		return "", err
	}
	h = Clean(h)
	if h == "" {
		return "", errors.New("hostname is empty")
	}
	return h, nil
}

// Clean strips a trailing ".local" suffix (macOS Bonjour adornment).
func Clean(h string) string {
	return strings.TrimSuffix(h, ".local")
}

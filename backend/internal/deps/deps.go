// Package deps holds blank imports for dependencies that are only used in
// generated/test code added in subsequent phases. Without these imports,
// `go mod tidy` would drop the deps from go.mod. This file will be removed
// once the real consumers land.
package deps

import (
	_ "github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite"
)

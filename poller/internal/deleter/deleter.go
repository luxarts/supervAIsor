package deleter

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
)

// Deleter removes a session's JSONL file from the host filesystem and
// prunes the offset state. Constructed once per process.
type Deleter struct {
	projectsDir string
	statePath   string
	offsets     *offsets.Store
}

func New(projectsDir, statePath string, off *offsets.Store) *Deleter {
	return &Deleter{projectsDir: projectsDir, statePath: statePath, offsets: off}
}

// Delete removes ~/.claude/projects/<projectDir>/<sessionID>.jsonl, then
// drops its entry from the offset store and persists. Refuses any path
// that resolves outside projectsDir.
func (d *Deleter) Delete(_ context.Context, sessionID, projectDir string) error {
	if sessionID == "" || projectDir == "" {
		return errors.New("empty sessionID or projectDir")
	}
	candidate := filepath.Join(d.projectsDir, projectDir, sessionID+".jsonl")
	resolved := filepath.Clean(candidate)
	root := filepath.Clean(d.projectsDir) + string(filepath.Separator)
	if !strings.HasPrefix(resolved, root) {
		return errors.New("path outside projects dir")
	}
	if err := os.Remove(resolved); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	d.offsets.Forget(resolved)
	if d.statePath != "" {
		_ = d.offsets.Save(d.statePath)
	}
	return nil
}

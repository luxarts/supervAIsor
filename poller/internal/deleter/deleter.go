package deleter

import (
	"context"
	"errors"
	"io/fs"
	"log"
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
		log.Printf("deleter: empty sessionID(%q) or projectDir(%q)", sessionID, projectDir)
		return errors.New("empty sessionID or projectDir")
	}
	candidate := filepath.Join(d.projectsDir, projectDir, sessionID+".jsonl")
	resolved := filepath.Clean(candidate)
	root := filepath.Clean(d.projectsDir) + string(filepath.Separator)
	log.Printf("deleter: target=%s (root=%s)", resolved, root)
	if !strings.HasPrefix(resolved, root) {
		log.Printf("deleter: REFUSED path outside projects dir: %s", resolved)
		return errors.New("path outside projects dir")
	}
	err := os.Remove(resolved)
	switch {
	case err == nil:
		log.Printf("deleter: removed %s", resolved)
	case errors.Is(err, fs.ErrNotExist):
		log.Printf("deleter: file already gone %s", resolved)
	default:
		log.Printf("deleter: os.Remove failed: %v", err)
		return err
	}
	d.offsets.Forget(resolved)
	if d.statePath != "" {
		if err := d.offsets.Save(d.statePath); err != nil {
			log.Printf("deleter: offsets.Save failed: %v", err)
		}
	}
	return nil
}

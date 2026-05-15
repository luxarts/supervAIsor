package deleter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeleter_RemovesFileAndForgetsOffset(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	off := &offsets.Store{}

	projectDir := "-Users-x-Projects-foo"
	sessionID := "abc"
	full := filepath.Join(root, projectDir, sessionID+".jsonl")
	writeFile(t, full, "{}\n")
	off.Set(full, 1, 1)

	d := New(root, statePath, off)
	if err := d.Delete(context.Background(), sessionID, projectDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("file still exists: err=%v", err)
	}
	if _, _, ok := off.Get(full); ok {
		t.Error("offset entry should have been forgotten")
	}
}

func TestDeleter_NoOpOnMissingFile(t *testing.T) {
	root := t.TempDir()
	d := New(root, filepath.Join(root, "state.json"), &offsets.Store{})
	err := d.Delete(context.Background(), "nope", "-x")
	if err != nil {
		t.Errorf("missing file should be no-op, got %v", err)
	}
}

func TestDeleter_RefusesPathEscape(t *testing.T) {
	root := t.TempDir()
	d := New(root, filepath.Join(root, "state.json"), &offsets.Store{})
	if err := d.Delete(context.Background(), "abc", "../../etc"); err == nil {
		t.Error("path escape via project_dir should be refused")
	}
	if err := d.Delete(context.Background(), "../../etc/passwd", "x"); err == nil {
		t.Error("path escape via session_id should be refused")
	}
}

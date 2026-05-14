package tailer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRead_OnlyNewLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	if err := os.WriteFile(p, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &Tailer{Path: p, Offset: 0}
	lines, newOff, err := tr.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Fatalf("first read got %v", lines)
	}
	if newOff == 0 {
		t.Errorf("offset still 0")
	}

	// Append more.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("line3\n")
	f.Close()

	tr2 := &Tailer{Path: p, Offset: newOff}
	lines2, _, err := tr2.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines2) != 1 || lines2[0] != "line3" {
		t.Errorf("delta read got %v", lines2)
	}

	_ = time.Now()
}

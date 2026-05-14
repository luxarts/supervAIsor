package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectPath_DashedName(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Projects", "ai-dream-team")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := encodePath(target)
	got := ResolveProjectPath(encoded)
	if got != target {
		t.Fatalf("ResolveProjectPath(%q) = %q, want %q", encoded, got, target)
	}
}

func TestResolveProjectPath_NoDashes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "plainproj")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := encodePath(target)
	got := ResolveProjectPath(encoded)
	if got != target {
		t.Fatalf("got %q want %q", got, target)
	}
}

func TestResolveProjectPath_FallbackWhenNothingExists(t *testing.T) {
	got := ResolveProjectPath("-nonexistent-path")
	want := "/nonexistent/path"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveProjectPath_Empty(t *testing.T) {
	if got := ResolveProjectPath(""); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func encodePath(p string) string {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			out = append(out, '-')
		} else {
			out = append(out, p[i])
		}
	}
	return string(out)
}

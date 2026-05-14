package offsets

import (
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("file-a", 1234, 100)
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}

	s2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	off, ino, ok := s2.Get("file-a")
	if !ok || off != 100 || ino != 1234 {
		t.Errorf("got off=%d ino=%d ok=%v", off, ino, ok)
	}
}

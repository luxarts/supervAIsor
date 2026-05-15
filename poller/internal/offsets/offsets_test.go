package offsets

import (
	"path/filepath"
	"testing"
)

func TestForget_RemovesEntry(t *testing.T) {
	s := &Store{m: map[string]fileOffset{
		"/a": {Inode: 1, Offset: 10},
		"/b": {Inode: 2, Offset: 20},
	}}
	s.Forget("/a")
	if _, _, ok := s.Get("/a"); ok {
		t.Error("/a should be forgotten")
	}
	if _, _, ok := s.Get("/b"); !ok {
		t.Error("/b should still be present")
	}
}

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

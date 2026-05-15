package offsets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type fileOffset struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

type Store struct {
	mu sync.Mutex
	m  map[string]fileOffset
}

func Load(path string) (*Store, error) {
	s := &Store{m: map[string]fileOffset{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.m); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Save(path string) error {
	s.mu.Lock()
	b, err := json.MarshalIndent(s.m, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Get(file string) (offset int64, inode uint64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[file]
	return v.Offset, v.Inode, ok
}

func (s *Store) Set(file string, inode uint64, offset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]fileOffset{}
	}
	s.m[file] = fileOffset{Inode: inode, Offset: offset}
}

// Forget removes the offset entry for the given file path. Caller is
// responsible for persisting via Save.
func (s *Store) Forget(file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, file)
}

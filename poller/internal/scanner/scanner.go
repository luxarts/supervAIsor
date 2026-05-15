package scanner

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
	"github.com/luxarts/supervaisor-poller/internal/tailer"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
)

// Envelope matches the backend's events.IngestEnvelope JSON shape.
// ProjectDir is the resolved filesystem path (e.g. /Users/x/Projects/foo);
// ProjectDirRaw is the on-disk dir name (e.g. -Users-x-Projects-foo) — the
// source of truth for filesystem operations like delete.
type Envelope struct {
	Hostname      string          `json:"hostname"`
	SessionID     string          `json:"session_id"`
	ProjectDir    string          `json:"project_dir"`
	ProjectDirRaw string          `json:"project_dir_raw,omitempty"`
	FileMTime     time.Time       `json:"file_mtime"`
	LineIndex     int             `json:"line_index"`
	Raw           json.RawMessage `json:"raw"`
}

// Scanner walks the Claude projects directory and ships new JSONL lines to the
// backend over a WebSocket connection.
type Scanner struct {
	Hostname    string
	ProjectsDir string
	Offsets     *offsets.Store
	OffsetsPath string
	Client      *wsclient.Client
	Interval    time.Duration
}

// RunOnce performs a single scan pass: walk all project dirs, tail each .jsonl
// file, and ship new lines. After each pass the offsets are persisted.
func (s *Scanner) RunOnce() error {
	entries, err := os.ReadDir(s.ProjectsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(s.ProjectsDir, e.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		resolvedProject := ResolveProjectPath(e.Name())
		rawProjectDir := e.Name()
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dirPath, f.Name())
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			if err := s.processFile(path, resolvedProject, rawProjectDir, sessionID); err != nil {
				log.Printf("process %s: %v", path, err)
			}
		}
	}
	return s.Offsets.Save(s.OffsetsPath)
}

// processFile tails a single .jsonl file, detects inode changes (rotation),
// and sends each valid JSON line as an Envelope to the backend. projectDir
// is the resolved filesystem path for display; rawProjectDir is the on-disk
// directory name used by the deleter to reconstruct the JSONL path.
func (s *Scanner) processFile(path, projectDir, rawProjectDir, sessionID string) error {
	prevOff, prevIno, _ := s.Offsets.Get(path)

	t := &tailer.Tailer{Path: path, Offset: prevOff}
	curIno, err := t.Inode()
	if err != nil {
		return err
	}
	if prevIno != 0 && curIno != prevIno {
		// File was rotated / replaced — restart from the beginning.
		t.Offset = 0
	}

	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines, newOff, err := t.Read()
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}

	for i, line := range lines {
		env := Envelope{
			Hostname:      s.Hostname,
			SessionID:     sessionID,
			ProjectDir:    projectDir,
			ProjectDirRaw: rawProjectDir,
			FileMTime:     fi.ModTime().UTC(),
			LineIndex:     int(prevOff) + i, // approximate line index
			Raw:           json.RawMessage(line),
		}
		if !json.Valid(env.Raw) {
			continue
		}
		if err := s.Client.Send(env); err != nil {
			// Broken connection — caller will force reconnect on next loop.
			s.Client.Close()
			return err
		}
	}
	s.Offsets.Set(path, curIno, newOff)
	return nil
}

// Run is the main poll loop. It ensures the WS client is connected before each
// scan and sleeps Interval between scans. It returns when stop is closed.
func (s *Scanner) Run(stop <-chan struct{}) {
	if s.Interval == 0 {
		s.Interval = time.Second
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		s.Client.EnsureConnected(stop)
		if err := s.RunOnce(); err != nil {
			log.Printf("scan: %v", err)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

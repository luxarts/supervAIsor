package tailer

import (
	"bufio"
	"errors"
	"io"
	"os"
	"syscall"
)

type Tailer struct {
	Path   string
	Offset int64
}

// Inode returns the current inode of the file, or 0 if unknown.
func (t *Tailer) Inode() (uint64, error) {
	fi, err := os.Stat(t.Path)
	if err != nil {
		return 0, err
	}
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("stat: not a unix file")
	}
	return uint64(sys.Ino), nil
}

// Read returns the lines added since t.Offset and the new offset.
// If the file is smaller than t.Offset (rotation), it reads from the start.
func (t *Tailer) Read() ([]string, int64, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return nil, t.Offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, t.Offset, err
	}
	size := fi.Size()

	start := t.Offset
	if start > size {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, t.Offset, err
	}

	var out []string
	br := bufio.NewReader(f)
	pos := start
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := line
			if line[len(line)-1] == '\n' {
				trimmed = line[:len(line)-1]
				out = append(out, trimmed)
				pos += int64(len(line))
			} else {
				// Partial line — stop, leave it for next read.
				break
			}
		}
		if err != nil {
			break
		}
	}
	return out, pos, nil
}

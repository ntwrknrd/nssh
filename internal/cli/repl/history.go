package repl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ntwrknrd/nssh/internal/config"
)

const (
	maxHistoryEntries = 1000
	maxHistoryBytes   = 1 << 20
)

type historyStore struct{ path string }

func defaultHistoryStore() historyStore {
	return historyStore{path: filepath.Join(config.DefaultPaths().StateDir, "repl_history")}
}

func (h historyStore) load() ([]string, error) {
	if h.path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0600); err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return nil, err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	entries, err := readHistory(f)
	if err != nil {
		return nil, err
	}
	return boundedHistory(entries), nil
}

func (h historyStore) append(line string) error {
	line = strings.TrimSpace(line)
	if line == "" || strings.Contains(line, "\n") {
		return nil
	}
	if len(line)+1 > maxHistoryBytes {
		return fmt.Errorf("submission exceeds history limit of 1 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	entries, err := readHistory(f)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries = boundedHistory(append(entries, line))
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	_, err = f.WriteString(strings.Join(entries, "\n") + "\n")
	return err
}

func readHistory(f *os.File) ([]string, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := info.Size() - maxHistoryBytes
	if offset < 0 {
		offset = 0
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxHistoryBytes))
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if i := strings.IndexByte(string(data), '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			return nil, nil
		}
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), nil
}

func boundedHistory(entries []string) []string {
	out := make([]string, 0, len(entries))
	size := 0
	for i := len(entries) - 1; i >= 0 && len(out) < maxHistoryEntries; i-- {
		entry := strings.TrimSpace(entries[i])
		if entry == "" || strings.Contains(entry, "\n") {
			continue
		}
		if len(entry)+1 > maxHistoryBytes {
			continue
		}
		if size+len(entry)+1 > maxHistoryBytes {
			break
		}
		out = append(out, entry)
		size += len(entry) + 1
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

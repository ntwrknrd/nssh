package repl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

const DefaultHistoryLimit = 1000

type FileHistoryStore struct {
	Path  string
	Limit int
}

func DefaultHistoryStore() *FileHistoryStore {
	return &FileHistoryStore{
		Path:  filepath.Join(config.DefaultPaths().StateDir, "repl_history"),
		Limit: DefaultHistoryLimit,
	}
}

func (s *FileHistoryStore) Load() ([]string, error) {
	if s == nil || strings.TrimSpace(s.Path) == "" {
		return nil, nil
	}
	f, err := os.Open(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open repl history: %w", err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read repl history: %w", err)
	}
	if limit := s.limit(); limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

func (s *FileHistoryStore) Append(line string) error {
	if s == nil || strings.TrimSpace(s.Path) == "" {
		return nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return fmt.Errorf("create repl history dir: %w", err)
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open repl history: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("append repl history: %w", err)
	}
	return nil
}

func (s *FileHistoryStore) limit() int {
	if s == nil || s.Limit <= 0 {
		return DefaultHistoryLimit
	}
	return s.Limit
}

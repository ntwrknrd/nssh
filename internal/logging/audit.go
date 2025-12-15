package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

const (
	defaultMaxAuditSize = 10 * 1024 * 1024 // 10 MB
	maxRotatedFiles     = 3                // Keep audit.log.1, .2, .3
)

// AuditLogger wraps slog with dual output: stderr and audit file.
type AuditLogger struct {
	*slog.Logger
	auditFile *os.File
}

// NewAuditLogger creates a logger that writes to both stderr and audit file.
// stderrLevel controls stderr verbosity; audit file always logs Info+.
// Performs rotation check on startup if audit file exceeds MaxFileSize.
func NewAuditLogger(stderrLevel slog.Level, audit *config.AuditConfig, stateDir string) (*AuditLogger, error) {
	handlers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: stderrLevel}),
	}

	var auditFile *os.File
	if audit.Enabled {
		auditPath := filepath.Join(stateDir, "audit.log")
		if err := os.MkdirAll(filepath.Dir(auditPath), 0700); err != nil {
			return nil, err
		}

		// Parse and check max size for rotation
		maxSize, err := ParseMaxSize(audit.MaxSize)
		if err != nil {
			maxSize = defaultMaxAuditSize
		}
		if err := rotateIfNeeded(auditPath, maxSize); err != nil {
			return nil, fmt.Errorf("rotate audit log: %w", err)
		}

		auditFile, err = os.OpenFile(auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}

		handlers = append(handlers, slog.NewTextHandler(auditFile, &slog.HandlerOptions{
			Level: slog.LevelInfo, // Audit always logs Info+
		}))
	}

	return &AuditLogger{
		Logger:    slog.New(&multiHandler{handlers: handlers}),
		auditFile: auditFile,
	}, nil
}

// rotateIfNeeded checks if the audit log exceeds maxSize and rotates if so.
// Rotation: audit.log -> audit.log.1 -> audit.log.2 -> audit.log.3 (deleted)
func rotateIfNeeded(path string, maxSize int64) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil // No file to rotate
	}
	if err != nil {
		return err
	}
	if info.Size() < maxSize {
		return nil // Under limit
	}

	// Delete oldest rotated file
	oldestPath := fmt.Sprintf("%s.%d", path, maxRotatedFiles)
	_ = os.Remove(oldestPath)

	// Shift existing rotated files: .2 -> .3, .1 -> .2
	for i := maxRotatedFiles - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", path, i)
		newPath := fmt.Sprintf("%s.%d", path, i+1)
		_ = os.Rename(oldPath, newPath) // Ignore errors (file may not exist)
	}

	// Rotate current file to .1
	return os.Rename(path, path+".1")
}

// Close closes the audit file if open.
func (a *AuditLogger) Close() error {
	if a.auditFile != nil {
		return a.auditFile.Close()
	}
	return nil
}

// multiHandler fans out log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

// ParseMaxSize parses a size string like "10MB" into bytes.
func ParseMaxSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return defaultMaxAuditSize, nil
	}

	var multiplier int64 = 1
	switch {
	case strings.HasSuffix(s, "MB"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "GB"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}

	val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}

	return val * multiplier, nil
}

// ListAuditLogs returns all audit log files in the state directory.
func ListAuditLogs(stateDir string) ([]string, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var logs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "audit.log") {
			logs = append(logs, filepath.Join(stateDir, e.Name()))
		}
	}

	// Sort with main log first, then rotated files in order
	sort.Slice(logs, func(i, j int) bool {
		// audit.log comes first
		if filepath.Base(logs[i]) == "audit.log" {
			return true
		}
		if filepath.Base(logs[j]) == "audit.log" {
			return false
		}
		return logs[i] < logs[j]
	})

	return logs, nil
}

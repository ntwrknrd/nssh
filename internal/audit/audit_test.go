package audit

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestParseMaxSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "", want: defaultMaxAuditSize},
		{input: "512", want: 512},
		{input: "2KB", want: 2 * 1024},
		{input: "3MB", want: 3 * 1024 * 1024},
		{input: "4GB", want: 4 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		got, err := ParseMaxSize(tt.input)
		if err != nil {
			t.Fatalf("ParseMaxSize(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("ParseMaxSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNewLoggerWritesAuditFile(t *testing.T) {
	stateDir := t.TempDir()
	logger, err := NewLogger(slog.LevelError, &config.AuditConfig{
		Enabled: true,
		MaxSize: "10MB",
	}, stateDir)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	logger.Info("ssh_connect_start", "host", "edge01")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(stateDir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), "ssh_connect_start") || !strings.Contains(string(data), "edge01") {
		t.Fatalf("audit log did not contain expected event: %q", string(data))
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainEntrypointStaysThin(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "cmd", "nssh", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"github.com/ntwrknrd/nssh/internal/connect",
		"github.com/ntwrknrd/nssh/internal/inventory",
		"github.com/ntwrknrd/nssh/internal/ssh/connector",
		"github.com/ntwrknrd/nssh/internal/ssh/compat",
		"github.com/ntwrknrd/nssh/internal/ssh/sshconfig",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("cmd/nssh/main.go imports forbidden runtime package %s", forbidden)
		}
	}
}

func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			panic("repo root not found")
		}
		wd = parent
	}
}

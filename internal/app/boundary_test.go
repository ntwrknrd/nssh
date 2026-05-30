package app

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestMainEntrypointStaysThin(t *testing.T) {
	mainPath := filepath.Join(repoRoot(), "cmd", "nssh", "main.go")
	file, err := parser.ParseFile(token.NewFileSet(), mainPath, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	forbidden := map[string]bool{
		"github.com/ntwrknrd/nssh/internal/connect":       true,
		"github.com/ntwrknrd/nssh/internal/inventory":     true,
		"github.com/ntwrknrd/nssh/internal/ssh/connector": true,
		"github.com/ntwrknrd/nssh/internal/ssh/compat":    true,
		"github.com/ntwrknrd/nssh/internal/ssh/sshconfig": true,
	}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", imp.Path.Value, err)
		}
		if forbidden[path] {
			t.Fatalf("cmd/nssh/main.go imports forbidden runtime package %s", path)
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

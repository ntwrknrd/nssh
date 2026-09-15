package app

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ntwrknrd/nssh/internal/connect"
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

func TestRootVerbosityLadder(t *testing.T) {
	old := connectRequestFunc
	defer func() { connectRequestFunc = old }()
	var got connect.Request
	connectRequestFunc = func(_ context.Context, req connect.Request) error { got = req; return nil }
	for _, args := range [][]string{{"-vvv", "edge01"}, {"-v", "-v", "-v", "edge01"}, {"-vvvJjump", "edge01"}} {
		if err := execute(Options{}, args); err != nil {
			t.Fatal(err)
		}
		if got.Options.Verbosity != 3 || got.Options.SSHVerbosity != 2 {
			t.Fatalf("verbosity: %+v", got.Options)
		}
	}
	if err := execute(Options{}, []string{"edge01"}); err != nil {
		t.Fatal(err)
	}
	if got.Options.Verbosity != 0 || got.Options.SSHVerbosity != 0 {
		t.Fatalf("verbosity leaked: %+v", got.Options)
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

package app

import (
	"os"
	"os/exec"
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

func TestRemovedInternalPackagesStayRemoved(t *testing.T) {
	root := repoRoot()
	for _, path := range []string{
		filepath.Join(root, "internal", "ssh", "recording"),
		filepath.Join(root, "internal", "logging"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("removed package path still exists: %s", path)
		}
	}
}

func TestNoImportsOfRemovedInternalPackages(t *testing.T) {
	removedRecordingImport := "github.com/ntwrknrd/nssh/internal/ssh/" + "recording"
	removedLoggingImport := "github.com/ntwrknrd/nssh/internal/" + "logging"
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}|{{join .Imports \",\"}}", "./...")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		imports := strings.Split(parts[1], ",")
		for _, imp := range imports {
			switch strings.TrimSpace(imp) {
			case removedRecordingImport, removedLoggingImport:
				t.Fatalf("package %s imports removed package %s", parts[0], imp)
			}
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

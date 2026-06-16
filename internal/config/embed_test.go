package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExampleConfigUsesDocsSource(t *testing.T) {
	// Find project root from test file location
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get test file path")
	}
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	docsPath := filepath.Join(projectRoot, "docs", "examples", "config", "config.example.yaml")
	docsContent, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read docs config: %v", err)
	}

	packageCopy := filepath.Join(projectRoot, "internal", "config", "example_config.yaml")
	if _, err := os.Stat(packageCopy); err == nil {
		t.Fatalf("internal package config copy exists: %s", packageCopy)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat package config copy: %v", err)
	}

	if ExampleConfig != string(docsContent) {
		t.Errorf("embedded config does not match docs/examples/config/config.example.yaml")
	}
}

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExampleConfigInSync(t *testing.T) {
	// Find project root from test file location
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get test file path")
	}
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	docsPath := filepath.Join(projectRoot, "docs", "examples", "config", "config.example.toml")
	docsContent, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read docs config: %v", err)
	}

	if ExampleConfig != string(docsContent) {
		t.Errorf("embedded config out of sync with docs/examples/config/config.example.toml\n" +
			"Run: cp docs/examples/config/config.example.toml internal/config/example_config.toml")
	}
}

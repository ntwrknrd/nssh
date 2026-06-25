package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExampleConfigUsesInternalSource(t *testing.T) {
	// Find project root from test file location
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get test file path")
	}
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	templatePath := filepath.Join(projectRoot, "internal", "config", "example_config.yaml")
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read internal config template: %v", err)
	}

	docsCopy := filepath.Join(projectRoot, "docs", "examples", "config", "config.example.yaml")
	if _, err := os.Stat(docsCopy); err == nil {
		t.Fatalf("docs config copy exists: %s", docsCopy)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat docs config copy: %v", err)
	}

	if ExampleConfig != string(templateContent) {
		t.Errorf("embedded config does not match internal/config/example_config.yaml")
	}
}

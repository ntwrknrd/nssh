package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPathsUsesConfigYAML(t *testing.T) {
	paths := resolvePaths()
	if !strings.HasSuffix(paths.ConfigFile, filepath.Join("nssh", "config.yaml")) {
		t.Fatalf("ConfigFile = %q, want config.yaml", paths.ConfigFile)
	}
}

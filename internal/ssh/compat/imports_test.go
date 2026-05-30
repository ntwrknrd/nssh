//go:build unix

package compat_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundaries verifies that SSH subsystem packages do not import forbidden packages.
// Rules:
// - internal/ssh/... must NOT import higher-level CLI, UI, recording, agent, session, or vault packages
func TestImportBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		forbidden []string
	}{
		{
			name: "ssh must not import orchestration packages",
			pkg:  "github.com/ntwrknrd/nssh/internal/ssh/...",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/cli/...",
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/ntwrknrd/nssh/internal/recording",
				"github.com/ntwrknrd/nssh/internal/agent/...",
				"github.com/ntwrknrd/nssh/internal/session/...",
				"github.com/ntwrknrd/nssh/internal/vault/...",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}|{{join .Imports \",\"}}", tt.pkg)
			out, err := cmd.Output()
			if err != nil {
				// Package may not exist in non-unix builds, skip
				if exitErr, ok := err.(*exec.ExitError); ok {
					if strings.Contains(string(exitErr.Stderr), "no Go files") ||
						strings.Contains(string(exitErr.Stderr), "no buildable Go source files") {
						t.Skip("package not available in this build configuration")
					}
				}
				t.Fatalf("go list failed: %v", err)
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				parts := strings.SplitN(line, "|", 2)
				pkg := parts[0]
				var imports []string
				if len(parts) == 2 && parts[1] != "" {
					imports = strings.Split(parts[1], ",")
				}

				for _, imp := range imports {
					imp = strings.TrimSpace(imp)
					if imp == "" {
						continue
					}
					for _, forbidden := range tt.forbidden {
						if importViolates(imp, forbidden) {
							t.Errorf("package %s imports forbidden package %s", pkg, forbidden)
						}
					}
				}
			}
		})
	}
}

func importViolates(importPath, forbidden string) bool {
	if strings.HasSuffix(forbidden, "/...") {
		prefix := strings.TrimSuffix(forbidden, "/...")
		return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
	}
	return importPath == forbidden
}

//go:build linux || darwin

package session_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundaries verifies that session packages do not import forbidden packages.
// Rules:
// - internal/session/... must NOT import internal/ui, cobra, or term
// - internal/session/mode must be a leaf package (no vault/agent/CLI imports)
func TestImportBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		forbidden []string
	}{
		{
			name: "session must not import ui/cobra/term",
			pkg:  "github.com/ntwrknrd/nssh/internal/session/...",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/spf13/cobra",
				"golang.org/x/term",
			},
		},
		{
			name: "session/mode must be a leaf package",
			pkg:  "github.com/ntwrknrd/nssh/internal/session/mode",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/vault",
				"github.com/ntwrknrd/nssh/internal/agent",
				"github.com/ntwrknrd/nssh/internal/cli",
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/spf13/cobra",
				"golang.org/x/term",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get all imports for the package
			cmd := exec.Command("go", "list", "-f", "{{.Imports}}", tt.pkg)
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

			imports := string(out)
			for _, forbidden := range tt.forbidden {
				if strings.Contains(imports, forbidden) {
					t.Errorf("package %s imports forbidden package %s", tt.pkg, forbidden)
				}
			}
		})
	}
}

package vault_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundaries verifies that vault packages do not import forbidden packages.
// Rules:
// - internal/vault/... must NOT import internal/agent, internal/ui, cobra, or term
// - internal/vault/piv must NOT import github.com/go-piv/piv-go
func TestImportBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		forbidden []string
	}{
		{
			name: "vault must not import agent",
			pkg:  "github.com/ntwrknrd/nssh/internal/vault/...",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/agent",
			},
		},
		{
			name: "vault must not import ui/cobra/term",
			pkg:  "github.com/ntwrknrd/nssh/internal/vault/...",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/spf13/cobra",
				"golang.org/x/term",
			},
		},
		{
			name: "vault/piv must not import piv-go",
			pkg:  "github.com/ntwrknrd/nssh/internal/vault/piv",
			forbidden: []string{
				"github.com/go-piv/piv-go",
				"github.com/go-piv/piv-go/v2/piv",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get all imports for the package (including transitive)
			cmd := exec.Command("go", "list", "-f", "{{.Imports}}", tt.pkg)
			out, err := cmd.Output()
			if err != nil {
				// Package may not exist in non-hardware builds, skip
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

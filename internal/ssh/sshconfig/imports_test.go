package sshconfig_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundaries verifies SSH subsystem import layering.
//
// Rules:
// - compat/sshconfig must NOT import ui/cobra/term
// - recording must NOT import ui/cobra/term/connector
// - sshconfig must NOT import the connector package (it should depend on compat only)
func TestImportBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		forbidden []string
	}{
		{
			name: "ssh/compat must stay leaf",
			pkg:  "github.com/ntwrknrd/nssh/internal/ssh/compat",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/spf13/cobra",
				"golang.org/x/term",
				"github.com/ntwrknrd/nssh/internal/ssh/connector",
			},
		},
		{
			name: "recording must stay non-interactive",
			pkg:  "github.com/ntwrknrd/nssh/internal/recording",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/spf13/cobra",
				"golang.org/x/term",
				"github.com/ntwrknrd/nssh/internal/ssh/connector",
			},
		},
		{
			name: "ssh/sshconfig must not import connector",
			pkg:  "github.com/ntwrknrd/nssh/internal/ssh/sshconfig",
			forbidden: []string{
				"github.com/ntwrknrd/nssh/internal/ui",
				"github.com/spf13/cobra",
				"golang.org/x/term",
				"github.com/ntwrknrd/nssh/internal/ssh/connector",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-f", "{{.Imports}}", tt.pkg)
			out, err := cmd.Output()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					stderr := string(exitErr.Stderr)
					if strings.Contains(stderr, "no Go files") ||
						strings.Contains(stderr, "no buildable Go source files") {
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

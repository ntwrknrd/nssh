package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestPreprocessArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		out  []string
	}{
		{
			name: "simple host connects through smart-connect",
			in:   []string{"router1"},
			out:  []string{"smart-connect", "router1"},
		},
		{
			name: "known subcommand passes through",
			in:   []string{"connect", "router1"},
			out:  []string{"connect", "router1"},
		},
		{
			name: "verbose flag before host",
			in:   []string{"-v", "router1"},
			out:  []string{"-v", "smart-connect", "router1"},
		},
		{
			name: "ssh flag with value before host (-o)",
			in:   []string{"-o", "StrictHostKeyChecking=no", "router1"},
			out:  []string{"smart-connect", "router1", "-o", "StrictHostKeyChecking=no"},
		},
		{
			name: "ssh flag with value before host (-p)",
			in:   []string{"-p", "2200", "router1"},
			out:  []string{"smart-connect", "router1", "-p", "2200"},
		},
		{
			name: "ssh flag with value after host",
			in:   []string{"router1", "-p", "2200"},
			out:  []string{"smart-connect", "router1", "-p", "2200"},
		},
		{
			name: "ssh boolean flags before host",
			in:   []string{"-4", "-A", "router1"},
			out:  []string{"smart-connect", "router1", "-4", "-A"},
		},
		{
			name: "multiple SSH flags",
			in:   []string{"-p", "2222", "-l", "admin", "somehost"},
			out:  []string{"smart-connect", "somehost", "-p", "2222", "-l", "admin"},
		},
		{
			name: "mixed global and SSH flags",
			in:   []string{"-v", "-4", "-p", "2222", "somehost", "-l", "root"},
			out:  []string{"-v", "smart-connect", "somehost", "-4", "-p", "2222", "-l", "root"},
		},
		{
			name: "global flag only",
			in:   []string{"-v", "--help"},
			out:  []string{"-v", "--help"},
		},
		{
			name: "empty args",
			in:   []string{},
			out:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PreprocessArgs(tt.in)
			if !reflect.DeepEqual(got, tt.out) {
				t.Fatalf("PreprocessArgs(%v) = %v, want %v", tt.in, got, tt.out)
			}
		})
	}
}

func TestRootCommandRegistersPublicCommands(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})

	for _, name := range []string{"agent", "inv", "connect", "cp", "self"} {
		if cmd, _, err := root.Find([]string{name}); err != nil || cmd == root || cmd.Name() != name {
			t.Fatalf("expected public command %q, got cmd=%v err=%v", name, cmd, err)
		}
	}
}

func TestSelfHelpDoesNotTruncate(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	for _, path := range [][]string{
		{"self"},
		{"self", "reinstall"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("find %v: %v", path, err)
		}
		help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
		if strings.Contains(help, "...") {
			t.Fatalf("%s help should not be truncated:\n%s", strings.Join(path, " "), help)
		}
	}
}

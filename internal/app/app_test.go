package app

import (
	"reflect"
	"slices"
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
			in:   []string{"inv", "list"},
			out:  []string{"inv", "list"},
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
			name: "post host flag is remote command",
			in:   []string{"router1", "-p", "2200"},
			out:  []string{"smart-connect", "router1", "--", "-p", "2200"},
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
			out:  []string{"-v", "smart-connect", "somehost", "-4", "-p", "2222", "--", "-l", "root"},
		},
		{
			name: "remote command after host",
			in:   []string{"router1", "show", "version"},
			out:  []string{"smart-connect", "router1", "--", "show", "version"},
		},
		{
			name: "ssh options before host and remote command after host",
			in:   []string{"-p", "2222", "router1", "show", "version"},
			out:  []string{"smart-connect", "router1", "-p", "2222", "--", "show", "version"},
		},
		{
			name: "select opens smart picker",
			in:   []string{"--select"},
			out:  []string{"smart-connect"},
		},
		{
			name: "literal target bypasses subcommand parsing",
			in:   []string{"--target", "log"},
			out:  []string{"smart-connect", "--literal-target", "log"},
		},
		{
			name: "literal target carries remote command",
			in:   []string{"--target", "log", "show", "version"},
			out:  []string{"smart-connect", "--literal-target", "log", "--", "show", "version"},
		},
		{
			name: "ssh option before literal target",
			in:   []string{"-p", "2222", "--target", "log", "show", "version"},
			out:  []string{"smart-connect", "--literal-target", "log", "-p", "2222", "--", "show", "version"},
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

	var got []string
	for _, cmd := range root.Commands() {
		if !cmd.Hidden {
			got = append(got, cmd.Name())
		}
	}
	want := []string{"agent", "cp", "inv", "log", "self"}
	if !slices.Equal(got, want) {
		t.Fatalf("public commands = %v, want %v", got, want)
	}
}

func TestAgentRestartRejectedByRootCommand(t *testing.T) {
	err := execute(Options{Version: "test"}, []string{"agent", "restart"})
	if err == nil {
		t.Fatal("agent restart succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), `unknown command "restart"`) {
		t.Fatalf("agent restart error = %v, want unknown command", err)
	}
}

func TestLogArchiveCommandIsRegisteredWithoutOpportunisticFlag(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	cmd, _, err := root.Find([]string{"log", "archive"})
	if err != nil {
		t.Fatalf("find log archive: %v", err)
	}
	if cmd.Name() != "archive" {
		t.Fatalf("log archive command name = %q", cmd.Name())
	}
	if flag := cmd.Flags().Lookup("opportunistic"); flag != nil {
		t.Fatal("log archive must not expose --opportunistic")
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

func TestSelfBenchRegistersOnlyConnectionBenchmarks(t *testing.T) {
	root := NewRootCmd(Options{Version: "test"})
	cmd, _, err := root.Find([]string{"self", "bench"})
	if err != nil {
		t.Fatalf("find self bench: %v", err)
	}

	var got []string
	for _, subcmd := range cmd.Commands() {
		if !subcmd.Hidden {
			got = append(got, subcmd.Name())
		}
	}
	want := []string{"scp", "ssh"}
	if !slices.Equal(got, want) {
		t.Fatalf("self bench commands = %v, want %v", got, want)
	}
}

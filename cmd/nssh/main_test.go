package main

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
			name: "simple host routes through smart-connect",
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
			// SSH flags are moved AFTER hostname to avoid Cobra parsing them at root level
			name: "ssh flag with value before host (-o)",
			in:   []string{"-o", "StrictHostKeyChecking=no", "router1"},
			out:  []string{"smart-connect", "router1", "-o", "StrictHostKeyChecking=no"},
		},
		{
			// SSH flags are moved AFTER hostname to avoid Cobra parsing them at root level
			name: "ssh flag with value before host (-p)",
			in:   []string{"-p", "2200", "router1"},
			out:  []string{"smart-connect", "router1", "-p", "2200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preprocessArgs(tt.in)
			if !reflect.DeepEqual(got, tt.out) {
				t.Fatalf("preprocessArgs(%v) = %v, want %v", tt.in, got, tt.out)
			}
		})
	}
}

func TestRootCommandCutover(t *testing.T) {
	root := newRootCmd()

	for _, name := range []string{"inv", "cred", "connect", "cp", "self", "lock", "unlock"} {
		if cmd, _, err := root.Find([]string{name}); err != nil || cmd == root || cmd.Name() != name {
			t.Fatalf("expected public command %q, got cmd=%v err=%v", name, cmd, err)
		}
	}

	for _, removed := range []string{"host", "ctx", "sync"} {
		if cmd, _, err := root.Find([]string{removed}); err == nil && cmd != root && cmd.Name() == removed {
			t.Fatalf("removed command %q is still registered", removed)
		}
	}
}

func TestInvListHelpShowsSelectionFlags(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"inv", "list"})
	if err != nil {
		t.Fatalf("find inv list: %v", err)
	}

	help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	for _, want := range []string{
		"-g, --groups",
		"-s, --select STRING",
		"filter by text or field:value",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}

	for _, line := range strings.Split(help, "\n") {
		if strings.Contains(line, "--select") && strings.Contains(line, "...") {
			t.Fatalf("select help is truncated:\n%s", help)
		}
	}
}

func TestInvGetHelpShowsGroupShorthandAndExplicitUsage(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"inv", "get"})
	if err != nil {
		t.Fatalf("find inv get: %v", err)
	}

	help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	for _, want := range []string{
		"nssh inv get HOST",
		"nssh inv get -g GROUP",
		"-g, --group",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "nssh inv get NAME [flags]") {
		t.Fatalf("help still has vague [flags] usage:\n%s", help)
	}
}

func TestInvSetHelpShowsGroupModeAndExplicitUsage(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"inv", "set"})
	if err != nil {
		t.Fatalf("find inv set: %v", err)
	}

	help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	for _, want := range []string{
		"nssh inv set HOST",
		"nssh inv set -g GROUP",
		"nssh inv set HOST -g GROUP",
		"-g, --group STRING",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "nssh inv set HOST [flags]") {
		t.Fatalf("help still has vague [flags] usage:\n%s", help)
	}
}

func TestCredHelpShowsPositionalHostAndGroupShorthand(t *testing.T) {
	root := newRootCmd()
	cases := []struct {
		path     []string
		required []string
		rejected []string
	}{
		{
			path: []string{"cred", "get"},
			required: []string{
				"nssh cred get HOST",
				"nssh cred get -g GROUP",
				"-g, --group STRING",
				"-r, --reveal-secret",
			},
			rejected: []string{"--host", "--show-secret", "..."},
		},
		{
			path: []string{"cred", "set"},
			required: []string{
				"nssh cred set HOST",
				"nssh cred set -g GROUP",
				"-g, --group STRING",
				"--username STRING",
			},
			rejected: []string{"--host", "..."},
		},
		{
			path: []string{"cred", "rm"},
			required: []string{
				"nssh cred rm HOST",
				"nssh cred rm -g GROUP",
				"-g, --group STRING",
			},
			rejected: []string{"--host", "..."},
		},
	}

	for _, tc := range cases {
		t.Run(strings.Join(tc.path, " "), func(t *testing.T) {
			cmd, _, err := root.Find(tc.path)
			if err != nil {
				t.Fatalf("find %v: %v", tc.path, err)
			}
			help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
			for _, want := range tc.required {
				if !strings.Contains(help, want) {
					t.Fatalf("help missing %q:\n%s", want, help)
				}
			}
			for _, reject := range tc.rejected {
				if strings.Contains(help, reject) {
					t.Fatalf("help contains %q:\n%s", reject, help)
				}
			}
		})
	}
}

func TestSelfUpgradeHiddenButCallable(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"self", "upgrade"})
	if err != nil {
		t.Fatalf("find self upgrade: %v", err)
	}
	if cmd == nil || cmd.Name() != "upgrade" {
		t.Fatalf("self upgrade command = %v", cmd)
	}
	if !cmd.Hidden {
		t.Fatal("self upgrade should be hidden")
	}

	selfCmd, _, err := root.Find([]string{"self"})
	if err != nil {
		t.Fatalf("find self: %v", err)
	}
	help := ui.RenderStyledHelp(selfCmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	if strings.Contains(help, "upgrade") {
		t.Fatalf("self help exposes hidden upgrade command:\n%s", help)
	}
}

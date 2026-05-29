package main

import (
	"os"
	"path/filepath"
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

func TestInternalSyncPackageRemoved(t *testing.T) {
	root := repoRoot()
	legacyImport := "github.com/ntwrknrd/nssh/internal/" + "sync"
	if _, err := os.Stat(filepath.Join(root, "internal", "sync")); !os.IsNotExist(err) {
		t.Fatalf("internal/sync should be removed after inventory.provider superseded sync.sources")
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), legacyImport) {
			t.Fatalf("%s still imports internal/sync", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInternalCLIResolvePackageRemoved(t *testing.T) {
	root := repoRoot()
	legacyImport := "github.com/ntwrknrd/nssh/internal/cli/" + "resolve"
	if _, err := os.Stat(filepath.Join(root, "internal", "cli", "resolve")); !os.IsNotExist(err) {
		t.Fatalf("internal/cli/resolve should move to internal/connect")
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), legacyImport) {
			t.Fatalf("%s still imports internal/cli/resolve", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRootCommandCutover(t *testing.T) {
	root := newRootCmd()

	for _, name := range []string{"agent", "inv", "connect", "cp", "self"} {
		if cmd, _, err := root.Find([]string{name}); err != nil || cmd == root || cmd.Name() != name {
			t.Fatalf("expected public command %q, got cmd=%v err=%v", name, cmd, err)
		}
	}

	for _, removed := range []string{"host", "ctx", "sync", "cred", "lock", "unlock"} {
		if cmd, _, err := root.Find([]string{removed}); err == nil && cmd != root && cmd.Name() == removed {
			t.Fatalf("removed command %q is still registered", removed)
		}
	}
}

func TestHardwareKeySurfacesAreRemoved(t *testing.T) {
	root := newRootCmd()

	if cmd, _, err := root.Find([]string{"self", "piv"}); err == nil && cmd.Name() == "piv" {
		t.Fatal("self piv command should not be registered")
	}

	reinstall, _, err := root.Find([]string{"self", "reinstall"})
	if err != nil {
		t.Fatalf("find self reinstall: %v", err)
	}
	if reinstall.Flags().Lookup("hardware") != nil {
		t.Fatal("self reinstall should not expose --hardware")
	}
	if reinstall.Flags().Lookup("release") == nil {
		t.Fatal("self reinstall should expose --release")
	}

	if cmd, _, err := root.Find([]string{"self", "rekey"}); err == nil && cmd.Name() == "rekey" {
		t.Fatal("self rekey should not be registered after age vault removal")
	}
}

func TestBuildAndInstallSurfacesDoNotMentionHardwareOrCGO(t *testing.T) {
	for _, file := range []string{"Makefile", filepath.Join("scripts", "install.sh")} {
		data, err := os.ReadFile(filepath.Join(repoRoot(), file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := strings.ToLower(string(data))
		for _, reject := range []string{"hardware", "cgo", "piv"} {
			if strings.Contains(text, reject) {
				t.Fatalf("%s should not mention %q after hardware-key support removal", file, reject)
			}
		}
	}
}

func TestSelfHelpDoesNotTruncate(t *testing.T) {
	root := newRootCmd()
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

func TestSelfHelpUsesApprovedCommandDescriptions(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"self"})
	if err != nil {
		t.Fatalf("find self: %v", err)
	}

	help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	for _, want := range []string{
		"bench                             Performance benchmarking",
		"cfg                               Manage configuration",
		"init                              Initialize configuration",
		"reset                             Reset configuration",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("self help missing %q:\n%s", want, help)
		}
	}
}

func TestSelfReinstallHelpSaysGitHubPackage(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"self", "reinstall"})
	if err != nil {
		t.Fatalf("find self reinstall: %v", err)
	}

	help := ui.RenderStyledHelp(cmd, ui.StyledHelpConfig{ShowGlobalFlags: true, Width: 80})
	for _, want := range []string{
		"Install latest from GitHub",
		"--release STRING",
		"GitHub release tag",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
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
		"--credential-provider STRING",
		"--credential-ref STRING",
		"--credential-clear",
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

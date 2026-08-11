package self

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRelease(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"v0.3.0", "v0.3.0"},
		{"0.3.0", "v0.3.0"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := normalizeRelease(tt.in)
			if err != nil {
				t.Fatalf("normalizeRelease(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRelease(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeReleaseRejectsShellUnsafeInput(t *testing.T) {
	for _, in := range []string{"v0.3.0;echo nope", "v0.3.0$(echo nope)", "../v0.3.0"} {
		if got, err := normalizeRelease(in); err == nil {
			t.Fatalf("normalizeRelease(%q) = %q, want error", in, got)
		}
	}
}

func TestInstallShellCommandTargetsRelease(t *testing.T) {
	got, err := installShellCommand("0.3.0")
	if err != nil {
		t.Fatalf("installShellCommand error = %v", err)
	}

	script := localInstallScript()
	if script == "" {
		t.Fatal("expected local install script")
	}
	want := "sh " + shellQuote(script) + " --events --release v0.3.0"
	if got != want {
		t.Fatalf("installShellCommand = %q, want %q", got, want)
	}
}

func TestInstallShellCommandUsesRemoteScriptOutsideProject(t *testing.T) {
	temp := t.TempDir()
	previousWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(temp); err != nil {
		t.Fatal(err)
	}

	got, err := installShellCommand("0.3.0")
	if err != nil {
		t.Fatalf("installShellCommand error = %v", err)
	}

	want := "curl -fsSL https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh | sh -s -- --events --release v0.3.0"
	if got != want {
		t.Fatalf("installShellCommand = %q, want %q", got, want)
	}
}

func TestParseInstallEvent(t *testing.T) {
	tests := []struct {
		line string
		kind string
		data string
		ok   bool
	}{
		{"NSSH_INSTALL_STATUS\tDownloading archive", "status", "Downloading archive", true},
		{"NSSH_INSTALL_VERSION\tv0.2.4", "version", "v0.2.4", true},
		{"NSSH_INSTALL_PATH\t/Users/cj/.local/bin/nssh", "path", "/Users/cj/.local/bin/nssh", true},
		{"ordinary installer output", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			kind, data, ok := parseInstallEvent(tt.line)
			if kind != tt.kind || data != tt.data || ok != tt.ok {
				t.Fatalf("parseInstallEvent(%q) = %q, %q, %t; want %q, %q, %t", tt.line, kind, data, ok, tt.kind, tt.data, tt.ok)
			}
		})
	}
}

func TestRunInstallerWithEventsCapturesVersionAndPath(t *testing.T) {
	result, err := runInstallerWithEvents("printf 'NSSH_INSTALL_STATUS\\tDownloading archive\\nNSSH_INSTALL_VERSION\\tv0.2.4\\nNSSH_INSTALL_PATH\\t/tmp/nssh\\n'")
	if err != nil {
		t.Fatalf("runInstallerWithEvents error = %v", err)
	}
	if result.Version != "v0.2.4" {
		t.Fatalf("version = %q, want v0.2.4", result.Version)
	}
	if result.Path != "/tmp/nssh" {
		t.Fatalf("path = %q, want /tmp/nssh", result.Path)
	}
	if result.Output != "" || result.Errors != "" {
		t.Fatalf("unexpected output=%q errors=%q", result.Output, result.Errors)
	}
}

func TestRunInstallerWithEventsKeepsUnexpectedOutput(t *testing.T) {
	result, err := runInstallerWithEvents("printf 'unexpected output\\n'; printf 'unexpected error\\n' >&2; exit 7")
	if err == nil {
		t.Fatal("runInstallerWithEvents should return command error")
	}
	if result.Output != "unexpected output\n" {
		t.Fatalf("output = %q, want unexpected output", result.Output)
	}
	if result.Errors != "unexpected error\n" {
		t.Fatalf("errors = %q, want unexpected error", result.Errors)
	}
}

func TestRunReinstallDevPrintsMinimalBuildOutput(t *testing.T) {
	temp := t.TempDir()
	project := filepath.Join(temp, "project")
	if err := os.MkdirAll(filepath.Join(project, "cmd", "nssh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "cmd", "nssh-askpass"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.test/nssh\n\ngo 1.25\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "cmd", "nssh", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "cmd", "nssh-askpass", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	home := filepath.Join(temp, "home")
	t.Setenv("HOME", home)
	previousWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWd); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	got := captureStdout(t, func() {
		if err := runReinstallDev(); err != nil {
			t.Fatalf("runReinstallDev() error = %v", err)
		}
	})

	for _, reject := range []string{"Project root:", "─── Build", "Building nssh", "[✓]", "Installed:"} {
		if strings.Contains(got, reject) {
			t.Fatalf("runReinstallDev output should not contain %q:\n%s", reject, got)
		}
	}
	if !strings.Contains(got, "Built ~/.local/bin/nssh") {
		t.Fatalf("runReinstallDev output missing built message:\n%s", got)
	}
	if !strings.Contains(got, "Built ~/.local/bin/nssh-askpass") {
		t.Fatalf("runReinstallDev output missing askpass built message:\n%s", got)
	}
	for _, binary := range []string{"nssh", "nssh-askpass"} {
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", binary)); err != nil {
			t.Fatalf("%s was not installed: %v", binary, err)
		}
	}
}

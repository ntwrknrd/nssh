package self

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestRunCfgDefaultOmitsCommandBanners(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got := captureStdout(t, func() {
		if err := runCfg(false, false); err != nil {
			t.Fatal(err)
		}
	})

	if strings.HasPrefix(got, "\n") {
		t.Fatalf("cfg output should not start with banner spacing, got %q", got)
	}
	for _, unwanted := range []string{"──", "OK"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("cfg output should not include command banner %q:\n%s", unwanted, got)
		}
	}
	for _, want := range []string{
		"agent:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cfg output missing %q:\n%s", want, got)
		}
	}
}

func TestRunCfgPathsOnlyStaysRawPath(t *testing.T) {
	got := captureStdout(t, func() {
		if err := runCfg(false, true); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(got, "NSSH CONFIG") || strings.Contains(got, "OK") {
		t.Fatalf("paths-only output should not include command UI:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), filepath.Join("nssh", "config.yaml")) {
		t.Fatalf("paths-only output should include the config path, got %q", got)
	}
}

func TestConfigFilesIncludesResolvedIncludes(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config", "nssh")
	includeDir := filepath.Join(configDir, "conf.d")
	if err := os.MkdirAll(includeDir, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(configDir, "config.yaml")
	first := filepath.Join(includeDir, "01-base.yaml")
	second := filepath.Join(includeDir, "02-inventory.yaml")
	if err := os.WriteFile(first, []byte("agent:\n  idle_timeout: 30m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("inventory:\n  providers:\n    local:\n      type: local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("include: [conf.d/*.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}

	files, err := configFiles(&config.Paths{ConfigFile: root})
	if err != nil {
		t.Fatalf("configFiles: %v", err)
	}
	want := strings.Join([]string{root, first, second}, "\n")
	got := strings.Join(files, "\n")
	if got != want {
		t.Fatalf("config files:\nwant %s\n got %s", want, got)
	}
}

func TestRenderConfigTextLeavesNonTerminalOutputPlain(t *testing.T) {
	input := "agent:\n  idle_timeout: 4h\n"

	got := renderConfigText(input, false)

	if got != input {
		t.Fatalf("non-terminal config output should stay plain:\nwant %q\n got %q", input, got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-terminal config output should not include ANSI escapes:\n%q", got)
	}
}

func TestRenderConfigTextHighlightsTerminalOutput(t *testing.T) {
	input := "agent:\n  idle_timeout: 4h\n"

	got := renderConfigText(input, true)

	if got == input {
		t.Fatal("terminal config output should be highlighted")
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("terminal config output should include ANSI escapes:\n%q", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	fn()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

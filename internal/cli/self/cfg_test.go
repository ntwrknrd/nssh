package self

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCfgDefaultUsesCommandBanners(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got := captureStdout(t, func() {
		if err := runCfg(false, false); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.HasPrefix(got, "\n") {
		t.Fatalf("cfg output should start with blank line before banner, got %q", got)
	}
	for _, want := range []string{
		filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nssh", "config.toml"),
		"[agent]",
		"OK",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cfg output missing %q:\n%s", want, got)
		}
	}
}

func TestRunCfgPathOnlyStaysRawPath(t *testing.T) {
	got := captureStdout(t, func() {
		if err := runCfg(false, true); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(got, "NSSH CONFIG") || strings.Contains(got, "OK") {
		t.Fatalf("path-only output should not include command UI:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), filepath.Join("nssh", "config.toml")) {
		t.Fatalf("path-only output should be only the config path, got %q", got)
	}
}

func TestRenderConfigTextLeavesNonTerminalOutputPlain(t *testing.T) {
	input := "[agent]\nidle_timeout = \"4h\"\n"

	got := renderConfigText(input, false)

	if got != input {
		t.Fatalf("non-terminal config output should stay plain:\nwant %q\n got %q", input, got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-terminal config output should not include ANSI escapes:\n%q", got)
	}
}

func TestRenderConfigTextHighlightsTerminalOutput(t *testing.T) {
	input := "[agent]\nidle_timeout = \"4h\"\n"

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

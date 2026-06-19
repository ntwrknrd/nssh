package log

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestBuildGIFExportCommandAppliesConfiguredAggSizing(t *testing.T) {
	settings := config.GIFExportConfig{
		WindowSize: "145x30",
		FontSize:   18,
	}

	got, err := buildGIFExportCommand("/opt/bin/agg", "session.cast", "session.gif", settings)
	if err != nil {
		t.Fatalf("buildGIFExportCommand: %v", err)
	}
	want := []string{"/opt/bin/agg", "--cols", "145", "--rows", "30", "--font-size", "18", "session.cast", "session.gif"}
	if !slices.Equal(got, want) {
		t.Fatalf("gif export command = %#v, want %#v", got, want)
	}
}

func TestBuildGIFExportCommandWithoutSizingUsesCurrentAggCommand(t *testing.T) {
	got, err := buildGIFExportCommand("/opt/bin/agg", "session.cast", "session.gif", config.GIFExportConfig{})
	if err != nil {
		t.Fatalf("buildGIFExportCommand: %v", err)
	}
	want := []string{"/opt/bin/agg", "session.cast", "session.gif"}
	if !slices.Equal(got, want) {
		t.Fatalf("gif export command = %#v, want %#v", got, want)
	}
}

func TestBuildGIFExportCommandRejectsInvalidWindowSize(t *testing.T) {
	_, err := buildGIFExportCommand("/opt/bin/agg", "session.cast", "session.gif", config.GIFExportConfig{
		WindowSize: "145",
	})
	if err == nil {
		t.Fatal("buildGIFExportCommand succeeded, want invalid window size error")
	}
}

func TestBuildTextExportCommandIsUnchanged(t *testing.T) {
	got := buildTextExportCommand("/opt/bin/asciinema", "session.cast", "session.txt")
	want := []string{"/opt/bin/asciinema", "convert", "--overwrite", "session.cast", "session.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("text export command = %#v, want %#v", got, want)
	}
}

func TestFindGIFConverterRequiresAgg(t *testing.T) {
	binDir := t.TempDir()
	fallback := filepath.Join(binDir, "asciicast2gif")
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatalf("write fallback converter: %v", err)
	}
	t.Setenv("PATH", binDir)

	_, err := findGifConverter()
	if err == nil {
		t.Fatal("findGifConverter succeeded with only asciicast2gif on PATH, want agg error")
	}
}

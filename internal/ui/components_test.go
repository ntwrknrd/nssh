package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestCommandStartPrefixesBannerWithBlankLine(t *testing.T) {
	got := captureStdout(t, func() {
		CommandStart("TEST")
	})

	if !strings.HasPrefix(got, "\n") {
		t.Fatalf("CommandStart() output should start with blank line, got %q", got)
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

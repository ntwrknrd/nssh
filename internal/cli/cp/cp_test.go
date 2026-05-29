package cp

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestBareCpPrintsHelp(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		cmd := NewCmd()
		cmd.SetArgs([]string{})
		err = cmd.Execute()
	})

	if err != nil {
		t.Fatalf("bare cp should show help, got error: %v", err)
	}
	for _, want := range []string{
		"cp <source> <dest>",
		"Copy files via SCP",
		"--recursive",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q:\n%s", want, out)
		}
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

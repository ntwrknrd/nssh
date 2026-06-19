package self

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunResetDryRunDoesNotMentionSSHConfig(t *testing.T) {
	output := captureResetStdout(t, func() {
		if err := runReset(true, false); err != nil {
			t.Fatalf("runReset: %v", err)
		}
	})

	for _, reject := range []string{"SSH config", ".ssh"} {
		if strings.Contains(output, reject) {
			t.Fatalf("reset output mentions %q:\n%s", reject, output)
		}
	}
}

func TestResetHelpDoesNotMentionSSHConfig(t *testing.T) {
	cmd := NewResetCmd()

	for _, reject := range []string{"SSH config", ".ssh"} {
		if strings.Contains(cmd.Long, reject) {
			t.Fatalf("reset help mentions %q:\n%s", reject, cmd.Long)
		}
	}
}

func captureResetStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = old
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

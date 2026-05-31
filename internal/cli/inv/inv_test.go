package inv

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestInventoryCommandShowsRefreshAndStatusButNotDoctor(t *testing.T) {
	output, err := executeInvCommand("inv")
	if err != nil {
		t.Fatalf("inv command: %v", err)
	}
	for _, want := range []string{"Usage:", "inv [flags]", "refresh", "status"} {
		if !strings.Contains(output, want) {
			t.Fatalf("inv output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "doctor") {
		t.Fatalf("inv output should not list doctor:\n%s", output)
	}
}

func TestInventoryStatusRefreshFlagIsRejected(t *testing.T) {
	output, err := executeInvCommand("inv", "status", "--refresh")
	if err == nil {
		t.Fatalf("status --refresh succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(output, "unknown flag: --refresh") {
		t.Fatalf("status --refresh error missing unknown flag:\n%s", output)
	}
}

func TestInventoryDoctorCommandIsRejected(t *testing.T) {
	output, err := executeInvCommand("inv", "doctor")
	if err == nil {
		t.Fatalf("doctor succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(output, "unknown command \"doctor\"") {
		t.Fatalf("doctor error missing unknown command:\n%s", output)
	}
}

func executeInvCommand(args ...string) (string, error) {
	cmd := NewCmd()
	cmd.SetArgs(args[1:])
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer
	defer func() { os.Stdout = originalStdout }()

	err = cmd.Execute()
	_ = writer.Close()
	stdout, readErr := io.ReadAll(reader)
	if readErr != nil && err == nil {
		err = readErr
	}
	buf.Write(stdout)
	return buf.String(), err
}

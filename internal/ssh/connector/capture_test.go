//go:build unix

package connector

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

func TestRunCaptureInjectsPasswordAndDoesNotWriteStdout(t *testing.T) {
	writeFakeSSH(t, `#!/bin/sh
printf "netops@edge01's password: "
IFS= read -r pw
printf "\ncommand output\n"
`)
	direct := captureStdout(t, func() {
		conn := NewConnector("edge01", "netops", secret.NewFromString("pw"), []string{"-T", "--", "show version"})
		conn.SetTimeouts(&config.SSHConnectionConfig{Timeout: config.Duration(3 * time.Second)})
		out, err := conn.RunCapture(context.Background())
		if err != nil {
			t.Fatalf("RunCapture: %v", err)
		}
		if !strings.Contains(string(out), "command output") {
			t.Fatalf("captured output = %q", string(out))
		}
		if strings.Contains(string(out), "password:") {
			t.Fatalf("captured output leaked password prompt: %q", string(out))
		}
	})
	if direct != "" {
		t.Fatalf("RunCapture wrote to stdout: %q", direct)
	}
}

func TestRunCaptureRejectsHostKeyPromptNonInteractively(t *testing.T) {
	writeFakeSSH(t, `#!/bin/sh
echo "The authenticity of host 'edge01' can't be established."
echo "ED25519 key fingerprint is SHA256:abc123."
echo "Are you sure you want to continue connecting (yes/no/[fingerprint])?"
/bin/sleep 10
`)
	conn := NewConnector("edge01", "netops", nil, []string{"-T", "--", "show version"})
	conn.SetTimeouts(&config.SSHConnectionConfig{Timeout: config.Duration(3 * time.Second)})
	_, err := conn.RunCapture(context.Background())
	if err == nil || !strings.Contains(err.Error(), "host key prompt") {
		t.Fatalf("RunCapture error = %v, want host key prompt error", err)
	}
}

func TestRunCaptureDoesNotResolveDeferredPasswordWithoutPrompt(t *testing.T) {
	writeFakeSSH(t, `#!/bin/sh
printf "command output\n"
`)
	called := false
	conn := NewConnector("edge01", "netops", nil, []string{"-T", "--", "show version"})
	conn.SetPasswordResolver(func(context.Context) (*secret.Secret, error) {
		called = true
		return secret.NewFromString("pw"), nil
	})
	conn.SetTimeouts(&config.SSHConnectionConfig{Timeout: config.Duration(3 * time.Second)})
	out, err := conn.RunCapture(context.Background())
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	if !strings.Contains(string(out), "command output") {
		t.Fatalf("captured output = %q", string(out))
	}
	if called {
		t.Fatal("deferred password resolver should not run without a password prompt")
	}
}

func writeFakeSSH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out)
}

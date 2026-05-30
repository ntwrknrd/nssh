package agentcmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimeagent "github.com/ntwrknrd/nssh/internal/agent"
)

func TestAgentCommandShowsHelp(t *testing.T) {
	output, err := executeAgentCommand("agent")
	if err != nil {
		t.Fatalf("agent command: %v", err)
	}
	for _, want := range []string{"Usage:", "agent [flags]", "status", "stop", "restart", "doctor"} {
		if !strings.Contains(output, want) {
			t.Fatalf("agent output missing %q:\n%s", want, output)
		}
	}
	for _, reject := range []string{"Agent: inactive", "NO-OP"} {
		if strings.Contains(output, reject) {
			t.Fatalf("agent output should be help, not status; found %q:\n%s", reject, output)
		}
	}
	help, err := executeAgentCommand("agent", "--help")
	if err != nil {
		t.Fatalf("agent help: %v", err)
	}
	if !strings.Contains(help, "doctor") {
		t.Fatalf("agent help missing doctor command:\n%s", help)
	}
}

func TestAgentStatusShowsRuntimeState(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := runtimeagent.SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := runtimeagent.RunInBackground(context.Background(), runtimeagent.NewRuntimeProvider(), runtimeagent.DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	waitForSocket(t, socketPath)

	client, err := runtimeagent.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_ = client.Close()

	output, err := executeAgentCommand("agent", "status")
	if err != nil {
		t.Fatalf("agent status: %v", err)
	}
	for _, want := range []string{"Agent", "active", "Provider sessions", "Idle in", "Ends in"} {
		if !strings.Contains(output, want) {
			t.Fatalf("status missing %q:\n%s", want, output)
		}
	}
}

func TestAgentStopSucceedsWhenRunningAndWhenNotRunning(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := runtimeagent.SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := runtimeagent.RunInBackground(context.Background(), runtimeagent.NewRuntimeProvider(), runtimeagent.DefaultRuntimeConfig())
	waitForSocket(t, socketPath)

	if _, err := executeAgentCommand("agent", "stop"); err != nil {
		t.Fatalf("agent stop running: %v", err)
	}
	cancel()
	<-done

	if _, err := executeAgentCommand("agent", "stop"); err != nil {
		t.Fatalf("agent stop not running: %v", err)
	}
}

func TestAgentRestartStartsCleanRuntime(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := runtimeagent.SetSocketPathForTest(socketPath)
	defer restore()
	var cancel context.CancelFunc
	var done <-chan struct{}
	oldStart := startRuntimeAgent
	startRuntimeAgent = func() error {
		cancel, done = runtimeagent.RunInBackground(context.Background(), runtimeagent.NewRuntimeProvider(), runtimeagent.DefaultRuntimeConfig())
		waitForSocket(t, socketPath)
		return nil
	}
	defer func() {
		startRuntimeAgent = oldStart
		if cancel != nil {
			cancel()
			<-done
		}
	}()

	if _, err := executeAgentCommand("agent", "restart"); err != nil {
		t.Fatalf("agent restart: %v", err)
	}
	client, err := runtimeagent.Connect()
	if err != nil {
		t.Fatalf("connect after restart: %v", err)
	}
	status, err := client.Status()
	_ = client.Close()
	if err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	if status.ProviderSessions != 0 {
		t.Fatalf("provider sessions after restart = %d", status.ProviderSessions)
	}
}

func executeAgentCommand(args ...string) (string, error) {
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

func testSocketPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("nssh-agent-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", path)
}

package agentcmd

import (
	"bytes"
	"context"
	"errors"
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
	for _, want := range []string{"Usage:", "agent [flags]", "status", "stop", "reset"} {
		if !strings.Contains(output, want) {
			t.Fatalf("agent output missing %q:\n%s", want, output)
		}
	}
	for _, reject := range []string{"Agent: inactive", "NO-OP", "auth", "doctor", "restart"} {
		if strings.Contains(output, reject) {
			t.Fatalf("agent output should be help, not status; found %q:\n%s", reject, output)
		}
	}
	help, err := executeAgentCommand("agent", "--help")
	if err != nil {
		t.Fatalf("agent help: %v", err)
	}
	for _, reject := range []string{"auth", "doctor"} {
		if strings.Contains(help, reject) {
			t.Fatalf("agent help should not expose %q:\n%s", reject, help)
		}
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
	for _, want := range []string{"Agent", "active", "Access", "Idle shutdown in", "Max lifetime ends in"} {
		if !strings.Contains(output, want) {
			t.Fatalf("status missing %q:\n%s", want, output)
		}
	}
}

func TestAgentStatusFormatsCredentialProvidersAndDurations(t *testing.T) {
	got := formatCredentialProviders([]string{"op-private", "op-expedient"}, 2)
	if got != "2 (op-expedient, op-private)" {
		t.Fatalf("credential providers = %q", got)
	}
	if got := formatAgentSeconds(14345); got != "3h 59m 5s" {
		t.Fatalf("formatted duration = %q, want 3h 59m 5s", got)
	}
	if got := formatAgentSeconds(20861); got != "5h 47m 41s" {
		t.Fatalf("formatted duration = %q, want 5h 47m 41s", got)
	}
}

func TestAgentStatusFormatsManagedAccessWithoutSecrets(t *testing.T) {
	now := time.Now()
	status := &runtimeagent.StatusInfo{
		Access: []runtimeagent.AccessStatus{
			{
				Name:                 "op-expedient",
				Type:                 "1password",
				OnePasswordKeepalive: true,
				OnePasswordState:     "active",
				KeepaliveInterval:    300,
				KeepaliveNextUnix:    now.Add(4*time.Minute + 30*time.Second).Unix(),
				KeepaliveLastSuccess: now.Add(-30 * time.Second).Unix(),
			},
			{
				Name:                 "bw-work",
				Type:                 "bitwarden",
				BitwardenWarmSession: true,
				BitwardenWarmActive:  true,
			},
		},
	}
	got := formatManagedAccess(status)
	for _, want := range []string{"op-expedient keepalive active", "every 5m", "next in", "last ok", "bw-work warm session active"} {
		if !strings.Contains(got, want) {
			t.Fatalf("managed access missing %q: %s", want, got)
		}
	}
	for _, reject := range []string{"BW_SESSION", "secret", "op://", "password", "username"} {
		if strings.Contains(got, reject) {
			t.Fatalf("managed access leaked %q: %s", reject, got)
		}
	}
}

func TestAgentStatusOmitsConfiguredProviderNames(t *testing.T) {
	status := &runtimeagent.StatusInfo{
		CredentialProviders:     2,
		CredentialProviderNames: []string{"sops", "op-expedient"},
	}
	got := formatManagedAccess(status)
	if got != "none retained" {
		t.Fatalf("managed access = %q, want none retained", got)
	}
	if strings.Contains(got, "sops") || strings.Contains(got, "op-expedient") {
		t.Fatalf("managed access should not include configured provider names: %s", got)
	}
}

func TestAgentStatusFormatsHealthAndResourceState(t *testing.T) {
	status := &runtimeagent.StatusInfo{
		ProcessCount:       1,
		HeapAllocBytes:     10 * 1024 * 1024,
		RSSBytes:           40 * 1024 * 1024,
		Goroutines:         17,
		OpenFDs:            9,
		ProviderRequests:   42,
		ProviderFailures:   0,
		DuplicateProcesses: false,
	}
	health := formatHealth(status)
	if !strings.Contains(health, "1 process") || !strings.Contains(health, "42 requests") || !strings.Contains(health, "0 failures") {
		t.Fatalf("health = %q", health)
	}
	resources := formatResources(status)
	for _, want := range []string{"40 MB RSS", "10 MB heap", "17 goroutines", "9 fds"} {
		if !strings.Contains(resources, want) {
			t.Fatalf("resources missing %q: %q", want, resources)
		}
	}
}

func TestAgentStatusFormatsDuplicateProcessWarning(t *testing.T) {
	status := &runtimeagent.StatusInfo{ProcessCount: 2, DuplicateProcesses: true, ProviderFailures: 3}
	health := formatHealth(status)
	if !strings.Contains(health, "2 processes") || !strings.Contains(health, "duplicate") || !strings.Contains(health, "3 failures") {
		t.Fatalf("health missing duplicate warning: %q", health)
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

func TestAgentResetStopsRunningAgentWithoutRestarting(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := runtimeagent.SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := runtimeagent.RunInBackground(context.Background(), runtimeagent.NewRuntimeProvider(), runtimeagent.DefaultRuntimeConfig())
	defer func() {
		cancel()
		<-done
	}()
	waitForSocket(t, socketPath)

	output, err := executeAgentCommand("agent", "reset")
	if err != nil {
		t.Fatalf("agent reset: %v", err)
	}
	if !strings.Contains(output, "Agent reset") {
		t.Fatalf("reset output missing success message:\n%s", output)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("agent reset did not stop running agent")
	}
	cancel = func() {}
	done = closedDone()

	_, err = runtimeagent.Connect()
	if !errors.Is(err, runtimeagent.ErrAgentNotRunning) {
		t.Fatalf("connect after reset error = %v, want ErrAgentNotRunning", err)
	}
}

func TestAgentResetSucceedsWhenNotRunning(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := runtimeagent.SetSocketPathForTest(socketPath)
	defer restore()

	output, err := executeAgentCommand("agent", "reset")
	if err != nil {
		t.Fatalf("agent reset not running: %v", err)
	}
	if !strings.Contains(output, "Agent reset") {
		t.Fatalf("reset output missing success message:\n%s", output)
	}
	_, err = runtimeagent.Connect()
	if !errors.Is(err, runtimeagent.ErrAgentNotRunning) {
		t.Fatalf("connect after inactive reset error = %v, want ErrAgentNotRunning", err)
	}
}

func TestAgentRestartIsUnknown(t *testing.T) {
	output, err := executeAgentCommand("agent", "restart")
	if err == nil {
		t.Fatalf("agent restart succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(err.Error(), `unknown command "restart"`) && !strings.Contains(output, `unknown command "restart"`) {
		t.Fatalf("agent restart error = %v, output:\n%s", err, output)
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

func closedDone() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

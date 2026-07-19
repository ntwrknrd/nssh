package connector

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestBuildTestSSHArgsUsesRenderedNSSHOptions(t *testing.T) {
	cfg := TestConfig{
		Timeout: 5 * time.Second,
		SSHOptions: config.SSHHostConfig{
			Options: config.SSHOptions{
				"KexAlgorithms": config.NewSSHOptionItems("curve25519-sha256"),
			},
		},
	}

	args, cleanup, err := buildTestSSHArgs("example.com", "alice", cfg)
	if err != nil {
		t.Fatalf("buildTestSSHArgs error: %v", err)
	}
	defer cleanup()

	for _, want := range []string{"-F", "none", "KexAlgorithms=curve25519-sha256"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
}

func TestBuildTestSSHArgsStripsLogLevelFromRenderedNSSHOptions(t *testing.T) {
	cfg := TestConfig{
		Timeout: 5 * time.Second,
		SSHOptions: config.SSHHostConfig{
			Options: config.SSHOptions{
				"LogLevel":     config.NewSSHOptionString("ERROR"),
				"TCPKeepAlive": config.NewSSHOptionBool(true),
			},
		},
	}

	args, cleanup, err := buildTestSSHArgs("example.com", "alice", cfg)
	if err != nil {
		t.Fatalf("buildTestSSHArgs error: %v", err)
	}
	defer cleanup()

	if slices.Contains(args, "LogLevel=ERROR") {
		t.Fatalf("diagnostic probe args should not inherit LogLevel=ERROR: %#v", args)
	}
	if !slices.Contains(args, "TCPKeepAlive=yes") {
		t.Fatalf("diagnostic probe args should preserve other options: %#v", args)
	}
}

func TestBuildTestSSHArgs_UsesTempKnownHostsByDefault(t *testing.T) {
	cfg := TestConfig{Timeout: 5 * time.Second}

	args, cleanup, err := buildTestSSHArgs("example.com", "alice", cfg)
	if err != nil {
		t.Fatalf("buildTestSSHArgs error: %v", err)
	}
	defer cleanup()

	found := false
	var khPath string
	for i := 0; i < len(args)-1; i++ {
		if strings.HasPrefix(args[i], "UserKnownHostsFile=") {
			found = true
			khPath = strings.TrimPrefix(args[i], "UserKnownHostsFile=")
			break
		}
	}
	if !found {
		t.Fatalf("expected UserKnownHostsFile option in args: %v", args)
	}
	if _, err := os.Stat(khPath); err != nil {
		t.Fatalf("temp known_hosts not created: %v", err)
	}

	// Cleanup should remove the file
	cleanup()
	if _, err := os.Stat(khPath); !os.IsNotExist(err) {
		t.Fatalf("temp known_hosts was not removed by cleanup")
	}
}

func TestBuildTestSSHArgs_AllowsSystemKnownHostsWhenEnabled(t *testing.T) {
	cfg := TestConfig{Timeout: 5 * time.Second, UseSystemKnownHosts: true}

	args, cleanup, err := buildTestSSHArgs("example.com", "", cfg)
	if err != nil {
		t.Fatalf("buildTestSSHArgs error: %v", err)
	}
	defer cleanup()

	for _, a := range args {
		if strings.HasPrefix(a, "UserKnownHostsFile=") {
			t.Fatalf("expected no temp known_hosts when UseSystemKnownHosts=true, got %q", a)
		}
	}
}

func TestBuildTestSSHArgsUsesAskpassWithoutBatchMode(t *testing.T) {
	args, cleanup, err := buildTestSSHArgs("edge01.example", "target-user", TestConfig{
		Timeout: time.Second,
		Env:     []string{"SSH_ASKPASS=/tmp/nssh-askpass"},
	})
	if err != nil {
		t.Fatalf("buildTestSSHArgs: %v", err)
	}
	defer cleanup()
	joined := strings.Join(args, "\n")
	if strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("askpass probe enabled BatchMode: %#v", args)
	}
	if !strings.Contains(joined, "PreferredAuthentications=password,keyboard-interactive,publickey") {
		t.Fatalf("askpass probe missing password authentication: %#v", args)
	}
}

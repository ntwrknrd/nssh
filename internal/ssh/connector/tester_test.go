package connector

import (
	"os"
	"strings"
	"testing"
	"time"
)

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

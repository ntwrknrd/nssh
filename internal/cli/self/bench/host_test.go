package bench

import (
	"strings"
	"testing"
)

func TestRunSSHBenchmarkRejectsMissingHostBeforeBenchmarking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	err := runSSHBenchmark("codex-no-such-bench-host", 0, 1, true)
	if err == nil {
		t.Fatal("runSSHBenchmark error = nil, want host not found")
	}
	if !strings.Contains(err.Error(), "host not found: codex-no-such-bench-host") {
		t.Fatalf("runSSHBenchmark error = %q, want host not found", err)
	}
	if strings.Contains(err.Error(), "nssh binary not found") {
		t.Fatalf("runSSHBenchmark resolved binary before host validation: %v", err)
	}
}

func TestRunSCPBenchmarkRejectsMissingHostBeforeBenchmarking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	err := runSCPBenchmark("codex-no-such-bench-host", 0, 1, true, "1K")
	if err == nil {
		t.Fatal("runSCPBenchmark error = nil, want host not found")
	}
	if !strings.Contains(err.Error(), "host not found: codex-no-such-bench-host") {
		t.Fatalf("runSCPBenchmark error = %q, want host not found", err)
	}
	if strings.Contains(err.Error(), "nssh binary not found") {
		t.Fatalf("runSCPBenchmark resolved binary before host validation: %v", err)
	}
}

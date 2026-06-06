package self

import (
	"strings"
	"testing"
)

func TestRunStatusDoesNotShowAgentRuntimeSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	got := captureStdout(t, func() {
		if err := runStatus(); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{"NSSH STATUS", "Version", "Dependencies", "Configuration", "SSH Config", "Logging"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
	for _, reject := range []string{"Session", "provider runtime active", "Idle in", "Ends in"} {
		if strings.Contains(got, reject) {
			t.Fatalf("self status should not include agent runtime %q:\n%s", reject, got)
		}
	}
}

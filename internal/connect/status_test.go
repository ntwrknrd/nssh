package connect

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func inRex(t *testing.T) {
	t.Helper()
	t.Setenv("REX_SESSION", "s1")
	t.Setenv("REX_BLOCK", "b1")
	t.Setenv("NSSH_REX", "")
}

func TestSessionStatusLifecycle(t *testing.T) {
	inRex(t)
	out := withTitleTerminal(t, true)
	status := newSessionStatus(&ResolvedHost{Canonical: "acm-core1"}, nil)
	if status == nil {
		t.Fatal("no reporter inside Rex")
	}

	status.connecting()
	prompt := status.hostKeyPrompt(func(connector.HostKeyPrompt) connector.HostKeyAction {
		if !strings.Contains(out.String(), "state=blocked:app=nssh:kind=question") {
			t.Errorf("prompt shown before blocked report: %q", out.String())
		}
		return connector.HostKeyAcceptOnce
	})
	if prompt(connector.HostKeyPrompt{}) != connector.HostKeyAcceptOnce {
		t.Fatal("wrapped prompt changed the action")
	}
	status.established()
	status.finished(nil)

	var states []string
	for _, report := range strings.Split(out.String(), "\x1b\\") {
		if fields := strings.SplitN(strings.TrimPrefix(report, "\x1b]7501;"), ":", 2); fields[0] != "" {
			states = append(states, fields[0])
		}
	}
	want := "state=working state=blocked state=working state=idle state=clear"
	if got := strings.Join(states, " "); got != want {
		t.Fatalf("states = %q, want %q", got, want)
	}
}

func TestSessionStatusErrorSurvives(t *testing.T) {
	inRex(t)
	out := withTitleTerminal(t, true)
	newSessionStatus(&ResolvedHost{Canonical: "acm-core1"}, nil).finished(&exit.ExitError{Code: 255})
	// "acm-core1: exit status 255"
	if got := out.String(); !strings.HasPrefix(got, "\x1b]7501;state=error:app=nssh:") || !strings.Contains(got, "msg=YWNtLWNvcmUxOiBleGl0IHN0YXR1cyAyNTU=") {
		t.Fatalf("error report = %q", got)
	}
}

func TestSessionStatusGating(t *testing.T) {
	resolved := &ResolvedHost{Canonical: "acm-core1"}
	t.Run("outside rex", func(t *testing.T) {
		t.Setenv("REX_SESSION", "")
		withTitleTerminal(t, true)
		if newSessionStatus(resolved, nil) != nil {
			t.Fatal("reporter outside Rex")
		}
	})
	t.Run("not a terminal", func(t *testing.T) {
		inRex(t)
		withTitleTerminal(t, false)
		if newSessionStatus(resolved, nil) != nil {
			t.Fatal("reporter without a terminal")
		}
	})
	t.Run("non-interactive mode", func(t *testing.T) {
		inRex(t)
		withTitleTerminal(t, true)
		if newSessionStatus(resolved, []string{"-N"}) != nil {
			t.Fatal("reporter for -N")
		}
	})
	var nilStatus *sessionStatus
	nilStatus.connecting()
	nilStatus.established()
	nilStatus.finished(nil)
	if nilStatus.hostKeyPrompt(nil) != nil {
		t.Fatal("nil reporter wrapped a nil prompt")
	}
}

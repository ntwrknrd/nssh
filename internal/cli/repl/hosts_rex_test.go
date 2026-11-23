package repl

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	core "github.com/ntwrknrd/nssh/internal/repl"
)

func TestHostListStatus(t *testing.T) {
	t.Setenv("REX_SESSION", "s1")
	t.Setenv("REX_BLOCK", "b1")
	t.Setenv("NSSH_REX", "")
	var out bytes.Buffer
	status := newRexHostStatus(&out, true, "show version")
	a := core.ResolvedTarget{Identity: "core1"}
	b := core.ResolvedTarget{Identity: "admin@core2"}
	status.start([]core.ResolvedTarget{a, b})
	status.event(core.Event{Target: a, State: core.Queued})
	status.event(core.Event{Target: a, State: core.Running})
	status.event(core.Event{Target: a, State: core.Completed})
	status.event(core.Event{Target: b, State: core.Failed})
	status.finish()

	var reports []string
	for _, report := range strings.Split(out.String(), "\x1b\\") {
		if report != "" {
			reports = append(reports, strings.TrimPrefix(report, "\x1b]7501;"))
		}
	}
	msg := func(s string) string { return "msg=" + base64.StdEncoding.EncodeToString([]byte(s)) }
	checks := []string{
		"state=clear:id=hosts",
		"state=working:app=nssh:",
		"state=working:id=hosts/core1:",
		"state=done:id=hosts/core1:",
		"state=working:app=nssh:progress=50:",
		"state=error:id=hosts/h-",
		"state=working:app=nssh:",
		"state=error:app=nssh:",
	}
	if len(reports) != len(checks) {
		t.Fatalf("reports = %q, want %d", reports, len(checks))
	}
	for i, prefix := range checks {
		if !strings.HasPrefix(reports[i], prefix) {
			t.Fatalf("report %d = %q, want prefix %q", i, reports[i], prefix)
		}
	}
	if !strings.Contains(reports[7], msg("1 of 2 hosts failed")) {
		t.Fatalf("final report = %q", reports[7])
	}
}

func TestHostListStatusDisabled(t *testing.T) {
	t.Setenv("REX_SESSION", "s1")
	t.Setenv("REX_BLOCK", "b1")
	var out bytes.Buffer
	if newRexHostStatus(&out, false, "x") != nil {
		t.Fatal("reporter without a terminal")
	}
	t.Setenv("NSSH_REX", "0")
	status := newRexHostStatus(&out, true, "x")
	status.start(nil)
	status.event(core.Event{State: core.Running})
	status.finish()
	if status != nil || out.Len() != 0 {
		t.Fatalf("disabled reporter wrote %q", out.String())
	}
}

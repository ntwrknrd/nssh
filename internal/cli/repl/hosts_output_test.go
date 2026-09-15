package repl

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	core "github.com/ntwrknrd/nssh/internal/repl"
)

func TestHostListOutputKeepsTablesAndRecapsRequestedOrder(t *testing.T) {
	var out, errOut bytes.Buffer
	p := hostListOutput{out: &out, errOut: &errOut}
	first := core.ResolvedTarget{Identity: "operator@border.example.net", Value: "border"}
	second := core.ResolvedTarget{Identity: "operator@agg.example.net", Value: "agg"}
	p.start("show env power", []core.ResolvedTarget{first, second})
	table := "Power     Input\n------ --------\n1         73.0W\n"
	events := []core.Event{
		{Target: second, State: core.Completed, Result: core.Result{Stdout: []byte(table)}},
		{Target: first, State: core.Completed, Result: core.Result{Stdout: []byte(table)}},
	}
	for _, e := range events {
		p.event(e)
	}
	p.recap(events)
	if want := taskBanner("[agg] OK") + "\n" + table + "\n" + taskBanner("[border] OK") + "\n" + table + "\n"; out.String() != want {
		t.Fatalf("output=%q want=%q", out.String(), want)
	}
	recap := errOut.String()
	for _, forbidden := range []string{"operator@", ".example.net", "completed", "exit 0", "Summary (requested order)"} {
		if strings.Contains(out.String()+recap, forbidden) {
			t.Errorf("redundant %q in output", forbidden)
		}
	}
	if strings.Count(recap, "show env power") != 1 || !strings.Contains(recap, "Command: show env power") || !strings.Contains(recap, "2 hosts") || !strings.Contains(recap, "Results") {
		t.Fatalf("headings: %s", recap)
	}
	if !strings.Contains(recap, "border : ok=1  failed=0  canceled=0\nagg    : ok=1  failed=0  canceled=0\n") {
		t.Fatalf("recap: %s", recap)
	}
}

func TestHostListOutputAttributesStderrAndFailureWithoutChangingBytes(t *testing.T) {
	var out, errOut bytes.Buffer
	p := hostListOutput{out: &out, errOut: &errOut}
	target := core.ResolvedTarget{Identity: "canonical", Value: "alias"}
	p.start("show", []core.ResolvedTarget{target})
	event := core.Event{Target: target, State: core.Failed, Err: errors.New("SSH failed"), Result: core.Result{
		Stdout: []byte("\x1b[32mdevice\x1b[0m"), Stderr: []byte("diagnostic\n"), ExitCode: 7, Truncated: true,
	}}
	p.event(event)
	p.recap([]core.Event{event})
	if out.String() != taskBanner("[alias] FAILED (exit 7)")+"\n\x1b[32mdevice\x1b[0m\n"+"\n" {
		t.Fatalf("stdout=%q", out.String())
	}
	for _, want := range []string{taskBanner("[alias] STDERR") + "\ndiagnostic\n", "error: [alias] SSH failed", "warning: [alias] output truncated", "alias : ok=0  failed=1  canceled=0"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("missing %q: %q", want, errOut.String())
		}
	}
	if strings.Contains(errOut.String(), "device") || strings.Contains(out.String(), "diagnostic") {
		t.Fatal("streams mixed")
	}
}

func TestHostListOutputEmptyAndCanceled(t *testing.T) {
	var out, errOut bytes.Buffer
	p := hostListOutput{out: &out, errOut: &errOut}
	p.recap(nil)
	if errOut.Len() != 0 {
		t.Fatal("preflight failure printed recap")
	}
	target := core.ResolvedTarget{Identity: "host", Value: "host"}
	p.start("command", []core.ResolvedTarget{target})
	p.event(core.Event{Target: target, State: core.Running})
	event := core.Event{Target: target, State: core.Canceled}
	p.event(event)
	p.recap([]core.Event{event})
	if out.Len() != 0 || strings.Count(errOut.String(), "[host] CANCELED") != 1 || !strings.Contains(errOut.String(), "ok=0  failed=0  canceled=1") {
		t.Fatalf("out=%q err=%q", out.String(), errOut.String())
	}
}

func TestHostListOutputEscapesMetadata(t *testing.T) {
	if got := displayLabel("show\n\x1b[31m"); got != `show\u000a\u001b[31m` {
		t.Fatalf("label=%q", got)
	}
	var out bytes.Buffer
	if got := hostListColor(&out, "ok", core.Completed); got != "ok" {
		t.Fatalf("redirected output colored: %q", got)
	}
	if len(taskBanner("TASK [show]")) != 80 {
		t.Fatal("unexpected banner width")
	}
}

package repl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/exit"
	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestPlainEventSeparatesStreamsAndFailure(t *testing.T) {
	var out, errs bytes.Buffer
	event := core.Event{Target: core.ResolvedTarget{Identity: "edge"}, Command: "show", State: core.Failed, Result: core.Result{Stdout: []byte("OUTPUT\n"), Stderr: []byte("ERROR\n"), ExitCode: 7, Truncated: true}, Err: errors.New("failed")}
	writePlainEvent(event, &out, &errs)
	if out.String() != "[edge] show: OUTPUT\n" {
		t.Fatalf("stdout=%q", out.String())
	}
	for _, want := range []string{"[edge] show: ERROR\n", "exit 7", "output truncated"} {
		if !strings.Contains(errs.String(), want) {
			t.Fatalf("stderr=%q lacks %q", errs.String(), want)
		}
	}
	if strings.Contains(errs.String(), "OUTPUT") {
		t.Fatal("stdout copied into stderr")
	}
}

func TestSummaryKeepsRepeatedCommandsAndRequestedOrder(t *testing.T) {
	target := core.ResolvedTarget{Identity: "a"}
	events := []core.Event{{Target: target, Command: "same", CommandIndex: 0, State: core.Queued}, {Target: target, Command: "same", CommandIndex: 1, State: core.Queued}, {Target: target, Command: "same", CommandIndex: 1, State: core.Skipped}, {Target: target, Command: "same", CommandIndex: 0, State: core.Failed, Result: core.Result{ExitCode: 7}}}
	got := renderSummary(events)
	if !strings.HasSuffix(got, "[a] same: failed (exit 7)\n[a] same: skipped\n") {
		t.Fatal(got)
	}
}

func TestPlainIdleCancellationDoesNotWaitForInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runPlainContext(ctx, r, io.Discard, io.Discard, 1) }()
	cancel()
	select {
	case err := <-done:
		var code *exit.ExitError
		if !errors.As(err, &code) || code.Code != 130 {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle input did not cancel")
	}
}

func TestTranscriptEvictionSurvivesResizeAndShowsNewestResult(t *testing.T) {
	m := model{viewport: viewport.New(80, 20), transcript: limitedBuffer{max: 32}}
	m.appendTranscript(strings.Repeat("old", 20))
	m.appendTranscript("new failure\n")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(model)
	if len(m.transcript.String()) > 32 || !strings.Contains(m.viewport.View(), "older transcript output evicted") || !strings.Contains(m.viewport.View(), "new failure") {
		t.Fatal(m.viewport.View())
	}
	m.appendTranscript("\x1b]52;c;YWJj\x07\x1b[2Jsafe\x00\n")
	if strings.ContainsAny(m.transcript.String(), "\x1b\x00\x07") {
		t.Fatalf("unsafe controls=%q", m.transcript.String())
	}
}

func TestModalTrustOwnsInputAndActiveCancelWaits(t *testing.T) {
	canceled := false
	m := model{input: textinput.New(), viewport: viewport.New(80, 10), transcript: limitedBuffer{max: 1024}, active: true, cancel: func() { canceled = true }}
	request := &trustRequest{prompt: connector.HostKeyPrompt{Host: "edge", Changed: true}, response: make(chan connector.HostKeyAction, 1)}
	next, _ := m.Update(request)
	m = next.(model)
	if !strings.Contains(m.View(), "CHANGED HOST KEY") {
		t.Fatal(m.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(model)
	if action := <-request.response; action != connector.HostKeyAcceptAlways {
		t.Fatal(action)
	}
	if m.input.Value() != "" || m.trust != nil {
		t.Fatal("prompt input reached command editor")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if !canceled || !m.active {
		t.Fatal("cancel must retain active state until cleanup finishes")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd != nil || !m.active {
		t.Fatal("started submission during cleanup")
	}
}

func TestCatalogSelectorsAndAliasIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	path := filepath.Join(root, "config", "nssh", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	yaml := `inventory:
  providers:
    local:
      type: local
      groups:
        lab: {}
      hosts:
        edge.example:
          group: lab
          aliases: [edge]
          auth:
            mode: key
            username: ops
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	cat, err := connect.BuildHostCatalog(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolve := catalogResolver(cfg, cat)
	for _, spec := range []core.Target{{Value: "group:local/lab port:22 user:ops", Selector: true}, {Value: "edge"}, {Value: "ops@edge.example"}} {
		got, err := resolve(context.Background(), spec)
		if err != nil || len(got) != 1 || got[0].Identity != "ops@edge.example" {
			t.Fatalf("spec=%+v got=%+v err=%v", spec, got, err)
		}
	}
}

func TestHistoryConcurrentInstancesPreserveEntries(t *testing.T) {
	h := historyStore{path: filepath.Join(t.TempDir(), "history")}
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.append(fmt.Sprintf("command %d", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := h.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("history lost entries: %v", got)
	}
}

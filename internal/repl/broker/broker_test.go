package broker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/repl"
)

func TestRunEmitsReadyOnStartupAndExits(t *testing.T) {
	var out bytes.Buffer

	err := Run(context.Background(), repl.Options{
		In:  strings.NewReader(`{"type":"exit"}` + "\n"),
		Out: &out,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	if got := events[0]["type"]; got != "ready" {
		t.Fatalf("first event type = %v, want ready; events=%v", got, events)
	}
}

func TestRunSuggestReturnsHostSuggestions(t *testing.T) {
	var out bytes.Buffer

	err := Run(context.Background(), repl.Options{
		In: strings.NewReader(
			`{"type":"suggest","line":"[ 'ed' ] ( '' )"}` + "\n" +
				`{"type":"exit"}` + "\n",
		),
		Out:      &out,
		Resolver: fakeResolver{suggestions: []string{"edge01", "edge02"}},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	suggestions := findEvent(t, events, "suggestions")
	got := suggestions["suggestions"].([]any)
	if len(got) != 2 || got[0] != "edge01" || got[1] != "edge02" {
		t.Fatalf("suggestions = %#v", got)
	}
}

func TestRunHistoryLoadAndAppendUseHistoryStore(t *testing.T) {
	var out bytes.Buffer
	history := &fakeHistoryStore{lines: []string{"[ 'edge01' ] ( 'show hostname' )"}}

	err := Run(context.Background(), repl.Options{
		In: strings.NewReader(
			`{"type":"history_load"}` + "\n" +
				`{"type":"history_append","line":"[ 'edge02' ] ( 'show version' )"}` + "\n" +
				`{"type":"exit"}` + "\n",
		),
		Out:     &out,
		History: history,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	loaded := findEvent(t, events, "history")
	got := loaded["lines"].([]any)
	if len(got) != 1 || got[0] != "[ 'edge01' ] ( 'show hostname' )" {
		t.Fatalf("history lines = %#v", got)
	}
	if len(history.appended) != 1 || history.appended[0] != "[ 'edge02' ] ( 'show version' )" {
		t.Fatalf("appended history = %#v", history.appended)
	}
}

func TestRunSubmitEmitsStatusStartedCompletedAndFinalStatus(t *testing.T) {
	var out bytes.Buffer
	runner := fakeRunner{outputs: map[string][]byte{
		"edge01": []byte("edge01\n\x00\n"),
		"edge02": []byte("edge02\n"),
	}}

	err := Run(context.Background(), repl.Options{
		In: strings.NewReader(
			`{"type":"submit","line":"[ 'edge(01,02)' ] ( 'show hostname' )"}` + "\n" +
				`{"type":"exit"}` + "\n",
		),
		Out:         &out,
		Resolver:    fakeResolver{},
		Runner:      runner,
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	assertEventTypes(t, events, []string{
		"ready",
		"status",
		"started",
		"completed",
		"started",
		"completed",
		"status",
	})
	first := firstCompletedForHost(t, events, "edge01")
	encoded := first["stdout"].(string)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("stdout is not base64: %v", err)
	}
	if string(decoded) != "edge01\n\x00\n" {
		t.Fatalf("decoded stdout = %q", decoded)
	}
	if got := int(first["exit_code"].(float64)); got != 0 {
		t.Fatalf("exit_code = %d, want 0", got)
	}
	final := events[len(events)-1]
	if final["type"] != "status" || int(final["done"].(float64)) != 2 || int(final["pending"].(float64)) != 0 {
		t.Fatalf("final status = %#v", final)
	}
}

func TestRunSubmitAcceptsRepeatedTargetTokens(t *testing.T) {
	var out bytes.Buffer

	err := Run(context.Background(), repl.Options{
		In: strings.NewReader(
			`{"type":"submit","line":"[ 'edge01', 'edge02' ] ( 'show hostname' )"}` + "\n" +
				`{"type":"exit"}` + "\n",
		),
		Out:         &out,
		Resolver:    fakeResolver{},
		Runner:      fakeRunner{},
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	first := firstCompletedForHost(t, events, "edge01")
	second := firstCompletedForHost(t, events, "edge02")
	if first["command"] != "show hostname" || second["command"] != "show hostname" {
		t.Fatalf("completed commands = %q, %q", first["command"], second["command"])
	}
	final := events[len(events)-1]
	if final["type"] != "status" || int(final["done"].(float64)) != 2 || int(final["pending"].(float64)) != 0 {
		t.Fatalf("final status = %#v", final)
	}
}

func TestRunSubmitMultiCommandEmitsCommandMajorBatches(t *testing.T) {
	var out bytes.Buffer

	err := Run(context.Background(), repl.Options{
		In: strings.NewReader(
			`{"type":"submit","line":"[ 'edge01', 'edge02' ] ( 'show one', 'show two' )"}` + "\n" +
				`{"type":"exit"}` + "\n",
		),
		Out:         &out,
		Resolver:    fakeResolver{},
		Runner:      fakeRunner{},
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	var completed []batchCommand
	for _, event := range events {
		if event["type"] == "completed" {
			completed = append(completed, batchCommand{
				batch:   int(event["batch"].(float64)),
				index:   int(event["index"].(float64)),
				host:    event["host"].(string),
				command: event["command"].(string),
			})
		}
	}
	if len(completed) != 4 {
		t.Fatalf("completed = %#v, want 4 completions; events=%#v", completed, events)
	}
	for i, event := range completed[:2] {
		if event.batch != 1 || event.command != "show one" {
			t.Fatalf("completed[%d] = %#v, want batch 1 show one; completed=%#v", i, event, completed)
		}
	}
	for i, event := range completed[2:] {
		if event.batch != 2 || event.command != "show two" {
			t.Fatalf("completed[%d] = %#v, want batch 2 show two; completed=%#v", i+2, event, completed)
		}
	}
	seen := map[string]bool{}
	for _, event := range completed {
		seen[fmt.Sprintf("%d:%d:%s:%s", event.batch, event.index, event.host, event.command)] = true
	}
	for _, key := range []string{
		"1:0:edge01:show one",
		"1:1:edge02:show one",
		"2:0:edge01:show two",
		"2:1:edge02:show two",
	} {
		if !seen[key] {
			t.Fatalf("missing completion %s in %#v", key, completed)
		}
	}
	final := events[len(events)-1]
	if final["type"] != "status" ||
		int(final["total"].(float64)) != 4 ||
		int(final["done"].(float64)) != 4 ||
		int(final["pending"].(float64)) != 0 {
		t.Fatalf("final status = %#v", final)
	}
}

type batchCommand struct {
	batch   int
	index   int
	host    string
	command string
}

func TestRunRejectsSubmitWhileBatchIsActive(t *testing.T) {
	inR, inW := ioPipe(t)
	var out threadSafeBuffer
	release := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), repl.Options{
			In:       inR,
			Out:      &out,
			Resolver: fakeResolver{},
			Runner:   blockingRunner{release: release},
		})
	}()

	writeLine(t, inW, `{"type":"submit","line":"[ 'edge(01,02)' ] ( 'show hostname' )"}`)
	waitForEvent(t, &out, "started")
	writeLine(t, inW, `{"type":"submit","line":"[ 'edge03' ] ( 'show hostname' )"}`)
	waitForEvent(t, &out, "error")
	close(release)
	writeLine(t, inW, `{"type":"exit"}`)
	if err := waitDone(done); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	errEvent := findEvent(t, events, "error")
	if !strings.Contains(errEvent["message"].(string), "batch already active") {
		t.Fatalf("error event = %#v", errEvent)
	}
}

func TestRunCancelCancelsActiveBatchAndEmitsFinalStatus(t *testing.T) {
	inR, inW := ioPipe(t)
	var out threadSafeBuffer
	release := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), repl.Options{
			In:       inR,
			Out:      &out,
			Resolver: fakeResolver{},
			Runner:   blockingRunner{release: release},
		})
	}()

	writeLine(t, inW, `{"type":"submit","line":"[ 'edge(01,02)' ] ( 'show hostname' )"}`)
	waitForEvent(t, &out, "started")
	writeLine(t, inW, `{"type":"cancel"}`)
	waitForFinalStatus(t, &out, 2)
	writeLine(t, inW, `{"type":"exit"}`)
	if err := waitDone(done); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	final := events[len(events)-1]
	if final["type"] != "status" || int(final["done"].(float64)) != 2 || int(final["failed"].(float64)) != 2 {
		t.Fatalf("final cancel status = %#v", final)
	}
}

func TestRunParserErrorDoesNotRunCommand(t *testing.T) {
	var out bytes.Buffer
	runner := &countingRunner{}

	err := Run(context.Background(), repl.Options{
		In:     strings.NewReader(`{"type":"submit","line":"edge01 show hostname"}` + "\n" + `{"type":"exit"}` + "\n"),
		Out:    &out,
		Runner: runner,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	errEvent := findEvent(t, events, "error")
	if !strings.Contains(errEvent["message"].(string), "target group must start with [") {
		t.Fatalf("error event = %#v", errEvent)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func TestRunResolverErrorDoesNotRunCommand(t *testing.T) {
	var out bytes.Buffer
	runner := &countingRunner{}

	err := Run(context.Background(), repl.Options{
		In:       strings.NewReader(`{"type":"submit","line":"[ 'bad01' ] ( 'show hostname' )"}` + "\n" + `{"type":"exit"}` + "\n"),
		Out:      &out,
		Resolver: fakeResolver{},
		Runner:   runner,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	events := decodeEvents(t, out.String())
	errEvent := findEvent(t, events, "error")
	if !strings.Contains(errEvent["message"].(string), "bad host") {
		t.Fatalf("error event = %#v", errEvent)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

type fakeResolver struct {
	suggestions []string
}

func (r fakeResolver) ResolveHost(host string) (string, error) {
	if strings.HasPrefix(host, "bad") {
		return "", errors.New("bad host")
	}
	return host, nil
}

func (r fakeResolver) SelectHosts(selector string) ([]string, error) {
	return []string{"edge01", "edge02"}, nil
}

func (r fakeResolver) SuggestHosts(prefix string) ([]string, error) {
	return r.suggestions, nil
}

type fakeRunner struct {
	outputs map[string][]byte
}

func (r fakeRunner) RunCommand(_ context.Context, host, command string) repl.CommandResult {
	return repl.CommandResult{Host: host, Command: command, Output: r.outputs[host]}
}

type blockingRunner struct {
	release <-chan struct{}
}

func (r blockingRunner) RunCommand(ctx context.Context, host, command string) repl.CommandResult {
	select {
	case <-r.release:
		return repl.CommandResult{Host: host, Command: command, Output: []byte(host + "\n")}
	case <-ctx.Done():
		return repl.CommandResult{Host: host, Command: command, Err: ctx.Err()}
	}
}

type countingRunner struct {
	calls int
}

func (r *countingRunner) RunCommand(_ context.Context, host, command string) repl.CommandResult {
	r.calls++
	return repl.CommandResult{Host: host, Command: command}
}

type fakeHistoryStore struct {
	lines    []string
	appended []string
}

func (s *fakeHistoryStore) Load() ([]string, error) {
	return append([]string(nil), s.lines...), nil
}

func (s *fakeHistoryStore) Append(line string) error {
	s.appended = append(s.appended, line)
	return nil
}

type threadSafeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func decodeEvents(t *testing.T, text string) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	return events
}

func findEvent(t *testing.T, events []map[string]any, typ string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["type"] == typ {
			return event
		}
	}
	t.Fatalf("missing event %q in %#v", typ, events)
	return nil
}

func firstCompletedForHost(t *testing.T, events []map[string]any, host string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["type"] == "completed" && event["host"] == host {
			return event
		}
	}
	t.Fatalf("missing completed event for host %q in %#v", host, events)
	return nil
}

func assertEventTypes(t *testing.T, events []map[string]any, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("event count = %d, want %d: %#v", len(events), len(want), events)
	}
	for i, typ := range want {
		if events[i]["type"] != typ {
			t.Fatalf("event %d type = %v, want %s; events=%#v", i, events[i]["type"], typ, events)
		}
	}
}

func ioPipe(t *testing.T) (*io.PipeReader, *io.PipeWriter) {
	t.Helper()
	r, w := io.Pipe()
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w
}

func writeLine(t *testing.T, w *io.PipeWriter, line string) {
	t.Helper()
	if _, err := w.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func waitForEvent(t *testing.T, out *threadSafeBuffer, typ string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range decodeEventsAllowEmpty(t, out.String()) {
			if event["type"] == typ {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for event %q; output=%s", typ, out.String())
}

func waitForFinalStatus(t *testing.T, out *threadSafeBuffer, done int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events := decodeEventsAllowEmpty(t, out.String())
		if len(events) == 0 {
			continue
		}
		event := events[len(events)-1]
		if event["type"] == "status" && int(event["done"].(float64)) == done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for final status; output=%s", out.String())
}

func decodeEventsAllowEmpty(t *testing.T, text string) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events
}

func waitDone(done <-chan error) error {
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return errors.New("timed out waiting for Run")
	}
}

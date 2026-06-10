package repl

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestExecuteFanoutHonorsConcurrencyAndPreservesOrder(t *testing.T) {
	runner := &trackingRunner{
		delays: map[string]time.Duration{
			"edge01": 30 * time.Millisecond,
			"edge02": 10 * time.Millisecond,
			"edge03": 1 * time.Millisecond,
		},
	}

	results := ExecuteFanout(context.Background(), []string{"edge01", "edge02", "edge03"}, "show version", 2, runner)

	if runner.maxActive > 2 {
		t.Fatalf("max active = %d, want <= 2", runner.maxActive)
	}
	var hosts []string
	for _, result := range results {
		hosts = append(hosts, result.Host)
	}
	want := []string{"edge01", "edge02", "edge03"}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("result hosts = %#v, want %#v", hosts, want)
	}
}

func TestExecuteFanoutKeepsFailedTargetResult(t *testing.T) {
	runner := &trackingRunner{fail: map[string]error{"edge02": errors.New("boom")}}

	results := ExecuteFanout(context.Background(), []string{"edge01", "edge02"}, "show version", 12, runner)

	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("first result err = %v", results[0].Err)
	}
	if results[1].Err == nil || results[1].Host != "edge02" {
		t.Fatalf("second result = %+v", results[1])
	}
}

func TestDefaultConcurrency(t *testing.T) {
	if DefaultConcurrency != 12 {
		t.Fatalf("DefaultConcurrency = %d, want 12", DefaultConcurrency)
	}
}

func TestExecuteFanoutStreamEmitsCompletionOrder(t *testing.T) {
	runner := &trackingRunner{
		delays: map[string]time.Duration{
			"edge01": 30 * time.Millisecond,
			"edge02": 1 * time.Millisecond,
		},
	}

	events := collectEvents(ExecuteFanoutStream(context.Background(), []string{"edge01", "edge02"}, "show version", 2, runner))

	var completed []string
	for _, event := range events {
		if event.Kind == FanoutCompleted {
			completed = append(completed, event.Result.Host)
		}
	}
	want := []string{"edge02", "edge01"}
	if !reflect.DeepEqual(completed, want) {
		t.Fatalf("completed hosts = %#v, want %#v", completed, want)
	}
}

func TestExecuteFanoutStreamIncludesInputIndexes(t *testing.T) {
	events := collectEvents(ExecuteFanoutStream(context.Background(), []string{"edge01", "edge02"}, "show version", 2, &trackingRunner{}))

	indexes := make(map[string]int)
	for _, event := range events {
		if event.Kind == FanoutCompleted {
			indexes[event.Result.Host] = event.Index
		}
	}
	if indexes["edge01"] != 0 || indexes["edge02"] != 1 {
		t.Fatalf("indexes = %#v, want edge01=0 edge02=1", indexes)
	}
}

func TestExecuteFanoutStreamEmitsFailuresAndContinues(t *testing.T) {
	runner := &trackingRunner{fail: map[string]error{"edge02": errors.New("boom")}}

	events := collectEvents(ExecuteFanoutStream(context.Background(), []string{"edge01", "edge02"}, "show version", 2, runner))

	var completed int
	var sawFailure bool
	for _, event := range events {
		if event.Kind != FanoutCompleted {
			continue
		}
		completed++
		if event.Result.Host == "edge02" && event.Result.Err != nil {
			sawFailure = true
		}
	}
	if completed != 2 || !sawFailure {
		t.Fatalf("events = %+v, completed=%d sawFailure=%v", events, completed, sawFailure)
	}
}

func TestExecuteFanoutStreamHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	events := collectEvents(ExecuteFanoutStream(ctx, []string{"edge01", "edge02"}, "show version", 1, &trackingRunner{}))

	for _, event := range events {
		if event.Kind == FanoutCompleted && event.Result.Err == nil {
			t.Fatalf("completed event should carry cancellation error: %+v", event)
		}
	}
}

func TestExecuteCommandFanoutStreamRunsCommandMajorAndTagsBatches(t *testing.T) {
	events := collectEvents(ExecuteCommandFanoutStream(
		context.Background(),
		[]string{"edge01", "edge02"},
		[]string{"show one", "show two"},
		1,
		&trackingRunner{},
	))

	var completed []tuple
	for _, event := range events {
		if event.Kind == FanoutCompleted {
			completed = append(completed, tuple{event.Batch, event.Index, event.Result.Host, event.Result.Command})
		}
	}
	for i := 1; i < len(completed); i++ {
		if completed[i].batch < completed[i-1].batch {
			t.Fatalf("completed batches out of order: %#v", completed)
		}
	}
	want := map[tuple]bool{
		{1, 0, "edge01", "show one"}: true,
		{1, 1, "edge02", "show one"}: true,
		{2, 0, "edge01", "show two"}: true,
		{2, 1, "edge02", "show two"}: true,
	}
	got := make(map[tuple]bool)
	for _, event := range completed {
		got[event] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("completed = %#v, want entries %#v", completed, want)
	}
}

type tuple struct {
	batch   int
	index   int
	host    string
	command string
}

func collectEvents(ch <-chan FanoutEvent) []FanoutEvent {
	var events []FanoutEvent
	for event := range ch {
		events = append(events, event)
	}
	return events
}

type trackingRunner struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delays    map[string]time.Duration
	fail      map[string]error
}

func (r *trackingRunner) RunCommand(ctx context.Context, host, command string) CommandResult {
	r.mu.Lock()
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()

	if delay := r.delays[host]; delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}

	r.mu.Lock()
	r.active--
	r.mu.Unlock()

	if err := r.fail[host]; err != nil {
		return CommandResult{Host: host, Command: command, Err: err}
	}
	return CommandResult{Host: host, Command: command, Output: []byte(host + "\n")}
}

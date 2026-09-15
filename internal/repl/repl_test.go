package repl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	s, err := Parse("[ 'edge(1,2)', 'user@host' ] ( 'show version', 'show env' )")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Targets, []Target{{Value: "edge1"}, {Value: "edge2"}, {Value: "user@host"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
	if got, want := s.Commands, []string{"show version", "show env"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v", got)
	}
}
func TestParseRejectsMixedSelector(t *testing.T) {
	if _, err := Parse("[ 'select:group:edge', 'edge1' ] ( 'show version' )"); err == nil {
		t.Fatal("Parse succeeded")
	}
}
func TestExecutorFailureSkipsOnlyThatHost(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	e := Executor{Concurrency: 2, Resolve: func(_ context.Context, target Target) ([]ResolvedTarget, error) {
		return []ResolvedTarget{{Identity: target.Value}}, nil
	}, Run: func(_ context.Context, target ResolvedTarget, command []string) Result {
		mu.Lock()
		calls = append(calls, target.Identity+":"+command[0])
		mu.Unlock()
		if target.Identity == "a" && command[0] == "one" {
			return Result{Err: errors.New("failed")}
		}
		return Result{}
	}}
	events, err := e.Execute(context.Background(), Submission{Targets: []Target{{Value: "a"}, {Value: "b"}}, Commands: []string{"one", "two"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %#v", calls)
	}
	for _, call := range calls {
		if call == "a:two" {
			t.Fatal("failed host ran subsequent command")
		}
	}
	found := false
	for _, event := range events {
		if event.Target.Identity == "a" && event.Command == "two" && event.State == Skipped {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %#v", events)
	}
}
func TestExecutorDeduplicatesAndSortsSelector(t *testing.T) {
	var queued []string
	e := Executor{Concurrency: 1, Resolve: func(_ context.Context, _ Target) ([]ResolvedTarget, error) {
		return []ResolvedTarget{{Identity: "z"}, {Identity: "a"}, {Identity: "z"}}, nil
	}, Run: func(_ context.Context, _ ResolvedTarget, _ []string) Result { return Result{} }, OnEvent: func(event Event) {
		if event.State == Queued {
			queued = append(queued, event.Target.Identity)
		}
	}}
	if _, err := e.Execute(context.Background(), Submission{Targets: []Target{{Value: "x", Selector: true}}, Commands: []string{"cmd"}}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "z"}; !reflect.DeepEqual(queued, want) {
		t.Fatalf("queued=%v want=%v", queued, want)
	}
}

func TestParserGrammarBoundaries(t *testing.T) {
	for _, line := range []string{
		"[ '' ] ( 'x' )", "[ 'a', ] ( 'x' )", "[ 'a' ] ( 'x', )",
		"[ 'a' ] ( '' )", "[ 'a((1,2)' ] ( 'x' )", "[ '(1,2)' ] ( 'x' )",
		"[ 'a(1, )' ] ( 'x' )", "[ 'a(1,2)(3,4)' ] ( 'x' )",
		"[ 'select: ' ] ( 'x' )", "[ 'a' ] ( 'x' ) extra",
	} {
		t.Run(line, func(t *testing.T) {
			if _, err := Parse(line); err == nil {
				t.Fatal("accepted malformed submission")
			}
		})
	}
	for _, command := range []string{`show \'quoted\'`, `printf \\path`} {
		s, err := Parse("[ 'a' ] ( '" + command + "' )")
		if err != nil {
			t.Fatalf("%q: %v", command, err)
		}
		want := strings.ReplaceAll(command, `\'`, `'`)
		if s.Commands[0] != want {
			t.Fatalf("got %q want %q", s.Commands[0], want)
		}
	}
}

func TestExecutorBoundsBeforeRunning(t *testing.T) {
	e := Executor{Concurrency: 4, Resolve: func(_ context.Context, target Target) ([]ResolvedTarget, error) {
		resolved := make([]ResolvedTarget, MaxSubmissionTargets+1)
		for i := range resolved {
			resolved[i].Identity = fmt.Sprint(i)
		}
		return resolved, nil
	}, Run: func(context.Context, ResolvedTarget, []string) Result {
		t.Error("ran rejected submission")
		return Result{}
	}}
	if _, err := e.Execute(context.Background(), Submission{Targets: []Target{{Value: "all"}}, Commands: []string{"x"}}); err == nil {
		t.Fatal("accepted too many targets")
	}
	if _, err := Parse(strings.Repeat("x", MaxSubmissionBytes+1)); err == nil {
		t.Fatal("accepted oversized line")
	}
}

func TestExecutorCancellationWaitsForWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 8)
	release := make(chan struct{})
	done := make(chan error, 1)
	var calls atomic.Int32
	e := Executor{Concurrency: 2, Resolve: func(_ context.Context, spec Target) ([]ResolvedTarget, error) {
		return []ResolvedTarget{{Identity: spec.Value}}, nil
	}, Run: func(ctx context.Context, target ResolvedTarget, _ []string) Result {
		calls.Add(1)
		started <- target.Identity
		<-ctx.Done()
		<-release
		return Result{Err: ctx.Err()}
	}}
	go func() {
		events, err := e.Execute(ctx, Submission{Targets: []Target{{Value: "a"}, {Value: "b"}, {Value: "c"}, {Value: "d"}}, Commands: []string{"one", "two"}})
		terminal := 0
		for _, event := range events {
			if event.State == Canceled || event.State == Skipped {
				terminal++
			}
		}
		if terminal != 8 {
			t.Errorf("terminal events=%d want 8", terminal)
		}
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("worker concurrency exceeded")
	default:
	}
	cancel()
	select {
	case <-done:
		t.Fatal("returned before command cleanup")
	default:
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("workers did not stop")
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
}

func TestExecutorDeliversBoundedOutputWithoutRetainingIt(t *testing.T) {
	var terminal Event
	e := Executor{Concurrency: 1, Resolve: func(context.Context, Target) ([]ResolvedTarget, error) { return []ResolvedTarget{{Identity: "a"}}, nil }, Run: func(context.Context, ResolvedTarget, []string) Result {
		return Result{Stdout: bytes.Repeat([]byte("x"), MaxCommandOutput+1), ExitCode: 7}
	}, OnEvent: func(event Event) {
		if event.State == Failed {
			terminal = event
		}
	}}
	events, err := e.Execute(context.Background(), Submission{Targets: []Target{{Value: "a"}}, Commands: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !terminal.Result.Truncated || len(terminal.Result.Stdout) != MaxCommandOutput || terminal.Result.ExitCode != 7 {
		t.Fatalf("incorrect bounded result: length=%d truncated=%v exit=%d", len(terminal.Result.Stdout), terminal.Result.Truncated, terminal.Result.ExitCode)
	}
	for _, event := range events {
		if event.Result.Stdout != nil || event.Result.Stderr != nil {
			t.Fatal("retained command output in metadata")
		}
	}
}

func TestParseBoundsExpandedSubmission(t *testing.T) {
	prefix := strings.Repeat("x", MaxSubmissionBytes/4)
	for _, targets := range []string{
		"'" + prefix + "(1,2,3,4,5)'",
		"'" + prefix + "(1,2,3)', '" + prefix + "(4,5,6)'",
	} {
		line := "[ " + targets + " ] ( 'show version' )"
		if len(line) >= MaxSubmissionBytes {
			t.Fatal("fixture must fit before expansion")
		}
		if _, err := Parse(line); err == nil || !strings.Contains(err.Error(), "expanded submission") {
			t.Fatalf("accepted oversized expansion: %v", err)
		}
	}
	if _, err := expandTarget("edge("+strings.Repeat("1,", MaxSubmissionTargets)+"2)", MaxSubmissionTargets, MaxSubmissionBytes); err == nil {
		t.Fatal("expanded more than the target limit")
	}
	if _, err := Parse("[ 'edge(" + strings.Repeat("1,", MaxSubmissionTargets-1) + "2)' ] ( 'show version' )"); err != nil {
		t.Fatalf("rejected expansion at the target limit: %v", err)
	}
}

func TestExecutorOrdersResultsAndWaitsBetweenCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	secondDone := make(chan struct{})
	var printed []string
	var printedCount atomic.Int32
	e := Executor{Concurrency: 2,
		Resolve: func(_ context.Context, target Target) ([]ResolvedTarget, error) {
			return []ResolvedTarget{{Identity: target.Value}}, nil
		},
		Run: func(ctx context.Context, target ResolvedTarget, commands []string) Result {
			if commands[0] == "one" {
				switch target.Identity {
				case "a":
					select {
					case <-secondDone:
					case <-ctx.Done():
						return Result{Err: ctx.Err()}
					}
				case "b":
					close(secondDone)
				}
			} else if printedCount.Load() < 3 {
				t.Error("next command started before all previous results were printed")
			}
			return Result{Stdout: []byte(target.Identity)}
		},
		OnEvent: func(event Event) {
			if event.State == Completed {
				printed = append(printed, event.Command+":"+event.Target.Identity)
				printedCount.Add(1)
			}
		},
	}
	_, err := e.Execute(ctx, Submission{Targets: []Target{{Value: "a"}, {Value: "b"}, {Value: "c"}}, Commands: []string{"one", "two"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one:a", "one:b", "one:c", "two:a", "two:b", "two:c"}
	if !reflect.DeepEqual(printed, want) {
		t.Fatalf("printed=%v want=%v", printed, want)
	}
}

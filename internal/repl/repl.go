// Package repl contains the UI-independent parser and multi-host scheduler for nssh repl.
package repl

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	DefaultConcurrency    = 4
	MaxCommandOutput      = 8 << 20
	MaxSessionOutput      = 32 << 20
	MaxSubmissionBytes    = 2 << 20
	MaxSubmissionTargets  = 1000
	MaxSubmissionCommands = 100
	MaxSubmissionJobs     = 10000
)

type Target struct {
	Value    string
	Selector bool
}
type Submission struct {
	Targets  []Target
	Commands []string
}

func Parse(line string) (Submission, error) {
	if len(line) > MaxSubmissionBytes {
		return Submission{}, fmt.Errorf("submission exceeds %d bytes", MaxSubmissionBytes)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return Submission{}, fmt.Errorf("submission is empty")
	}
	if !strings.HasPrefix(line, "[") {
		return Submission{}, fmt.Errorf("targets must start with [")
	}
	targetEnd, err := matching(line, 0, ']')
	if err != nil {
		return Submission{}, err
	}
	rest := strings.TrimSpace(line[targetEnd+1:])
	if !strings.HasPrefix(rest, "(") {
		return Submission{}, fmt.Errorf("commands must follow targets in (...)")
	}
	commandEnd, err := matching(rest, 0, ')')
	if err != nil {
		return Submission{}, err
	}
	if strings.TrimSpace(rest[commandEnd+1:]) != "" {
		return Submission{}, fmt.Errorf("unexpected text after commands")
	}
	targets, err := quotedList(line[1:targetEnd])
	if err != nil {
		return Submission{}, fmt.Errorf("targets: %w", err)
	}
	commands, err := quotedList(rest[1:commandEnd])
	if err != nil {
		return Submission{}, fmt.Errorf("commands: %w", err)
	}
	if len(targets) > MaxSubmissionTargets || len(commands) > MaxSubmissionCommands {
		return Submission{}, fmt.Errorf("submission exceeds REPL limits")
	}
	if len(targets) == 0 || len(commands) == 0 {
		return Submission{}, fmt.Errorf("targets and commands are required")
	}
	out := Submission{Commands: commands}
	for _, target := range targets {
		if strings.HasPrefix(target, "select:") {
			if len(targets) != 1 {
				return Submission{}, fmt.Errorf("a selector cannot be mixed with explicit targets")
			}
			query := strings.TrimSpace(strings.TrimPrefix(target, "select:"))
			if query == "" {
				return Submission{}, fmt.Errorf("selector is empty")
			}
			return Submission{Targets: []Target{{Value: query, Selector: true}}, Commands: commands}, nil
		}
		expanded, err := expandTarget(target)
		if err != nil {
			return Submission{}, err
		}
		if len(out.Targets)+len(expanded) > MaxSubmissionTargets {
			return Submission{}, fmt.Errorf("submission exceeds target limit")
		}
		for _, value := range expanded {
			out.Targets = append(out.Targets, Target{Value: value})
		}
	}
	return out, nil
}

func matching(s string, start int, close byte) (int, error) {
	quoted, escaped := false, false
	for i := start + 1; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		if s[i] == '\\' && quoted && i+1 < len(s) && s[i+1] == '\'' {
			escaped = true
			continue
		}
		if s[i] == '\'' {
			quoted = !quoted
			continue
		}
		if !quoted && s[i] == close {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unterminated group")
}

func quotedList(s string) ([]string, error) {
	var out []string
	for len(strings.TrimSpace(s)) > 0 {
		s = strings.TrimSpace(s)
		if s[0] != '\'' {
			return nil, fmt.Errorf("values must be single quoted")
		}
		var b strings.Builder
		i, closed := 1, false
		for ; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			if s[i] == '\'' {
				closed = true
				i++
				break
			}
			b.WriteByte(s[i])
		}
		if !closed {
			return nil, fmt.Errorf("unterminated quoted value")
		}
		if b.Len() == 0 {
			return nil, fmt.Errorf("empty values are not allowed")
		}
		out = append(out, b.String())
		s = strings.TrimSpace(s[i:])
		if s == "" {
			break
		}
		if s[0] != ',' {
			return nil, fmt.Errorf("expected comma")
		}
		s = s[1:]
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("trailing comma")
		}
	}
	return out, nil
}

var suffixExpansion = regexp.MustCompile(`^(.*)\(([^(),]+(?:,[^(),]+)*)\)$`)

func expandTarget(value string) ([]string, error) {
	m := suffixExpansion.FindStringSubmatch(value)
	if m == nil {
		if strings.ContainsAny(value, "()") {
			return nil, fmt.Errorf("invalid target expansion %q", value)
		}
		return []string{value}, nil
	}
	if m[1] == "" || strings.ContainsAny(m[1], "()") {
		return nil, fmt.Errorf("invalid target expansion %q", value)
	}
	parts := strings.Split(m[2], ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty target suffix")
		}
		out = append(out, m[1]+part)
	}
	return out, nil
}

type ResolvedTarget struct {
	Identity string
	Value    any
}
type Resolver func(context.Context, Target) ([]ResolvedTarget, error)
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
	Err            error
	Truncated      bool
}
type Runner func(context.Context, ResolvedTarget, []string) Result
type State string

const (
	Queued    State = "queued"
	Running   State = "running"
	Completed State = "completed"
	Failed    State = "failed"
	Canceled  State = "canceled"
	Skipped   State = "skipped"
)

type Event struct {
	Target       ResolvedTarget
	Command      string
	CommandIndex int
	State        State
	Result       Result
	Err          error
}
type Executor struct {
	Concurrency int
	Resolve     Resolver
	Run         Runner
	OnEvent     func(Event)
	OnTargets   func([]ResolvedTarget)
}

func (e Executor) Execute(ctx context.Context, submission Submission) ([]Event, error) {
	if e.Resolve == nil || e.Run == nil {
		return nil, fmt.Errorf("repl resolver and runner are required")
	}
	if e.Concurrency < 1 {
		return nil, fmt.Errorf("concurrency must be at least one")
	}
	if len(submission.Targets) == 0 || len(submission.Commands) == 0 {
		return nil, fmt.Errorf("targets and commands are required")
	}
	if len(submission.Targets) > MaxSubmissionTargets || len(submission.Commands) > MaxSubmissionCommands {
		return nil, fmt.Errorf("submission exceeds REPL limits")
	}
	targets := make([]ResolvedTarget, 0)
	seen := map[string]bool{}
	for _, spec := range submission.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := e.Resolve(ctx, spec)
		if err != nil {
			return nil, err
		}
		if spec.Selector {
			sort.SliceStable(resolved, func(i, j int) bool { return resolved[i].Identity < resolved[j].Identity })
		}
		for _, target := range resolved {
			if target.Identity == "" {
				return nil, fmt.Errorf("resolved target has no identity")
			}
			if !seen[target.Identity] {
				if len(targets) == MaxSubmissionTargets {
					return nil, fmt.Errorf("submission exceeds target limit")
				}
				seen[target.Identity] = true
				targets = append(targets, target)
			}
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets resolved")
	}
	if len(targets) > MaxSubmissionTargets || len(submission.Commands) > MaxSubmissionCommands || len(targets)*len(submission.Commands) > MaxSubmissionJobs {
		return nil, fmt.Errorf("submission exceeds REPL limits")
	}
	if e.OnTargets != nil {
		e.OnTargets(targets)
	}
	// Retain only bounded status metadata; output is delivered synchronously once.
	events := make([]Event, 0, 3*len(targets)*len(submission.Commands))
	var mu sync.Mutex
	emit := func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.OnEvent != nil {
			e.OnEvent(event)
		}
		stored := event
		stored.Result.Stdout = nil
		stored.Result.Stderr = nil
		events = append(events, stored)
	}
	for _, target := range targets {
		for commandIndex, command := range submission.Commands {
			emit(Event{Target: target, Command: command, CommandIndex: commandIndex, State: Queued})
		}
	}
	jobs := make(chan ResolvedTarget, len(targets))
	for _, target := range targets {
		jobs <- target
	}
	close(jobs)
	workerCount := e.Concurrency
	if workerCount > len(targets) {
		workerCount = len(targets)
	}
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for target := range jobs {
			failed := false
			for commandIndex, command := range submission.Commands {
				if failed {
					emit(Event{Target: target, Command: command, CommandIndex: commandIndex, State: Skipped})
					continue
				}
				if ctx.Err() != nil {
					emit(Event{Target: target, Command: command, CommandIndex: commandIndex, State: Canceled, Err: ctx.Err()})
					failed = true
					continue
				}
				emit(Event{Target: target, Command: command, CommandIndex: commandIndex, State: Running})
				result := e.Run(ctx, target, []string{command})
				state := Completed
				if result.Err != nil || result.ExitCode != 0 {
					state = Failed
					failed = true
				}
				if ctx.Err() != nil {
					state = Canceled
					result.Err = ctx.Err()
					failed = true
				}
				emit(Event{Target: target, Command: command, CommandIndex: commandIndex, State: state, Result: limitResult(result), Err: result.Err})
			}
		}
	}
	wg.Add(workerCount)
	for range workerCount {
		go worker()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return append([]Event(nil), events...), ctx.Err()
}
func limitResult(result Result) Result {
	total := len(result.Stdout) + len(result.Stderr)
	if total <= MaxCommandOutput {
		return result
	}
	remain := MaxCommandOutput
	if len(result.Stdout) > remain {
		result.Stdout = result.Stdout[:remain]
		result.Stderr = nil
	} else {
		remain -= len(result.Stdout)
		result.Stderr = result.Stderr[:remain]
	}
	result.Truncated = true
	return result
}

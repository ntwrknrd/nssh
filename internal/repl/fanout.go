package repl

import (
	"context"
	"sync"
)

const DefaultConcurrency = 12

type CommandResult struct {
	Host     string
	Command  string
	Output   []byte
	ExitCode int
	Err      error
}

type CommandRunner interface {
	RunCommand(ctx context.Context, host, command string) CommandResult
}

type FanoutEventKind int

const (
	FanoutStarted FanoutEventKind = iota
	FanoutCompleted
)

type FanoutEvent struct {
	Kind    FanoutEventKind
	Batch   int
	Index   int
	Host    string
	Command string
	Result  CommandResult
}

func ExecuteFanout(ctx context.Context, targets []string, command string, concurrency int, runner CommandRunner) []CommandResult {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	results := make([]CommandResult, len(targets))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, target := range targets {
		i, target := i, target
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ctx.Err() != nil {
				results[i] = CommandResult{Host: target, Command: command, Err: ctx.Err()}
				return
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = CommandResult{Host: target, Command: command, Err: ctx.Err()}
				return
			}
			results[i] = runner.RunCommand(ctx, target, command)
			results[i].Host = target
			results[i].Command = command
		}()
	}
	wg.Wait()
	return results
}

func ExecuteFanoutStream(ctx context.Context, targets []string, command string, concurrency int, runner CommandRunner) <-chan FanoutEvent {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	events := make(chan FanoutEvent, len(targets)*2)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, target := range targets {
		i, target := i, target
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ctx.Err() != nil {
				events <- FanoutEvent{
					Kind:    FanoutCompleted,
					Batch:   1,
					Index:   i,
					Host:    target,
					Command: command,
					Result:  CommandResult{Host: target, Command: command, Err: ctx.Err()},
				}
				return
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				events <- FanoutEvent{
					Kind:    FanoutCompleted,
					Batch:   1,
					Index:   i,
					Host:    target,
					Command: command,
					Result:  CommandResult{Host: target, Command: command, Err: ctx.Err()},
				}
				return
			}
			events <- FanoutEvent{Kind: FanoutStarted, Batch: 1, Index: i, Host: target, Command: command}
			result := runner.RunCommand(ctx, target, command)
			result.Host = target
			result.Command = command
			events <- FanoutEvent{Kind: FanoutCompleted, Batch: 1, Index: i, Host: target, Command: command, Result: result}
		}()
	}
	go func() {
		wg.Wait()
		close(events)
	}()
	return events
}

func ExecuteCommandFanoutStream(ctx context.Context, targets []string, commands []string, concurrency int, runner CommandRunner) <-chan FanoutEvent {
	events := make(chan FanoutEvent, len(targets)*len(commands)*2)
	go func() {
		defer close(events)
		for commandIndex, command := range commands {
			for event := range ExecuteFanoutStream(ctx, targets, command, concurrency, runner) {
				event.Batch = commandIndex + 1
				events <- event
			}
		}
	}()
	return events
}

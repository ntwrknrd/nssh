package repl

import (
	"context"
	"io"
)

type Options struct {
	Concurrency  int
	In           io.Reader
	Out          io.Writer
	Resolver     TargetResolver
	Runner       CommandRunner
	History      HistoryStore
	Diff         bool
	PromptCursor PromptCursor
}

type PromptCursor string

const (
	PromptCursorPipe       PromptCursor = "pipe"
	PromptCursorUnderscore PromptCursor = "underscore"
)

type HistoryStore interface {
	Load() ([]string, error)
	Append(line string) error
}

type SSHCommandRunner struct{}

func (SSHCommandRunner) RunCommand(ctx context.Context, host, command string) CommandResult {
	out, code, err := runRemoteCommandCapture(ctx, host, command)
	return CommandResult{Host: host, Command: command, Output: out, ExitCode: code, Err: err}
}

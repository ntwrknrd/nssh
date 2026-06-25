package captured

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

type Request struct {
	Hostname      string
	Username      string
	Port          int
	SSHOptions    config.SSHHostConfig
	SSHVerbosity  int
	SSHArgs       []string
	RemoteCommand []string
	Timeout       time.Duration
	Env           []string
}

type Command struct {
	Name string
	Args []string
	Env  []string
}

type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

type OutputEvent struct {
	Stream Stream
	Data   []byte
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	Output   []OutputEvent
	ExitCode int
	Err      error
}

type Runner struct {
	Exec func(context.Context, Command) Result
}

func (r Runner) Run(ctx context.Context, req Request) (Result, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	execFn := r.Exec
	if execFn == nil {
		execFn = defaultExec
	}
	argsTimer := connector.StartTiming(connector.TimingSSHArgsBuild)
	args := buildOpenSSHArgs(req)
	argsTimer.Emit()

	result := execFn(ctx, Command{
		Name: "ssh",
		Args: args,
		Env:  req.Env,
	})
	if result.Err == nil {
		return result, nil
	}
	if result.ExitCode != 0 {
		return result, &exit.ExitError{
			Code:    result.ExitCode,
			Message: fmt.Sprintf("ssh exited with code %d", result.ExitCode),
			Cause:   result.Err,
		}
	}
	return result, fmt.Errorf("ssh exec: %w", result.Err)
}

func defaultExec(ctx context.Context, command Command) Result {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	if len(command.Env) > 0 {
		cmd.Env = append(os.Environ(), command.Env...)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Err: err}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{Err: err}
	}

	var stdout, stderr bytes.Buffer
	totalTimer := connector.StartTiming(connector.TimingSSHProcessTotal)
	startTimer := connector.StartTiming(connector.TimingSSHProcessStart)
	if err := cmd.Start(); err != nil {
		startTimer.Emit()
		totalTimer.Emit()
		return Result{Err: err}
	}
	startTimer.Emit()

	events := make(chan OutputEvent, 16)
	readErrs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readErrs <- readOutputEvents(StreamStdout, stdoutPipe, &stdout, events)
	}()
	go func() {
		defer wg.Done()
		readErrs <- readOutputEvents(StreamStderr, stderrPipe, &stderr, events)
	}()
	go func() {
		wg.Wait()
		close(events)
	}()

	var output []OutputEvent
	waitTimer := connector.StartTiming(connector.TimingSSHProcessWait)
	for event := range events {
		output = append(output, event)
	}
	err = cmd.Wait()
	for range 2 {
		if readErr := <-readErrs; readErr != nil && err == nil {
			err = readErr
		}
	}
	waitTimer.Emit()
	totalTimer.Emit()
	result := Result{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
		Output: output,
		Err:    err,
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	}
	return result
}

func readOutputEvents(stream Stream, r io.Reader, sink *bytes.Buffer, events chan<- OutputEvent) error {
	reader := bufio.NewReader(r)
	for {
		data, err := reader.ReadBytes('\n')
		if len(data) > 0 {
			if _, writeErr := sink.Write(data); writeErr != nil {
				return writeErr
			}
			chunk := append([]byte(nil), data...)
			events <- OutputEvent{Stream: stream, Data: chunk}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func buildOpenSSHArgs(req Request) []string {
	args := connector.RenderSSHOptions(req.SSHOptions, req.SSHVerbosity)
	if hasAskpassEnv(req.Env) {
		args = append(args, "-o", "NumberOfPasswordPrompts=1")
	} else {
		args = append(args, "-o", "BatchMode=yes")
	}
	if req.Timeout > 0 {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", int(req.Timeout.Seconds())))
	}
	if req.Port != 0 && req.Port != 22 && explicitPort(req.SSHArgs) == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, req.SSHArgs...)

	target := req.Hostname
	if req.Username != "" {
		target = req.Username + "@" + target
	}
	args = append(args, target)
	args = append(args, req.RemoteCommand...)
	return args
}

func explicitPort(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if arg == "-p" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "-p") && len(arg) > 2 {
			return arg[2:]
		}
		if arg == "-o" && i+1 < len(args) {
			next := strings.ToLower(args[i+1])
			if strings.HasPrefix(next, "port=") {
				return args[i+1][5:]
			}
			i++
		}
	}
	return ""
}

func hasAskpassEnv(env []string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, "SSH_ASKPASS=") {
			return true
		}
	}
	return false
}

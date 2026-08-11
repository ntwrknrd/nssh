package captured

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
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
	Stdin         io.Reader
}

type Command struct {
	Name  string
	Args  []string
	Env   []string
	Stdin io.Reader
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

	stdin := req.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	result := execFn(ctx, Command{
		Name:  "ssh",
		Args:  args,
		Env:   req.Env,
		Stdin: stdin,
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
	if command.Stdin != nil {
		cmd.Stdin = command.Stdin
	}
	// Manage the output pipes here instead of using StdoutPipe/StderrPipe:
	// cmd.Wait closes those as soon as the process exits, racing the reader
	// goroutines and dropping buffered output.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return Result{Err: err}
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		return Result{Err: err}
	}
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stderrWrite

	var stdout, stderr bytes.Buffer
	totalTimer := connector.StartTiming(connector.TimingSSHProcessTotal)
	startTimer := connector.StartTiming(connector.TimingSSHProcessStart)
	if err := cmd.Start(); err != nil {
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		_ = stderrRead.Close()
		_ = stderrWrite.Close()
		startTimer.Emit()
		totalTimer.Emit()
		return Result{Err: err}
	}
	startTimer.Emit()
	// The child holds duplicates of the write ends; close ours so the
	// readers see EOF when the child exits.
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()

	events := make(chan OutputEvent, 16)
	readErrs := make(chan error, 2)
	outputDone := make(chan []OutputEvent, 1)
	go func() {
		var output []OutputEvent
		for event := range events {
			output = append(output, event)
		}
		outputDone <- output
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readErrs <- readOutputEvents(StreamStdout, stdoutRead, &stdout, events)
	}()
	go func() {
		defer wg.Done()
		readErrs <- readOutputEvents(StreamStderr, stderrRead, &stderr, events)
	}()

	waitTimer := connector.StartTiming(connector.TimingSSHProcessWait)
	err = cmd.Wait()
	// The child has exited, so everything it wrote is either consumed or
	// sitting in the kernel pipe buffers. Interrupt the readers so they
	// don't block on write ends inherited by background children, then
	// salvage the buffered remainder synchronously.
	past := time.Unix(0, 1)
	_ = stdoutRead.SetReadDeadline(past)
	_ = stderrRead.SetReadDeadline(past)
	wg.Wait()
	drainBuffered(stdoutRead, &stdout, StreamStdout, events)
	drainBuffered(stderrRead, &stderr, StreamStderr, events)
	_ = stdoutRead.Close()
	_ = stderrRead.Close()
	close(events)
	output := <-outputDone
	for range 2 {
		readErr := <-readErrs
		if readErr != nil && err == nil && !errors.Is(readErr, os.ErrDeadlineExceeded) {
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

// drainLimit bounds how much buffered pipe output drainBuffered salvages
// after the child exits. The child's unread output can never exceed the
// kernel pipe buffer, which is far smaller; the limit only cuts off
// background children that keep writing after the child exited.
const drainLimit = 1 << 20

// drainBuffered reads whatever is already buffered in the pipe without
// blocking, appending it to the sink and event stream in order after the
// interrupted reader goroutine's output.
func drainBuffered(f *os.File, sink *bytes.Buffer, stream Stream, events chan<- OutputEvent) {
	conn, err := f.SyscallConn()
	if err != nil {
		return
	}
	total := 0
	_ = conn.Control(func(fd uintptr) {
		buf := make([]byte, 32*1024)
		for total < drainLimit {
			n, readErr := syscall.Read(int(fd), buf)
			if n > 0 {
				total += n
				chunk := append([]byte(nil), buf[:n]...)
				sink.Write(chunk)
				events <- OutputEvent{Stream: stream, Data: chunk}
			}
			if readErr == syscall.EINTR {
				continue
			}
			if readErr != nil || n <= 0 {
				return
			}
		}
	})
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
	pinnedOptions, sshArgs := connector.SplitPinnedHostKeyOptions(req.SSHArgs)
	args := append([]string{}, pinnedOptions...)
	args = append(args, connector.RenderSSHOptions(req.SSHOptions, req.SSHVerbosity)...)
	if hasAskpassEnv(req.Env) {
		args = append(args, "-o", "NumberOfPasswordPrompts=1")
	} else {
		args = append(args, "-o", "BatchMode=yes")
	}
	if req.Timeout > 0 {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", int(req.Timeout.Seconds())))
	}
	if req.Port != 0 && req.Port != 22 && explicitPort(sshArgs) == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}
	args = append(args, sshArgs...)

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

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
	Hostname       string
	Username       string
	Port           int
	SSHOptions     config.SSHHostConfig
	SSHVerbosity   int
	SSHArgs        []string
	RemoteCommand  []string
	ConnectTimeout time.Duration
	Env            []string
	Stdin          io.Reader
	MaxOutputBytes int // Zero preserves unbounded root-command capture.
}

type Command struct {
	Name           string
	Args           []string
	Env            []string
	Stdin          io.Reader
	MaxOutputBytes int
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
	Stdout    []byte
	Stderr    []byte
	Output    []OutputEvent
	ExitCode  int
	Err       error
	Truncated bool
}

type Runner struct {
	Exec func(context.Context, Command) Result
}

func (r Runner) Run(ctx context.Context, req Request) (Result, error) {
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
		Name:           "ssh",
		Args:           args,
		Env:            req.Env,
		Stdin:          stdin,
		MaxOutputBytes: req.MaxOutputBytes,
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

	output := &outputBuffer{limit: command.MaxOutputBytes}
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

	readErrs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readErrs <- readOutputEvents(StreamStdout, stdoutRead, output)
	}()
	go func() {
		defer wg.Done()
		readErrs <- readOutputEvents(StreamStderr, stderrRead, output)
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
	drainBuffered(stdoutRead, StreamStdout, output)
	drainBuffered(stderrRead, StreamStderr, output)
	_ = stdoutRead.Close()
	_ = stderrRead.Close()
	for range 2 {
		readErr := <-readErrs
		if readErr != nil && err == nil && !errors.Is(readErr, os.ErrDeadlineExceeded) {
			err = readErr
		}
	}
	waitTimer.Emit()
	totalTimer.Emit()
	result := Result{
		Stdout:    output.stdout.Bytes(),
		Stderr:    output.stderr.Bytes(),
		Output:    output.events,
		Truncated: output.truncated,
		Err:       err,
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
func drainBuffered(f *os.File, stream Stream, output *outputBuffer) {
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
				output.append(stream, buf[:n])
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

// outputBuffer serializes reader observations. Bounded consumers retain each byte
// only once; root commands also retain event order for existing presentation.
type outputBuffer struct {
	mu             sync.Mutex
	stdout, stderr bytes.Buffer
	events         []OutputEvent
	limit          int
	truncated      bool
}

func (b *outputBuffer) append(stream Stream, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 {
		left := max(0, b.limit-b.stdout.Len()-b.stderr.Len())
		if len(data) > left {
			b.truncated = true
			data = data[:left]
		}
	}
	if len(data) == 0 {
		return
	}
	if stream == StreamStdout {
		b.stdout.Write(data)
	} else {
		b.stderr.Write(data)
	}
	if b.limit <= 0 {
		b.events = append(b.events, OutputEvent{Stream: stream, Data: append([]byte(nil), data...)})
	}
}

func readOutputEvents(stream Stream, r io.Reader, output *outputBuffer) error {
	reader := bufio.NewReader(r)
	for {
		// ReadSlice keeps even an unterminated multi-gigabyte line bounded.
		data, err := reader.ReadSlice('\n')
		output.append(stream, data)
		if err == io.EOF {
			return nil
		}
		if err != nil && err != bufio.ErrBufferFull {
			return err
		}
	}
}

func buildOpenSSHArgs(req Request) []string {
	pinnedOptions, sshArgs := connector.SplitPinnedHostKeyOptions(req.SSHArgs)
	args := connector.ComposeSSHOptions(connector.SSHOptionPlan{
		Enforced:     pinnedOptions,
		Runtime:      sshArgs,
		Resolved:     req.SSHOptions,
		SSHVerbosity: req.SSHVerbosity,
	})
	if hasAskpassEnv(req.Env) {
		if connector.EffectiveSSHOption(args, "NumberOfPasswordPrompts") == "" {
			args = append(args, "-o", "NumberOfPasswordPrompts=1")
		}
	} else if connector.EffectiveSSHOption(args, "BatchMode") == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	if req.ConnectTimeout > 0 && connector.EffectiveSSHOption(args, "ConnectTimeout") == "" {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", int(req.ConnectTimeout.Seconds())))
	}
	if req.Port != 0 && req.Port != 22 && connector.EffectiveSSHOption(args, "Port") == "" {
		args = append(args, "-p", fmt.Sprintf("%d", req.Port))
	}

	target := req.Hostname
	if req.Username != "" {
		target = req.Username + "@" + target
	}
	args = append(args, target)
	args = append(args, req.RemoteCommand...)
	return args
}

func hasAskpassEnv(env []string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, "SSH_ASKPASS=") {
			return true
		}
	}
	return false
}

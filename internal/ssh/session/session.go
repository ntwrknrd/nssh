// Package session owns a foreground OpenSSH terminal for one TUI pane.
package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"golang.org/x/term"
)

type Options struct {
	Width, Height int
	Output        func([]byte)
}

type Session struct {
	input      *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	readerDone chan struct{}
	writerDone chan struct{}
	writes     chan []byte
	mu         sync.Mutex
	err        error
}

// Start passes terminal bytes unchanged. It does not identify prompts, replace
// shells, run initialization commands, or infer command completion.
func Start(ctx context.Context, req captured.Request, opts Options) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	req.RemoteCommand = nil
	req.Env = append(req.Env, "TERM=xterm-256color")
	pinned, rest := connector.SplitPinnedHostKeyOptions(req.SSHArgs)
	req.SSHArgs = append(append(pinned, "-tt"), rest...)
	command := captured.BuildCommand(req)
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Env = append(os.Environ(), command.Env...)
	master, slave, err := pty.Open()
	if err != nil {
		cancel()
		return nil, err
	}
	fail := func(err error) (*Session, error) { cancel(); master.Close(); slave.Close(); return nil, err }
	if _, err = term.MakeRaw(int(slave.Fd())); err != nil {
		return fail(err)
	}
	if err = pty.Setsize(master, &pty.Winsize{Rows: uint16(max(1, min(opts.Height, 65535))), Cols: uint16(max(1, min(opts.Width, 65535)))}); err != nil {
		return fail(err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if err = cmd.Start(); err != nil {
		return fail(err)
	}
	slave.Close()
	s := &Session{input: master, cancel: cancel, done: make(chan struct{}), readerDone: make(chan struct{}), writerDone: make(chan struct{}), writes: make(chan []byte, 32)}
	go func() {
		defer close(s.readerDone)
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			if n > 0 && opts.Output != nil {
				opts.Output(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer close(s.writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-s.writes:
				if _, err := master.Write(data); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		cancel()
		close(s.done)
	}()
	return s, nil
}

// Send queues a bounded input event. A slow/dead pane fails visibly instead of
// silently dropping keys or reconnecting and replaying them.
func (s *Session) Send(data []byte) error {
	if len(data) > 1<<20 {
		return errors.New("terminal input exceeds 1 MiB")
	}
	if !s.Alive() {
		return errors.New("SSH session is closed")
	}
	select {
	case s.writes <- append([]byte(nil), data...):
		return nil
	default:
		return errors.New("terminal input queue is full")
	}
}
func (s *Session) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
func (s *Session) Resize(w, h int) {
	_ = pty.Setsize(s.input, &pty.Winsize{Rows: uint16(max(1, min(h, 65535))), Cols: uint16(max(1, min(w, 65535)))})
}
func (s *Session) Wait() error {
	<-s.done
	select {
	case <-s.readerDone:
	case <-time.After(250 * time.Millisecond):
		s.input.Close()
		<-s.readerDone
	}
	s.input.Close()
	<-s.writerDone
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
func (s *Session) Close() { s.cancel(); s.input.Close(); _ = s.Wait() }

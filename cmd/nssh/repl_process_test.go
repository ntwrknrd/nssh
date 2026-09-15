package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// These fixtures exercise the real binary and terminal, without network access
// or external credentials. Run them inside Linux when developing on macOS.
func TestREPLProcess(t *testing.T) {
	binary := buildBinary(t)
	t.Run("explanation exits before session setup", func(t *testing.T) {
		for _, flag := range []string{"--explain", "-e"} {
			f := newREPLFixture(t, binary)
			cmd := f.command(flag, "--plain", "--concurrency=0")
			cmd.Stdin = strings.NewReader("invalid submission\n")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", flag, err, output)
			}
			if !strings.Contains(string(output), "Syntax:") || !strings.Contains(string(output), "[ 'host1', 'host2' ]") {
				t.Fatalf("%s omitted REPL syntax: %s", flag, output)
			}
			if f.log() != "" {
				t.Fatal("explanation started SSH")
			}
		}
	})
	t.Run("plain streams and remote EOF", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		cmd := f.command("--plain")
		cmd.Stdin = strings.NewReader("[ 'good' ] ( 'one' )\n[ 'good' ] ( 'two' )\n")
		var out, errs bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errs
		if err := cmd.Run(); err != nil {
			t.Fatalf("run: %v\n%s", err, errs.String())
		}
		if !strings.Contains(out.String(), "stdout-good-one") || !strings.Contains(out.String(), "stdout-good-two") || strings.Contains(out.String(), "stderr-") {
			t.Fatalf("stdout=%q", out.String())
		}
		if !strings.Contains(errs.String(), "stderr-good-one") || strings.Contains(errs.String(), "stdout-") {
			t.Fatalf("stderr=%q", errs.String())
		}
		if strings.ContainsAny(out.String()+errs.String(), "\x1b") {
			t.Fatal("plain mode emitted terminal styling")
		}
		if _, err := os.Stat(filepath.Join(f.dir, "state", "nssh", "repl_history")); !os.IsNotExist(err) {
			t.Fatalf("plain history exists: %v", err)
		}
	})
	t.Run("failure isolates host and stops subsequent input", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		cmd := f.command("--plain")
		cmd.Stdin = strings.NewReader("[ 'bad', 'good' ] ( 'one', 'two' )\n[ 'good' ] ( 'after' )\n")
		output, err := cmd.CombinedOutput()
		if code := processExitCode(err); code != 1 {
			t.Fatalf("exit=%d\n%s", code, output)
		}
		log := f.log()
		for _, want := range []string{"bad one\n", "good one\n", "good two\n"} {
			if !strings.Contains(log, want) {
				t.Fatalf("log=%q lacks %q", log, want)
			}
		}
		if strings.Contains(log, "bad two") || strings.Contains(log, "after") {
			t.Fatalf("unexpected command: %q", log)
		}
		if !strings.Contains(string(output), "failed (exit 7)") || !strings.Contains(string(output), "two: skipped") {
			t.Fatalf("outcomes=%s", output)
		}
	})
	t.Run("plain active interrupt reaps SSH", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		cmd := f.command("--plain")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = input.Close() }()
		var output processOutput
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(input, "[ 'slow' ] ( 'hold', 'never' )\n")
		pid := f.awaitPID(t)
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		if code := processExitCode(cmd.Wait()); code != 130 {
			t.Fatalf("exit=%d\n%s", code, output.String())
		}
		assertProcessGone(t, pid)
		if strings.Contains(f.log(), "never") {
			t.Fatal("ran command after cancellation")
		}
	})
	t.Run("plain idle interrupt", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		cmd := f.command("--plain")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = input.Close() }()
		var output processOutput
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(input, ":help\n")
		awaitProcess(t, func() bool { return strings.Contains(output.String(), "Run grouped remote commands") }, "plain input readiness", &output)
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		if code := processExitCode(cmd.Wait()); code != 130 {
			t.Fatalf("exit=%d\n%s", code, output.String())
		}
	})
	t.Run("PTY guided inventory and multiple commands", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		s := f.terminal(t)
		s.write(t, "\x1bOQ") // Back to guided mode from the legacy test helper.
		s.await(t, "Filter:")
		s.write(t, "good\r")
		s.write(t, "one\rtwo")
		s.write(t, "\x1b[15~") // F5
		s.await(t, "stdout-good-two")
		s.await(t, "done 2")
		s.settle()
		if !strings.Contains(f.log(), "good one\ngood two\n") {
			t.Fatalf("commands not executed in order: %q", f.log())
		}
		s.write(t, "\x10") // Ctrl-P recalls the guided submission.
		s.write(t, "\x1b[15~")
		awaitProcess(t, func() bool { return strings.Count(f.log(), "good two\n") == 2 }, "guided history replay", &s.output)
		s.settle()
		s.write(t, "\x03")
		s.wait(t, 0)
	})
	t.Run("PTY completion history resize and quit", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		s := f.terminal(t)
		s.write(t, "[ 'go\t' ] ( 'one' )\r")
		s.await(t, "stdout-good-one")
		s.await(t, "done 1")
		s.settle()
		s.write(t, "\x1b[A\r")
		awaitProcess(t, func() bool { return strings.Count(f.log(), "good one\n") == 2 }, "history replay", &s.output)
		s.settle()
		if err := pty.Setsize(s.file, &pty.Winsize{Rows: 30, Cols: 100}); err != nil {
			t.Fatal(err)
		}
		s.write(t, "\x1b[5~\x1b[6~")
		s.settle()
		s.write(t, ":quit\r")
		s.wait(t, 0)
		history, err := os.ReadFile(filepath.Join(f.dir, "state", "nssh", "repl_history"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(history), "[ 'good' ] ( 'one' )") {
			t.Fatalf("history=%q", history)
		}
	})
	t.Run("PTY active cancel returns usable prompt", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		s := f.terminal(t)
		s.write(t, "[ 'slow' ] ( 'hold', 'never' )\r")
		pid := f.awaitPID(t)
		s.write(t, "\x03")
		s.await(t, "canceled")
		assertProcessGone(t, pid)
		s.settle()
		s.write(t, "[ 'good' ] ( 'after' )\r")
		s.await(t, "stdout-good-after")
		s.settle()
		s.write(t, "\x03")
		s.wait(t, 0)
		if strings.Contains(f.log(), "never") {
			t.Fatal("ran command after cancellation")
		}
	})
	t.Run("PTY trust cancellation leaves input usable", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		s := f.terminal(t)
		s.write(t, "[ 'trust' ] ( 'one' )\r")
		s.await(t, "Verify host key")
		s.write(t, "\x03")
		s.await(t, "canceled")
		s.settle()
		s.write(t, "[ 'good' ] ( 'after' )\r")
		s.await(t, "stdout-good-after")
		s.settle()
		s.write(t, "\x04")
		s.wait(t, 0)
		if strings.Contains(f.log(), "trust one") {
			t.Fatal("unapproved command ran")
		}
	})
	t.Run("PTY trust accept once", func(t *testing.T) {
		f := newREPLFixture(t, binary)
		s := f.terminal(t)
		s.write(t, "[ 'trust' ] ( 'one' )\r")
		s.await(t, "Verify host key")
		s.write(t, "o")
		s.await(t, "stdout-trust-one")
		s.settle()
		s.write(t, ":exit\r")
		s.wait(t, 0)
		if _, err := os.Stat(filepath.Join(f.dir, "home", ".ssh", "known_hosts")); !os.IsNotExist(err) {
			t.Fatalf("accept-once persisted trust: %v", err)
		}
	})
}

type replFixture struct {
	dir, binary string
	t           *testing.T
}

func newREPLFixture(t *testing.T, binary string) *replFixture {
	t.Helper()
	f := &replFixture{dir: t.TempDir(), binary: binary, t: t}
	for _, dir := range []string{"bin", "home", "config/nssh"} {
		if err := os.MkdirAll(filepath.Join(f.dir, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	var config strings.Builder
	config.WriteString("inventory:\n  providers:\n    local:\n      type: local\n      hosts:\n")
	for _, host := range []string{"good", "bad", "slow", "trust"} {
		_, _ = fmt.Fprintf(&config, "        %s:\n          auth:\n            mode: key\n            username: fixture\n", host)
	}
	writeProcessFile(t, filepath.Join(f.dir, "config", "nssh", "config.yaml"), config.String(), 0600)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
host=unknown
probe=no
for arg do
  case "$arg" in
    *PreferredAuthentications=none*) probe=yes ;;
    good|bad|slow|trust) host=$arg ;;
    fixture@good|fixture@bad|fixture@slow|fixture@trust) host=${arg#fixture@} ;;
  esac
  command=$arg
done
if [ "$probe" = yes ]; then
  if [ "$host" = trust ]; then
    printf '%s\n' 'debug1: Server host key: ssh-ed25519 FINGERPRINT' >&2
    printf '%s\n' 'Host key verification failed.' >&2
  else
    printf '%s\n' 'Permission denied' >&2
  fi
  exit 255
fi
printf '%s %s\n' "$host" "$command" >> "$NSSH_TEST_LOG"
if read -r unexpected; then
  printf '%s\n' 'remote stdin was not EOF' >&2
  exit 93
fi
if [ "$command" = hold ]; then
  printf '%s\n' "$$" > "$NSSH_TEST_PID"
  exec sleep 60
fi
printf 'stdout-%s-%s\n' "$host" "$command"
printf 'stderr-%s-%s\n' "$host" "$command" >&2
if [ "$host" = bad ] && [ "$command" = one ]; then exit 7; fi
`
	script = strings.ReplaceAll(script, "FINGERPRINT", ssh.FingerprintSHA256(key))
	writeProcessFile(t, filepath.Join(f.dir, "bin", "ssh"), script, 0700)
	writeProcessFile(t, filepath.Join(f.dir, "bin", "ssh-keyscan"), "#!/bin/sh\nprintf '%s\\n' 'trust "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))+"'\n", 0700)
	return f
}
func writeProcessFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}
func (f *replFixture) command(args ...string) *exec.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cmd := exec.CommandContext(ctx, f.binary, append([]string{"repl"}, args...)...)
	f.t.Cleanup(func() {
		cancel()
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	})
	cmd.Env = []string{"PATH=" + filepath.Join(f.dir, "bin") + ":/usr/bin:/bin", "HOME=" + filepath.Join(f.dir, "home"), "XDG_CONFIG_HOME=" + filepath.Join(f.dir, "config"), "XDG_DATA_HOME=" + filepath.Join(f.dir, "data"), "XDG_STATE_HOME=" + filepath.Join(f.dir, "state"), "TERM=xterm-256color", "NO_COLOR=1", "NSSH_TEST_LOG=" + filepath.Join(f.dir, "commands"), "NSSH_TEST_PID=" + filepath.Join(f.dir, "pid")}
	return cmd
}
func (f *replFixture) log() string {
	data, _ := os.ReadFile(filepath.Join(f.dir, "commands"))
	return string(data)
}
func (f *replFixture) awaitPID(t *testing.T) int {
	t.Helper()
	pid := 0
	awaitProcess(t, func() bool {
		data, err := os.ReadFile(filepath.Join(f.dir, "pid"))
		if err != nil {
			return false
		}
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		return pid > 0
	}, "SSH process start", nil)
	return pid
}
func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	awaitProcess(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, "SSH process cleanup", nil)
}
func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	return -1
}

type processOutput struct {
	mu sync.Mutex
	b  strings.Builder
}

func (o *processOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}
func (o *processOutput) String() string { o.mu.Lock(); defer o.mu.Unlock(); return o.b.String() }
func awaitProcess(t *testing.T, ready func() bool, description string, output *processOutput) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if output != nil {
		t.Fatalf("timed out waiting for %s\n%s", description, ansi.Strip(output.String()))
	}
	t.Fatalf("timed out waiting for %s", description)
}

type replTerminal struct {
	file     *os.File
	cmd      *exec.Cmd
	output   processOutput
	readDone chan struct{}
}

func (f *replFixture) terminal(t *testing.T) *replTerminal {
	t.Helper()
	s := &replTerminal{cmd: f.command(), readDone: make(chan struct{})}
	file, err := pty.StartWithSize(s.cmd, &pty.Winsize{Rows: 30, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	s.file = file
	go func() { _, _ = io.Copy(&s.output, file); close(s.readDone) }()
	t.Cleanup(func() { _ = file.Close(); <-s.readDone })
	s.await(t, "nssh repl")
	s.write(t, "\x1bOQ") // F2: existing cases exercise the syntax editor.
	return s
}
func (s *replTerminal) write(t *testing.T, text string) {
	t.Helper()
	if _, err := s.file.WriteString(text); err != nil {
		t.Fatal(err)
	}
}
func (s *replTerminal) await(t *testing.T, text string) {
	t.Helper()
	awaitProcess(t, func() bool { return strings.Contains(ansi.Strip(s.output.String()), text) }, text, &s.output)
}

// Allow the terminal's next render tick to finish after observing command output.
func (s *replTerminal) settle() { time.Sleep(100 * time.Millisecond) }
func (s *replTerminal) wait(t *testing.T, want int) {
	t.Helper()
	if code := processExitCode(s.cmd.Wait()); code != want {
		t.Fatalf("exit=%d want=%d\n%s", code, want, ansi.Strip(s.output.String()))
	}
}

// Package session owns a foreground OpenSSH shell for one TUI device.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"golang.org/x/term"
)

type Profile string

const (
	Auto  Profile = "auto"
	EOS   Profile = "eos"
	Junos Profile = "junos"
	Linux Profile = "linux"
)

func ValidProfile(p Profile) bool { return p == Auto || p == EOS || p == Junos || p == Linux }

var ErrCommand = errors.New("device reported command error")
var cliError = regexp.MustCompile(`(?mi)^(?:% (?:Invalid|Incomplete|Ambiguous|Unknown|Unrecognized|Error|Authorization)[^\n]*|error:[^\n]*|syntax error[^\n]*)$`)

type Result struct {
	Output                 []byte
	Prompt                 string
	ExitCode               int
	StatusKnown, Truncated bool
	Err                    error
}
type Options struct {
	Profile       Profile
	Width, Height int
	Limit         int
	Timeout       time.Duration
	Waiting       func(string)
}
type Session struct {
	op          sync.Mutex
	mu          sync.Mutex
	prompt      string
	profile     Profile
	dead        bool
	waiting     bool
	input       *os.File
	cancel      context.CancelFunc
	done        chan struct{}
	chunks      chan []byte
	readDone    chan struct{}
	options     Options
	shellPrompt string
	hierarchy   string
	deviceBase  string
}

// Start reuses prepared SSH arguments, including pinned trust and askpass. It
// returns only after authentication and terminal-only initialization complete.
func Start(lifetime, startup context.Context, req captured.Request, opts Options) (*Session, error) {
	if !ValidProfile(opts.Profile) {
		return nil, fmt.Errorf("invalid session profile %q", opts.Profile)
	}
	if opts.Limit <= 0 {
		opts.Limit = 8 << 20
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithCancel(lifetime)
	req.RemoteCommand = nil
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
	if err = pty.Setsize(master, &pty.Winsize{Rows: uint16(max(1, opts.Height)), Cols: uint16(max(1, opts.Width))}); err != nil {
		return fail(err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if err = cmd.Start(); err != nil {
		return fail(err)
	}
	slave.Close()
	s := &Session{input: master, cancel: cancel, done: make(chan struct{}), readDone: make(chan struct{}), chunks: make(chan []byte, 16), options: opts, profile: opts.Profile}
	go func() {
		defer close(s.readDone)
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				select {
				case s.chunks <- data:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { _ = cmd.Wait(); s.mu.Lock(); s.dead = true; s.mu.Unlock(); close(s.done) }()
	bootCtx, bootCancel := context.WithTimeout(startup, 30*time.Second)
	defer bootCancel()
	result := s.collect(bootCtx, "", "", true)
	if result.Err != nil {
		s.Close()
		return nil, result.Err
	}
	if s.profile == Auto {
		s.profile = detectProfile(result.Prompt)
	}
	s.deviceBase = strings.TrimRight(result.Prompt, "#>")
	if at := strings.Index(s.deviceBase, "("); at >= 0 {
		s.deviceBase = s.deviceBase[:at]
	}
	switch s.profile {
	case Linux:
		s.shellPrompt = "__nssh_" + nonce() + "__ "
		_, err = io.WriteString(s.input, "exec env PS1='"+s.shellPrompt+"' /bin/sh -i\n")
		if err == nil {
			err = s.collect(bootCtx, "", "", false).Err
		}
	case EOS:
		err = s.Execute(bootCtx, "terminal length 0").Err
	case Junos:
		err = s.Execute(bootCtx, "set cli screen-length 0").Err
	default:
		err = fmt.Errorf("unrecognized login prompt; choose :platform eos, junos, or linux before reconnecting")
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func nonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

var devicePrompt = regexp.MustCompile(`^[A-Za-z0-9_.:@/-]+(?:\([^\r\n()]*\))?[>#]$`)

func detectProfile(prompt string) Profile {
	if strings.HasSuffix(prompt, "$") || strings.ContainsAny(prompt, ":[] ") {
		return Linux
	}
	if strings.Contains(prompt, "@") {
		return Junos
	}
	if devicePrompt.MatchString(prompt) {
		return EOS
	}
	return Auto
}
func normalize(data []byte) string { return strings.ReplaceAll(ansi.Strip(string(data)), "\r", "") }
func tailPrompt(text string) string {
	parts := strings.Split(strings.TrimRight(text, " \t"), "\n")
	return strings.TrimSpace(parts[len(parts)-1])
}
func (s *Session) matchesPrompt(text string, boot bool) bool {
	if s.shellPrompt != "" {
		return strings.HasSuffix(text, s.shellPrompt)
	}
	p := tailPrompt(text)
	if len(p) > 512 {
		return false
	}
	if boot && s.profile == Linux {
		return strings.HasSuffix(p, "$") || strings.HasSuffix(p, "#") || strings.HasSuffix(p, ">") || strings.HasSuffix(p, "%")
	}
	if !boot && s.deviceBase != "" {
		return regexp.MustCompile(`^` + regexp.QuoteMeta(s.deviceBase) + `(?:\([^\r\n()]*\))?[>#]$`).MatchString(p)
	}
	if devicePrompt.MatchString(p) {
		return true
	}
	return boot && (strings.HasSuffix(p, "$") || strings.HasSuffix(p, "#"))
}
func (s *Session) Execute(ctx context.Context, command string) Result {
	s.op.Lock()
	defer s.op.Unlock()
	if strings.ContainsAny(command, "\r\n\x00") {
		return Result{Err: errors.New("a session command must be one line")}
	}
	if !s.Alive() {
		return Result{Err: errors.New("session closed; use :disconnect before explicitly reconnecting")}
	}
	line := command + "\n"
	token := ""
	if s.profile == Linux {
		token = "__nssh_exit_" + nonce() + "__"
		// One parsed shell line retains state through eval. An interactive read
		// cannot consume the status probe because it is syntax on the same line,
		// not a second line queued on stdin. A random result marker, rather than
		// prompt-shaped output, proves completion.
		quoted := "'" + strings.ReplaceAll(command, "'", "'\"'\"'") + "'"
		line = "command eval " + quoted + "; command printf '\\n" + token + "%d\\n' \"$?\"\n"
	}
	if err := ctx.Err(); err != nil {
		s.Close()
		return Result{Err: err}
	}
	if _, err := io.WriteString(s.input, line); err != nil {
		s.Close()
		return Result{Err: err}
	}
	limitCtx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	result := s.collect(limitCtx, command, token, false)
	if result.Err != nil && !errors.Is(result.Err, ErrCommand) {
		s.Close()
	}
	return result
}

// Completion requires a prompt after the command echo (or Linux's random exit
// marker). Silence alone never completes a command or answers a remote prompt.
func (s *Session) collect(ctx context.Context, command, token string, boot bool) Result {
	defer func() { s.mu.Lock(); s.waiting = false; s.mu.Unlock() }()
	var data, rawTail []byte
	truncated := false
	tail := ""
	seenEcho := command == ""
	known := false
	exitCode := 0
	quiet := time.NewTimer(time.Hour)
	defer quiet.Stop()
	waiting := time.NewTimer(time.Second)
	defer waiting.Stop()
	waitingSent := false
	for {
		select {
		case chunk := <-s.chunks:
			s.mu.Lock()
			s.waiting = false
			s.mu.Unlock()
			keep := min(len(chunk), max(0, s.options.Limit-len(data)))
			data = append(data, chunk[:keep]...)
			truncated = truncated || keep < len(chunk)
			rawTail = append(rawTail, chunk...)
			if len(rawTail) > 65536 {
				rawTail = rawTail[len(rawTail)-65536:]
			}
			tail = normalize(rawTail)
			if command != "" && strings.Contains(tail, command+"\n") {
				seenEcho = true
			}
			if token != "" {
				re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(token) + `([0-9]+)\n`)
				if match := re.FindStringSubmatch(tail); match != nil {
					exitCode, _ = strconv.Atoi(match[1])
					known = true
				}
			}
			if (token != "" && known) || (token == "" && seenEcho && s.matchesPrompt(tail, boot)) {
				quiet.Reset(150 * time.Millisecond)
			} else {
				quiet.Stop()
			}
			waiting.Reset(time.Second)
			waitingSent = false
		case <-quiet.C:
			if token == "" && !s.matchesPrompt(tail, boot) {
				continue
			}
			prompt := tailPrompt(tail)
			if s.profile == Junos {
				for _, line := range strings.Split(tail, "\n") {
					if len(line) < 512 && strings.HasPrefix(line, "[edit") && strings.HasSuffix(line, "]") {
						s.hierarchy = line
					}
				}
				if strings.HasSuffix(prompt, "#") && s.hierarchy != "" {
					prompt = s.hierarchy + " " + prompt
				}
				if strings.HasSuffix(prompt, ">") {
					s.hierarchy = ""
				}
			}
			if s.profile == Linux && s.shellPrompt != "" {
				prompt = "sh (persistent)"
			}
			s.mu.Lock()
			s.prompt = prompt
			s.mu.Unlock()
			output := normalize(data)
			if token != "" {
				output = strings.ReplaceAll(output, s.shellPrompt, "")
				var lines []string
				for _, line := range strings.Split(output, "\n") {
					if strings.HasPrefix(line, token) {
						break
					}
					if strings.Contains(line, "printf '\\n"+token) || line == command {
						continue
					}
					lines = append(lines, line)
				}
				output = strings.Join(lines, "\n")
			} else {
				if s.profile == Linux {
					output = strings.ReplaceAll(output, s.shellPrompt, "")
				}
				if at := strings.LastIndex(output, tailPrompt(tail)); at >= 0 {
					output = output[:at]
				}
				if command != "" {
					if at := strings.Index(output, command+"\n"); at >= 0 {
						output = output[at+len(command)+1:]
					}
				}
			}
			var commandErr error
			if s.profile != Linux && command != "" {
				if match := cliError.FindString(output); match != "" {
					commandErr = fmt.Errorf("%w: %s", ErrCommand, match)
				}
			}
			return Result{Output: []byte(strings.Trim(output, "\n")), Prompt: prompt, ExitCode: exitCode, StatusKnown: known, Truncated: truncated, Err: commandErr}
		case <-waiting.C:
			if !waitingSent && s.options.Waiting != nil && !boot {
				s.mu.Lock()
				s.waiting = true
				s.mu.Unlock()
				s.options.Waiting(tail)
				waitingSent = true
			}
		case <-ctx.Done():
			return Result{Output: data, Truncated: truncated, Err: fmt.Errorf("session canceled or timed out before a recognized prompt: %w", ctx.Err())}
		case <-s.done:
			return Result{Output: data, Truncated: truncated, Err: errors.New("SSH shell closed before command completion; commands were not replayed")}
		}
	}
}
func (s *Session) Reply(text string) error {
	if strings.ContainsAny(text, "\r\n\x00") {
		return errors.New("reply must be one line")
	}
	s.mu.Lock()
	if s.dead || !s.waiting {
		s.mu.Unlock()
		return errors.New("device is no longer waiting for a reply")
	}
	s.waiting = false
	s.mu.Unlock()
	_, err := io.WriteString(s.input, text+"\n")
	return err
}
func (s *Session) Alive() bool    { s.mu.Lock(); defer s.mu.Unlock(); return !s.dead }
func (s *Session) Prompt() string { s.mu.Lock(); defer s.mu.Unlock(); return s.prompt }
func (s *Session) Resize(width, height int) {
	if s.Alive() {
		_ = pty.Setsize(s.input, &pty.Winsize{Rows: uint16(max(1, min(height, 65535))), Cols: uint16(max(1, min(width, 65535)))})
	}
}
func (s *Session) Close() { s.cancel(); _ = s.input.Close(); <-s.done; <-s.readDone }

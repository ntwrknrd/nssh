package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/captured"
)

func fakeSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "starts")
	script := `#!/bin/sh
printf '%s\n' "$$" >> "$SESSION_STARTS"
if [ "$SESSION_TEST_PROFILE" = linux ]; then
  printf 'tester@linux:~$ '
  exec /bin/sh -i
fi
prompt='edge# '
if [ "$SESSION_TEST_PROFILE" = junos ]; then prompt='tester@edge> '; fi
value=unset
if [ "$SESSION_TEST_COLOR" = yes ]; then
 printf '\033['
 sleep 0.05
 printf '32m%s\033[0m' "$prompt"
else
 printf '%s' "$prompt"
fi
while IFS= read -r command; do
 printf '%s\n' "$command"
 case "$command" in
  'terminal length 0'|'set cli screen-length 0') ;;
  'configure terminal') prompt='edge(config)# ' ;;
  configure) prompt='tester@edge# ' ;;
  'edit interfaces') printf '[edit interfaces]\n' ;;
  'set value kept') value=kept ;;
  'show value') printf '%s\n' "$value" ;;
  confirm) printf 'Proceed? [yes/no] '; IFS= read -r answer; printf '\nanswer=%s\n' "$answer" ;;
  hold) sleep 10 ;;
  die) exit 1 ;;
  fail) printf '%% Invalid input\n' ;;
  'show big') i=0;while [ "$i" -lt 500 ]; do printf 'abcdefghij\n';i=$((i+1));done ;;
  *) printf 'output=%s\n' "$command" ;;
 esac
 printf '%s' "$prompt"
done
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("SESSION_STARTS", log)
	return log
}
func openTestSession(t *testing.T, profile Profile, opts Options) *Session {
	t.Helper()
	t.Setenv("SESSION_TEST_PROFILE", string(profile))
	lifetime, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	opts.Profile = profile
	opts.Width = 120
	opts.Height = 40
	s, err := Start(lifetime, lifetime, captured.Request{Hostname: "edge"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func TestDeviceSessionRetainsStateAndMode(t *testing.T) {
	for _, profile := range []Profile{EOS, Junos} {
		t.Run(string(profile), func(t *testing.T) {
			log := fakeSSH(t)
			s := openTestSession(t, profile, Options{})
			configure := "configure terminal"
			if profile == Junos {
				configure = "configure"
			}
			result := s.Execute(context.Background(), configure)
			if result.Err != nil || !strings.HasSuffix(result.Prompt, "#") {
				t.Fatalf("configure: %+v", result)
			}
			if result.StatusKnown {
				t.Fatal("invented remote exit status")
			}
			if r := s.Execute(context.Background(), "set value kept"); r.Err != nil {
				t.Fatal(r.Err)
			}
			if r := s.Execute(context.Background(), "show value"); r.Err != nil || string(r.Output) != "kept" {
				t.Fatalf("state lost: %+v", r)
			}
			data, _ := os.ReadFile(log)
			if len(strings.Fields(string(data))) != 1 {
				t.Fatalf("reconnected: %q", data)
			}
			s.Close()
			if s.Alive() {
				t.Fatal("session remained alive")
			}
			if r := s.Execute(context.Background(), "show value"); r.Err == nil {
				t.Fatal("closed session reused")
			}
		})
	}
}
func TestLinuxShellRetainsDirectoryVariablesAndExitStatus(t *testing.T) {
	log := fakeSSH(t)
	s := openTestSession(t, Linux, Options{})
	for _, command := range []string{"cd /tmp", "value=kept"} {
		if r := s.Execute(context.Background(), command); r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	result := s.Execute(context.Background(), "printf '%s:%s' \"$PWD\" \"$value\"")
	if result.Err != nil || !result.StatusKnown || result.ExitCode != 0 || !strings.Contains(string(result.Output), "/tmp:kept") {
		t.Fatalf("state: %+v", result)
	}
	result = s.Execute(context.Background(), "false")
	if result.Err != nil || result.ExitCode != 1 || !result.StatusKnown {
		t.Fatalf("exit status: %+v", result)
	}
	data, _ := os.ReadFile(log)
	if len(strings.Fields(string(data))) != 1 {
		t.Fatal("multiple SSH processes")
	}
}
func TestReplyIsExplicitAndSessionRemainsOpen(t *testing.T) {
	fakeSSH(t)
	waiting := make(chan string, 4)
	s := openTestSession(t, EOS, Options{Waiting: func(text string) { waiting <- text }})
	results := make(chan Result, 1)
	go func() { results <- s.Execute(context.Background(), "confirm") }()
	select {
	case text := <-waiting:
		if !strings.Contains(text, "Proceed?") {
			t.Fatal(text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing waiting notification")
	}
	select {
	case <-results:
		t.Fatal("confirmation auto-answered")
	default:
	}
	if err := s.Reply("yes"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-results:
		if result.Err != nil || !strings.Contains(string(result.Output), "answer=yes") {
			t.Fatalf("reply: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reply hung")
	}
}
func TestCancellationClosesSessionWithoutReplay(t *testing.T) {
	fakeSSH(t)
	s := openTestSession(t, EOS, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := s.Execute(ctx, "hold")
	if r.Err == nil || s.Alive() {
		t.Fatal("cancellation left session reusable")
	}
}
func TestOutputBoundStillConsumesPrompt(t *testing.T) {
	fakeSSH(t)
	s := openTestSession(t, EOS, Options{Limit: 128})
	r := s.Execute(context.Background(), "show big")
	if r.Err != nil || !r.Truncated || len(r.Output) > 128 {
		t.Fatalf("bounded output: %+v", r)
	}
	if r = s.Execute(context.Background(), "show value"); r.Err != nil || string(r.Output) != "unset" {
		t.Fatalf("next command corrupted: %+v", r)
	}
}

func TestLinuxReadDoesNotConsumeExitProbe(t *testing.T) {
	fakeSSH(t)
	waiting := make(chan string, 4)
	s := openTestSession(t, Linux, Options{Waiting: func(text string) { waiting <- text }})
	results := make(chan Result, 1)
	go func() { results <- s.Execute(context.Background(), "printf '%s' \"$PS1\"; read value") }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not wait for input")
	}
	select {
	case <-results:
		t.Fatal("read consumed queued protocol input")
	default:
	}
	if err := s.Reply("operator-answer"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-results:
		if r.Err != nil || !r.StatusKnown {
			t.Fatalf("read: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read did not finish")
	}
	r := s.Execute(context.Background(), "printf '%s' \"$value\"")
	if r.Err != nil || !strings.Contains(string(r.Output), "operator-answer") {
		t.Fatalf("lost reply: %+v", r)
	}
}

func TestLostSessionDoesNotReconnect(t *testing.T) {
	log := fakeSSH(t)
	s := openTestSession(t, EOS, Options{})
	if r := s.Execute(context.Background(), "die"); r.Err == nil {
		t.Fatal("session loss reported completion")
	}
	if r := s.Execute(context.Background(), "show value"); r.Err == nil {
		t.Fatal("lost session transparently reused")
	}
	data, _ := os.ReadFile(log)
	if len(strings.Fields(string(data))) != 1 {
		t.Fatal("lost command was reconnected or replayed")
	}
}

func TestDeviceCommandErrorKeepsSession(t *testing.T) {
	fakeSSH(t)
	s := openTestSession(t, EOS, Options{})
	r := s.Execute(context.Background(), "fail")
	if !errors.Is(r.Err, ErrCommand) || !s.Alive() {
		t.Fatalf("CLI error lost session: %+v", r)
	}
	if r = s.Execute(context.Background(), "show value"); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := s.Reply("yes"); err == nil {
		t.Fatal("reply accepted at an idle command prompt")
	}
}

func TestFragmentedColorDoesNotChangePromptIdentity(t *testing.T) {
	fakeSSH(t)
	t.Setenv("SESSION_TEST_COLOR", "yes")
	s := openTestSession(t, Auto, Options{Timeout: time.Second})
	r := s.Execute(context.Background(), "show value")
	if r.Err != nil || string(r.Output) != "unset" {
		t.Fatalf("fragmented ANSI changed prompt identity: %+v", r)
	}
}

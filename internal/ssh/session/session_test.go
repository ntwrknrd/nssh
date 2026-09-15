package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

type outputBuffer struct {
	sync.Mutex
	text string
}

func (b *outputBuffer) write(p []byte) { b.Lock(); b.text += string(p); b.Unlock() }
func (b *outputBuffer) get() string    { b.Lock(); defer b.Unlock(); return b.text }
func await(t *testing.T, b *outputBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(b.get(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing %q in %q", text, b.get())
}
func TestRawTerminalsRetainDeviceModesAndReplies(t *testing.T) {
	for _, profile := range []string{"eos", "junos", "linux"} {
		t.Run(profile, func(t *testing.T) {
			log := fakeSSH(t)
			t.Setenv("SESSION_TEST_PROFILE", profile)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := &outputBuffer{}
			s, err := Start(ctx, captured.Request{Hostname: "edge"}, Options{Width: 100, Height: 30, Output: b.write})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if profile == "linux" {
				await(t, b, "linux")
				_ = s.Send([]byte("value=kept; cd /tmp; printf 'value=%s pwd=%s\\n' \"$value\" \"$PWD\"\n"))
				await(t, b, "value=kept pwd=/tmp")
			} else {
				configure := "configure terminal"
				prompt := "edge(config)#"
				if profile == "junos" {
					configure = "configure"
					prompt = "tester@edge#"
				}
				_ = s.Send([]byte(configure + "\n"))
				await(t, b, prompt)
				_ = s.Send([]byte("set value kept\nshow value\n"))
				await(t, b, "kept")
				_ = s.Send([]byte("confirm\n"))
				await(t, b, "Proceed?")
				_ = s.Send([]byte("yes\n"))
				await(t, b, "answer=yes")
			}
			starts, _ := os.ReadFile(log)
			if len(strings.Fields(string(starts))) != 1 {
				t.Fatal("reopened SSH")
			}
			if strings.Contains(b.get(), "__nssh") {
				t.Fatal("injected shell protocol")
			}
			cancel()
			s.Close()
			if s.Send([]byte("never\n")) == nil {
				t.Fatal("accepted input after close")
			}
		})
	}
}
func TestTerminalCancellationStopsProcess(t *testing.T) {
	fakeSSH(t)
	ctx, cancel := context.WithCancel(context.Background())
	b := &outputBuffer{}
	s, err := Start(ctx, captured.Request{Hostname: "edge"}, Options{Width: 80, Height: 24, Output: b.write})
	if err != nil {
		t.Fatal(err)
	}
	await(t, b, "edge#")
	_ = s.Send([]byte("hold\n"))
	cancel()
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("terminal cleanup hung")
	}
}

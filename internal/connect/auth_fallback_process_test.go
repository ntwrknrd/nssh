//go:build unix

package connect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
)

func TestInteractiveAuthFallbackProcess(t *testing.T) {
	if os.Getenv("NSSH_FALLBACK_HELPER") == "askpass" {
		password, err := askpass.RequestPassword(context.Background(), os.Getenv(askpass.SocketEnv), os.Getenv(askpass.NonceEnv))
		if err != nil {
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(password)
		os.Exit(0)
	}
	if os.Getenv("NSSH_FALLBACK_HELPER") == "connect" {
		runAuthFallbackProcess()
		return
	}
	for _, tc := range []struct {
		name, scenario, policy string
		tty, manual            bool
	}{
		{"missing key", "missing", "manual", true, true},
		{"rejected password", "rejected", "manual", true, true},
		{"lookup deadline", "timeout", "manual", true, true},
		{"additional challenge", "secondprompt", "manual", true, true},
		{"proxy missing key", "proxy-missing", "manual", true, true},
		{"proxy rejection", "proxy-rejected", "manual", true, true},
		{"host disables fallback", "missing", "fail", true, false},
		{"unattended", "missing", "manual", false, false},
		{"unattended rejection", "rejected", "manual", false, false},
		{"unattended proxy", "proxy-missing", "manual", false, false},
		{"BatchMode", "batch", "manual", true, false},
		{"established session failure", "session-failure", "manual", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			helper := filepath.Join(dir, "nssh-askpass")
			// Same test executable implements the authenticated helper protocol.
			if err := os.WriteFile(helper, []byte("#!/bin/sh\nNSSH_FALLBACK_HELPER=askpass exec \"$NSSH_TEST_BINARY\" -test.run=^TestInteractiveAuthFallbackProcess$\n"), 0700); err != nil {
				t.Fatal(err)
			}
			ssh := `#!/bin/sh
if [ "$NSSH_PROXY_ASKPASS_REQUIRE" = force ]; then
  env SSH_ASKPASS="$NSSH_PROXY_SSH_ASKPASS" NSSH_ASKPASS_SOCKET="$NSSH_PROXY_ASKPASS_SOCKET" NSSH_ASKPASS_NONCE="$NSSH_PROXY_ASKPASS_NONCE" "$NSSH_PROXY_SSH_ASKPASS" "Proxy password:" >/dev/null || exit 255
  env SSH_ASKPASS="$NSSH_PROXY_SSH_ASKPASS" NSSH_ASKPASS_SOCKET="$NSSH_PROXY_ASKPASS_SOCKET" NSSH_ASKPASS_NONCE="$NSSH_PROXY_ASKPASS_NONCE" "$NSSH_PROXY_SSH_ASKPASS" "Proxy password:" >/dev/null || exit 255
  printf 'Permission denied (password).\n' >&2
  exit 255
fi
if [ "$SSH_ASKPASS_REQUIRE" = force ]; then
  "$SSH_ASKPASS" "Password:" >/dev/null || exit 255
  if [ "$NSSH_FALLBACK_SCENARIO" = session-failure ]; then
    printf 'Permission denied in application\n' >&2
    exit 255
  fi
  "$SSH_ASKPASS" "Password:" >/dev/null || exit 255
  printf 'Permission denied (password).\n' >&2
  exit 255
fi
stty -echo
printf 'native-password: '
IFS= read -r password
stty echo
if [ "$password" != manual-dummy ]; then exit 5; fi
printf '\nmanual-authenticated\n'
`
			if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(ssh), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInteractiveAuthFallbackProcess$")
			cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1", "COLORFGBG=15;0", "PATH="+dir+":"+os.Getenv("PATH"), "NSSH_TEST_BINARY="+os.Args[0], "NSSH_FALLBACK_HELPER=connect", "NSSH_FALLBACK_DIR="+dir, "NSSH_FALLBACK_SCENARIO="+tc.scenario, "NSSH_FALLBACK_POLICY="+tc.policy)
			var output []byte
			var err error
			if tc.tty {
				terminal, startErr := pty.Start(cmd)
				if startErr != nil {
					t.Fatal(startErr)
				}
				defer func() { _ = terminal.Close() }()
				done := make(chan []byte, 1)
				go func() {
					var out []byte
					wrote := false
					buf := make([]byte, 1024)
					for {
						n, readErr := terminal.Read(buf)
						out = append(out, buf[:n]...)
						if !wrote && strings.Contains(string(out), "native-password:") {
							wrote = true
							_, _ = terminal.WriteString("manual-dummy\n")
						}
						if readErr != nil {
							break
						}
					}
					done <- out
				}()
				err = cmd.Wait()
				output = <-done
			} else {
				output, err = cmd.CombinedOutput()
			}
			if ctx.Err() != nil {
				t.Fatalf("command hung: %s", output)
			}
			if tc.manual {
				if err != nil || !strings.Contains(string(output), "manual-authenticated") || !strings.Contains(string(output), "falling back to manual password entry") {
					t.Fatalf("err=%v output=%s", err, output)
				}
				if strings.Contains(string(output), "manual-dummy") || strings.Contains(string(output), "automatic-dummy") {
					t.Fatal("password echoed")
				}
			} else {
				var exitErr *exec.ExitError
				expectedCode := 3
				if tc.scenario == "session-failure" {
					expectedCode = 2
				}
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != expectedCode || strings.Contains(string(output), "native-password:") {
					t.Fatalf("err=%v output=%s", err, output)
				}
			}
		})
	}
}

func runAuthFallbackProcess() {
	scenario := os.Getenv("NSSH_FALLBACK_SCENARIO")
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) hostKeyProbeStatus {
		return hostKeyProbeClean
	}
	askpassHelperPathFunc = func() (string, error) { return filepath.Join(os.Getenv("NSSH_FALLBACK_DIR"), "nssh-askpass"), nil }
	host := &ResolvedHost{Hostname: "fixture", Username: "operator", Port: 22, AuthMode: config.AuthModePassword, AuthFallback: os.Getenv("NSSH_FALLBACK_POLICY"), AuthLookupTimeout: 50 * time.Millisecond}
	host.Credential = &ResolvedCredential{PasswordResolver: func(ctx context.Context) (*secret.Secret, error) {
		if scenario == "timeout" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if scenario == "rejected" || scenario == "proxy-rejected" || scenario == "secondprompt" || scenario == "session-failure" {
			return secret.NewFromString("automatic-dummy"), nil
		}
		return nil, errors.New("decryption key unavailable")
	}}
	if strings.HasPrefix(scenario, "proxy-") {
		host.Proxy = &ResolvedProxy{AuthMode: config.AuthModePassword, Credential: host.Credential, AuthFallback: host.AuthFallback, AuthLookupTimeout: host.AuthLookupTimeout}
		host.AuthFallback = "fail"
		host.Credential = nil
	}
	var args []string
	if scenario == "batch" {
		args = []string{"-o", "BatchMode=yes"}
	}
	result := runResolvedConnection(context.Background(), host, args, config.DefaultConfig(), nil, Options{})
	if result.Err != nil {
		fmt.Fprintln(os.Stderr, result.Err)
		var exitErr *exit.ExitError
		if errors.As(result.Err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
	os.Exit(0)
}

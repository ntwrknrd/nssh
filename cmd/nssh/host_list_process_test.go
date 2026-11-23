package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type hostListFixture struct{ dir, binary string }

func newHostListFixture(t *testing.T, binary string) *hostListFixture {
	t.Helper()
	f := &hostListFixture{t.TempDir(), binary}
	for _, d := range []string{"bin", "home", "config/nssh", "data", "state", "log"} {
		if err := os.MkdirAll(filepath.Join(f.dir, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := `ssh:
  defaults: {}
inventory:
  providers:
    local:
      type: local
      hosts:
        alpha: {aliases: [alpha-alias], auth: {mode: key, username: alpha-user}}
        beta: {auth: {mode: key, username: beta-user}}
        gamma: {auth: {mode: key, username: gamma-user}}
        delta: {auth: {mode: key, username: delta-user}}
        whole-comma: {aliases: ["whole,comma"], auth: {mode: key, username: whole-user}}
        blocked: {ssh: {options: {RequestTTY: force}}, auth: {mode: key}}
`
	if err := os.WriteFile(filepath.Join(f.dir, "config", "nssh", "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
exec 9> "$NSSH_HOST_LIST_LOG/$$"
printf '\036' >&9
for arg do printf '%s\037' "$arg" >&9; done
exec 9>&-
case "$*" in *fail*) exit 7;; *hold*) printf '%s\n' "$$" >> "$NSSH_HOST_LIST_PID"; exec sleep 60;; esac
exit 0
`
	if err := os.WriteFile(filepath.Join(f.dir, "bin", "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *hostListFixture) command(t *testing.T, args ...string) *exec.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	c := exec.CommandContext(ctx, f.binary, args...)
	c.Env = []string{"PATH=" + filepath.Join(f.dir, "bin") + ":/usr/bin:/bin", "HOME=" + filepath.Join(f.dir, "home"), "XDG_CONFIG_HOME=" + filepath.Join(f.dir, "config"), "XDG_DATA_HOME=" + filepath.Join(f.dir, "data"), "XDG_STATE_HOME=" + filepath.Join(f.dir, "state"), "NSSH_HOST_LIST_LOG=" + filepath.Join(f.dir, "log"), "NSSH_HOST_LIST_PID=" + filepath.Join(f.dir, "pid")}
	return c
}
func (f *hostListFixture) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	output, err := f.command(t, args...).CombinedOutput()
	if err != nil {
		t.Logf("nssh output: %s", output)
	}
	return f.readLog(t), err
}
func (f *hostListFixture) readLog(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.dir, "log"))
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(f.dir, "log", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		log.Write(data)
	}
	return log.String()
}
func (f *hostListFixture) records(t *testing.T) [][]string {
	t.Helper()
	data := f.readLog(t)
	var out [][]string
	for _, call := range strings.Split(data, "\036") {
		if call == "" {
			continue
		}
		parts := strings.Split(call, "\037")
		out = append(out, parts[:len(parts)-1])
	}
	return out
}
func hasTail(record, tail []string) bool {
	if len(record) < len(tail) {
		return false
	}
	return strings.Join(record[len(record)-len(tail):], "\x00") == strings.Join(tail, "\x00")
}
func commandRecords(records [][]string, tail []string) [][]string {
	var out [][]string
	for _, record := range records {
		if hasTail(record, tail) {
			out = append(out, record)
		}
	}
	return out
}
func hasTarget(records [][]string, target string) bool {
	for _, r := range records {
		for _, arg := range r {
			if arg == target {
				return true
			}
		}
	}
	return false
}
func requireHosts(t *testing.T, records [][]string, want ...string) {
	t.Helper()
	for _, host := range want {
		if !hasTarget(records, host) {
			t.Fatalf("records=%q missing %s", records, host)
		}
	}
}
func TestHostListProcessRunsEachOriginalHostWithOneRemoteArgv(t *testing.T) {
	for _, input := range []string{"alpha,beta,gamma,delta", "alpha, beta, gamma, delta"} {
		t.Run(input, func(t *testing.T) {
			f := newHostListFixture(t, buildBinary(t))
			if _, err := f.run(t, input, "show env power", "", "--", "-leading"); err != nil {
				t.Fatal(err)
			}
			records := f.records(t)
			requireHosts(t, records, "alpha-user@alpha", "beta-user@beta", "gamma-user@gamma", "delta-user@delta")
			want := []string{"show env power", "", "--", "-leading"}
			commands := commandRecords(records, want)
			if len(commands) != 4 {
				t.Fatalf("command records=%q", records)
			}
			for _, r := range commands {
				if !hasTail(r, want) {
					t.Fatalf("argv=%q want tail=%q", r, want)
				}
			}
		})
	}
}
func TestHostListProcessHonorsSharedUserAndPerHostUsers(t *testing.T) {
	f := newHostListFixture(t, buildBinary(t))
	if _, err := f.run(t, "-l", "shared", "alpha,beta", "show", "version"); err != nil {
		t.Fatal(err)
	}
	requireHosts(t, f.records(t), "shared@alpha", "shared@beta")
	f = newHostListFixture(t, buildBinary(t))
	if _, err := f.run(t, "alpha,beta", "show", "version"); err != nil {
		t.Fatal(err)
	}
	requireHosts(t, f.records(t), "alpha-user@alpha", "beta-user@beta")
}
func TestHostListProcessReportsFailureAndDeduplicatesAliases(t *testing.T) {
	f := newHostListFixture(t, buildBinary(t))
	if log, err := f.run(t, "alpha,alpha-alias,beta", "fail"); err == nil {
		t.Fatalf("aggregate failure succeeded: %s", log)
	} else {
		var failure *exec.ExitError
		if !errors.As(err, &failure) || failure.ExitCode() != 1 {
			t.Fatalf("aggregate exit=%v", err)
		}
	}
	records := f.records(t)
	count := 0
	for _, r := range commandRecords(records, []string{"fail"}) {
		if hasTarget([][]string{r}, "alpha-user@alpha") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("alias records=%q", records)
	}
	requireHosts(t, records, "beta-user@beta")
}
func TestHostListProcessLeavesWholeCommaAliasAndBlocksModesBeforeSSH(t *testing.T) {
	f := newHostListFixture(t, buildBinary(t))
	if _, err := f.run(t, "whole,comma"); err != nil {
		t.Fatal(err)
	}
	if !hasTarget(f.records(t), "whole-user@whole-comma") {
		t.Fatalf("whole alias records=%q", f.records(t))
	}
	for _, args := range [][]string{{"blocked,alpha", "show"}, {"-L", "8080:x:80", "alpha,beta", "show"}, {"-NT", "alpha,beta", "show"}, {"-o", "ForkAfterAuthentication=yes", "alpha,beta", "show"}, {"alpha,ssh://other", "show"}} {
		f = newHostListFixture(t, buildBinary(t))
		log, err := f.run(t, args...)
		if err == nil || log != "" {
			t.Fatalf("mode %v err=%v log=%q", args, err, log)
		}
	}
}
func TestHostListProcessCtrlCCleansChild(t *testing.T) {
	f := newHostListFixture(t, buildBinary(t))
	c := f.command(t, "alpha,beta", "hold")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Process.Kill() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(filepath.Join(f.dir, "pid"))
		if len(strings.Fields(string(data))) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("children did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := c.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err := c.Wait()
	var interrupted *exec.ExitError
	if !errors.As(err, &interrupted) || interrupted.ExitCode() != 130 {
		t.Fatalf("SIGINT exit=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(f.dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		deadline = time.Now().Add(3 * time.Second)
		for syscall.Kill(pid, 0) != syscall.ESRCH {
			if time.Now().After(deadline) {
				t.Fatalf("SSH child %d remains after Ctrl-C", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestHostListProcessWithoutCommandOpensRexPanes(t *testing.T) {
	f := newHostListFixture(t, buildBinary(t))
	rexDir := filepath.Join(f.dir, "rex")
	if err := os.MkdirAll(rexDir, 0700); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$(dirname "$0")/calls"
case "$*" in
*session.create*) echo '{"session_id":"session:n","initial_windows":[{"window_id":"window:1","block_ids":["block:1","block:2"]}]}' ;;
*session.list*) echo '{"sessions":[]}' ;;
*) echo '{}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(rexDir, "rex"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := f.command(t, "alpha-alias,beta")
	cmd.Env = append(cmd.Env, "REX_SESSION=session:me", "REX_BLOCK=block:me", "REX_BIN_DIR="+rexDir)
	if output, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(output), "opened alpha + beta in Rex session nssh") {
		t.Fatalf("err=%v output=%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(rexDir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if calls := string(data); !strings.Contains(calls, `"label":"alpha"`) || !strings.Contains(calls, `"--target","beta"`) {
		t.Fatalf("rex calls = %s", calls)
	}
	if log := f.readLog(t); log != "" {
		t.Fatalf("ssh ran in the invoking pane: %q", log)
	}

	// Outside Rex the command is still required.
	if _, err := f.run(t, "alpha,beta"); err == nil {
		t.Fatal("host list without a command succeeded outside Rex")
	}
}

func TestHostListProcessExpandsSuffixRanges(t *testing.T) {
	f := newHostListFixture(t, buildBinary(t))
	if _, err := f.run(t, "-l", "ops", "alpha,(x)", "show"); err == nil {
		t.Fatal("invalid expansion accepted")
	}
	f = newHostListFixture(t, buildBinary(t))
	cfg := filepath.Join(f.dir, "config", "nssh", "config.yaml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	extra := "        core1: {auth: {mode: key, username: c1}}\n        core2: {auth: {mode: key, username: c2}}\n        core3: {auth: {mode: key, username: c3}}\n"
	if err := os.WriteFile(cfg, append(data, extra...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(t, "core(1-3)", "show"); err != nil {
		t.Fatal(err)
	}
	records := commandRecords(f.records(t), []string{"show"})
	if len(records) != 3 {
		t.Fatalf("records=%q", records)
	}
	requireHosts(t, records, "c1@core1", "c2@core2", "c3@core3")
}

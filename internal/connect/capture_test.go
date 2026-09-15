package connect

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func captureTestHooks(t *testing.T) {
	t.Helper()
	oldRun, oldProbe, oldScan := runCapturedCommandFunc, hostKeyProbeFunc, scanHostKeyFunc
	oldStart, oldCheck := startMuxSessionFunc, checkMuxSessionFunc
	t.Cleanup(func() {
		runCapturedCommandFunc = oldRun
		hostKeyProbeFunc = oldProbe
		scanHostKeyFunc = oldScan
		startMuxSessionFunc = oldStart
		checkMuxSessionFunc = oldCheck
	})
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) hostKeyProbeStatus {
		return hostKeyProbeClean
	}
	checkMuxSessionFunc = func(context.Context, connector.MuxCheckRequest) (bool, bool) { return false, true }
}

func TestCommandCaptureOwnsStdinAndMuxDiagnostics(t *testing.T) {
	captureTestHooks(t)
	cfg := config.DefaultConfig()
	host := &ResolvedHost{Hostname: "edge", Canonical: "edge", AuthMode: config.AuthModeKey, Config: cfg, SSH: config.SSHHostConfig{Options: config.SSHOptions{
		"ControlMaster": config.NewSSHOptionString("auto"), "ControlPath": config.NewSSHOptionString("/tmp/test-%h"), "ControlPersist": config.NewSSHOptionString("60"),
	}}}
	started := false
	startMuxSessionFunc = func(_ context.Context, req connector.MuxStartRequest) error {
		started = true
		input, err := io.ReadAll(req.Stdin)
		if err != nil || len(input) != 0 {
			t.Errorf("mux input = %q, %v", input, err)
		}
		if req.Stdout == nil || req.Stderr == nil {
			t.Fatal("mux inherited global output")
		}
		_, _ = io.WriteString(req.Stderr, "setup\n")
		return nil
	}
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		input, err := io.ReadAll(req.Stdin)
		if err != nil || len(input) != 0 {
			t.Errorf("command input = %q, %v", input, err)
		}
		if req.MaxOutputBytes != 1024-len("setup\n") {
			t.Errorf("capture budget = %d", req.MaxOutputBytes)
		}
		if strings.Join(req.RemoteCommand, " ") != "show env power" {
			t.Errorf("command=%v", req.RemoteCommand)
		}
		return captured.Result{Stdout: []byte("power OK\n")}, nil
	}
	got, err := RunRemoteCommandCapture(context.Background(), host, []string{"show env power"}, CaptureOptions{MaxOutputBytes: 1024})
	if err != nil || !started || string(got.Stderr) != "setup\n" || string(got.Stdout) != "power OK\n" {
		t.Fatalf("result=%+v, started=%v, err=%v", got, started, err)
	}
}

func TestCommandCaptureRejectsMissingTrustInteraction(t *testing.T) {
	captureTestHooks(t)
	hostKeyProbeFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) hostKeyProbeStatus {
		return hostKeyProbeNeedsPrompt
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{Fingerprint: "SHA256:test"}, nil
	}
	runCapturedCommandFunc = func(context.Context, captured.Request) (captured.Result, error) {
		t.Error("command ran without trust")
		return captured.Result{}, nil
	}
	_, err := RunRemoteCommandCapture(context.Background(), &ResolvedHost{Hostname: "unknown", Config: config.DefaultConfig()}, []string{"show"}, CaptureOptions{})
	if err == nil || !strings.Contains(err.Error(), "interactive host-key") {
		t.Fatalf("err=%v", err)
	}
}

func TestCommandTrustInteractionSerializesAndHonorsCancel(t *testing.T) {
	captureTestHooks(t)
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{}, nil
	}
	var active, peak atomic.Int32
	prompt := func(p connector.HostKeyPrompt) connector.HostKeyAction {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		data, _ := io.ReadAll(p.Stdin)
		if len(data) != 0 {
			t.Error("prompt has process input")
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		return connector.HostKeyReject
	}
	opts := Options{capture: &CaptureOptions{HostKeyPrompt: prompt}}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge"}, nil, config.DefaultConfig(), opts, false, nil)
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Fatalf("overlapping trust prompts: %d", peak.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	commandTrustGate <- struct{}{}
	_, err := runHostKeyPreparation(ctx, &ResolvedHost{Hostname: "edge"}, nil, config.DefaultConfig(), opts, false, nil)
	<-commandTrustGate
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestCommandPreparationDiagnosticsAreBounded(t *testing.T) {
	cmd := exec.Command("sh", "-c", "cat")
	cmd.Stdin = strings.NewReader(strings.Repeat("x", 128<<10))
	output, err := collectPreparationOutput(cmd, Options{capture: &CaptureOptions{}})
	if len(output) != 64<<10 || err == nil {
		t.Fatalf("bytes=%d err=%v", len(output), err)
	}
}
func TestCommandCapturePassesSSHArgs(t *testing.T) {
	captureTestHooks(t)
	host := &ResolvedHost{Hostname: "edge", Canonical: "edge", AuthMode: config.AuthModeKey, Config: config.DefaultConfig()}
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		if got, want := strings.Join(req.SSHArgs, " "), "-l operator -p 2202"; got != want {
			t.Fatalf("SSHArgs=%q want=%q", got, want)
		}
		return captured.Result{}, nil
	}
	if _, err := RunRemoteCommandCapture(context.Background(), host, []string{"show"}, CaptureOptions{SSHArgs: []string{"-l", "operator", "-p", "2202"}}); err != nil {
		t.Fatal(err)
	}
}

package connect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

func TestHostNotFoundErrorCarriesHostname(t *testing.T) {
	var err error = &HostNotFoundError{Hostname: "edge01"}
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatal("HostNotFoundError should support errors.As")
	}
	if notFound.Hostname != "edge01" || notFound.Error() != "host not found: edge01" {
		t.Fatalf("notFound = %+v error=%q", notFound, notFound.Error())
	}
}

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = stdoutWrite
	os.Stderr = stderrWrite
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()

	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	stdout, err := io.ReadAll(stdoutRead)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	stderr, err := io.ReadAll(stderrRead)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(stdout), string(stderr)
}

func TestIsCompatibilityError(t *testing.T) {
	if !isCompatibilityError(&exit.ExitError{Code: exit.ExitConnectionFailed}) {
		t.Fatal("connection failed exit should be compatibility candidate")
	}
	if !isCompatibilityError(&exit.ExitError{Code: 255}) {
		t.Fatal("ssh exit 255 should be compatibility candidate")
	}
	if isCompatibilityError(&exit.ExitError{Code: exit.ExitAuthFailed}) {
		t.Fatal("auth failure should not be compatibility candidate")
	}
	if isCompatibilityError(errors.New("plain error")) {
		t.Fatal("plain error should not be compatibility candidate")
	}
}

func TestExtractExplicitUser(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		sshArgs  []string
		want     string
	}{
		{name: "user at host", hostname: "admin@edge01", want: "admin"},
		{name: "split login flag", hostname: "edge01", sshArgs: []string{"-l", "admin"}, want: "admin"},
		{name: "joined login flag", hostname: "edge01", sshArgs: []string{"-ladmin"}, want: "admin"},
		{name: "no explicit user", hostname: "edge01", sshArgs: []string{"-p", "2222"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractExplicitUser(tt.hostname, tt.sshArgs); got != tt.want {
				t.Fatalf("extractExplicitUser() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnectRequestRoutesInteractiveSessionToPTYConnector(t *testing.T) {
	var interactiveCalled bool
	oldConnectHost := connectHostFunc
	oldRunRemoteCommand := runRemoteCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		connectHostFunc = oldConnectHost
		runRemoteCommandFunc = oldRunRemoteCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		if host != "edge01" {
			t.Fatalf("resolve host = %q, want edge01", host)
		}
		return "edge01.example.com", nil
	}
	connectHostFunc = func(_ context.Context, host string, sshArgs []string, _ ...Options) error {
		interactiveCalled = true
		if host != "edge01.example.com" {
			t.Fatalf("interactive host = %q, want resolved host", host)
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-p" || sshArgs[1] != "2222" {
			t.Fatalf("interactive ssh args = %#v", sshArgs)
		}
		return nil
	}
	runRemoteCommandFunc = func(_ context.Context, _ string, _ []string, _ []string, _ ...Options) error {
		t.Fatal("remote command runner should not be called for interactive sessions")
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:    "edge01",
		SSHArgs: []string{"-p", "2222"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !interactiveCalled {
		t.Fatal("interactive connector was not called")
	}
}

func TestConnectRequestRoutesRemoteCommandToCapturedRunner(t *testing.T) {
	var remoteCalled bool
	oldConnectHost := connectHostFunc
	oldRunRemoteCommand := runRemoteCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		connectHostFunc = oldConnectHost
		runRemoteCommandFunc = oldRunRemoteCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		if host != "edge01" {
			t.Fatalf("resolve host = %q, want edge01", host)
		}
		return "edge01.example.com", nil
	}
	connectHostFunc = func(_ context.Context, _ string, _ []string, _ ...Options) error {
		t.Fatal("interactive connector should not be called for remote commands")
		return nil
	}
	runRemoteCommandFunc = func(_ context.Context, host string, sshArgs, command []string, _ ...Options) error {
		remoteCalled = true
		if host != "edge01.example.com" {
			t.Fatalf("remote host = %q, want resolved host", host)
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-p" || sshArgs[1] != "2222" {
			t.Fatalf("remote ssh args = %#v", sshArgs)
		}
		if len(command) != 2 || command[0] != "show" || command[1] != "version" {
			t.Fatalf("remote command = %#v", command)
		}
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:          "edge01",
		SSHArgs:       []string{"-p", "2222"},
		RemoteCommand: []string{"show", "version"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !remoteCalled {
		t.Fatal("remote command runner was not called")
	}
}

func TestConnectRequestPreservesLiteralTargetForRemoteCommand(t *testing.T) {
	var remoteCalled bool
	oldRunLiteralRemoteCommand := runLiteralRemoteCommandFunc
	oldResolveHostname := resolveHostnameFunc
	defer func() {
		runLiteralRemoteCommandFunc = oldRunLiteralRemoteCommand
		resolveHostnameFunc = oldResolveHostname
	}()

	resolveHostnameFunc = func(host string) (string, error) {
		t.Fatalf("literal target should not resolve hostname, got %q", host)
		return "", nil
	}
	runLiteralRemoteCommandFunc = func(_ context.Context, host string, sshArgs, command []string, _ ...Options) error {
		remoteCalled = true
		if host != "log" {
			t.Fatalf("literal host = %q, want log", host)
		}
		if len(sshArgs) != 2 || sshArgs[0] != "-p" || sshArgs[1] != "2222" {
			t.Fatalf("literal ssh args = %#v", sshArgs)
		}
		if len(command) != 2 || command[0] != "show" || command[1] != "version" {
			t.Fatalf("literal command = %#v", command)
		}
		return nil
	}

	err := ConnectRequest(context.Background(), Request{
		Host:          "log",
		LiteralTarget: true,
		SSHArgs:       []string{"-p", "2222"},
		RemoteCommand: []string{"show", "version"},
	})
	if err != nil {
		t.Fatalf("ConnectRequest: %v", err)
	}
	if !remoteCalled {
		t.Fatal("literal remote command runner was not called")
	}
}

func TestRunResolvedRemoteCommandHighlightsStdoutOnly(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	defer func() { runCapturedCommandFunc = oldRunCapturedCommand }()

	highlightEnabled := true
	var gotReq captured.Request
	runCapturedCommandFunc = func(_ context.Context, req captured.Request) (captured.Result, error) {
		gotReq = req
		return captured.Result{
			Stdout: []byte("set protocols bgp\n"),
			Stderr: []byte("set protocols bgp\n"),
		}, nil
	}

	stdout, stderr := captureOutput(t, func() {
		err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname:  "edge01",
			Username:  "netops",
			Port:      2200,
			Highlight: config.HighlightConfig{Enabled: &highlightEnabled, Profile: config.HighlightProfileJunos},
		}, []string{"-o", "LogLevel=ERROR"}, []string{"show", "configuration"}, config.DefaultConfig(), Options{})
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	})

	if gotReq.Hostname != "edge01" || gotReq.Username != "netops" || gotReq.Port != 2200 {
		t.Fatalf("captured request endpoint = %+v", gotReq)
	}
	if len(gotReq.SSHArgs) != 2 || gotReq.SSHArgs[0] != "-o" || gotReq.SSHArgs[1] != "LogLevel=ERROR" {
		t.Fatalf("captured request ssh args = %#v", gotReq.SSHArgs)
	}
	if len(gotReq.RemoteCommand) != 2 || gotReq.RemoteCommand[0] != "show" || gotReq.RemoteCommand[1] != "configuration" {
		t.Fatalf("captured request command = %#v", gotReq.RemoteCommand)
	}
	if !bytes.Contains([]byte(stdout), []byte("\x1b[")) {
		t.Fatalf("stdout was not highlighted: %q", stdout)
	}
	if stderr != "set protocols bgp\n" {
		t.Fatalf("stderr = %q, want raw unhighlighted text", stderr)
	}
}

func TestRunResolvedRemoteCommandPreservesCapturedOutputOrder(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	defer func() { runCapturedCommandFunc = oldRunCapturedCommand }()

	highlightEnabled := true
	runCapturedCommandFunc = func(_ context.Context, _ captured.Request) (captured.Result, error) {
		return captured.Result{
			Stdout: []byte("set protocols bgp\n"),
			Stderr: []byte("banner\n"),
			Output: []captured.OutputEvent{
				{Stream: captured.StreamStderr, Data: []byte("banner\n")},
				{Stream: captured.StreamStdout, Data: []byte("set protocols bgp\n")},
			},
		}, nil
	}

	stdout, stderr := captureOutput(t, func() {
		err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname:  "edge01",
			Highlight: config.HighlightConfig{Enabled: &highlightEnabled, Profile: config.HighlightProfileJunos},
		}, nil, []string{"show"}, config.DefaultConfig(), Options{})
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	})

	if stdout == "" || stderr != "banner\n" {
		t.Fatalf("stdout=%q stderr=%q, want highlighted stdout and raw stderr", stdout, stderr)
	}
	if !bytes.Contains([]byte(stdout), []byte("\x1b[")) {
		t.Fatalf("stdout was not highlighted: %q", stdout)
	}
}

func TestRunResolvedRemoteCommandLeavesDisabledHighlightUnchanged(t *testing.T) {
	oldRunCapturedCommand := runCapturedCommandFunc
	defer func() { runCapturedCommandFunc = oldRunCapturedCommand }()

	runCapturedCommandFunc = func(_ context.Context, _ captured.Request) (captured.Result, error) {
		return captured.Result{Stdout: []byte("set protocols bgp\n")}, nil
	}

	stdout, _ := captureOutput(t, func() {
		err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
			Hostname: "edge01",
			Port:     22,
		}, nil, []string{"show"}, config.DefaultConfig(), Options{})
		if err != nil {
			t.Fatalf("runResolvedRemoteCommand: %v", err)
		}
	})

	if stdout != "set protocols bgp\n" {
		t.Fatalf("stdout = %q, want unchanged", stdout)
	}
}

func TestRunResolvedRemoteCommandRequiresAskpassHelperForPassword(t *testing.T) {
	oldAskpassHelperPath := askpassHelperPathFunc
	defer func() { askpassHelperPathFunc = oldAskpassHelperPath }()

	askpassHelperPathFunc = func() (string, error) {
		return "", errors.New("nssh-askpass not found")
	}
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	err := runResolvedRemoteCommand(context.Background(), &ResolvedHost{
		Hostname: "edge01",
		Port:     22,
		Credential: &ResolvedCredential{
			Password: password,
		},
	}, nil, []string{"show"}, config.DefaultConfig(), Options{})
	if err == nil || !strings.Contains(err.Error(), "nssh-askpass not found") {
		t.Fatalf("err = %v, want missing askpass helper", err)
	}
}

func TestAppendResolvedHostCompatFixesCreatesProviderHostOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type:   config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{"cbb": {}},
		},
	}
	cfg.Inventory.Provider = cfg.Inventory.Providers
	resolved := &ResolvedHost{
		Canonical: "701-sw37r103c608.expedient.com",
		Hostname:  "701-sw37r103c608.expedient.com",
		Provider:  "netbox-prod",
		Group:     "cbb",
	}

	if err := appendResolvedHostCompatFixes(cfg, resolved, []compat.FloorSelection{
		{Category: compat.CategoryKex, Directive: "KexAlgorithms", Floor: "diffie-hellman-group14-sha1"},
		{Category: compat.CategoryKex, Directive: "KexAlgorithms", Floor: "diffie-hellman-group14-sha1"},
	}); err != nil {
		t.Fatalf("appendResolvedHostCompatFixes: %v", err)
	}

	host := cfg.Inventory.Providers["netbox-prod"].Hosts["701-sw37r103c608.expedient.com"]
	if host.Group != "cbb" {
		t.Fatalf("host group = %q, want cbb", host.Group)
	}
	if got := host.SSH.Compatibility.Kex; got != "diffie-hellman-group14-sha1" {
		t.Fatalf("host compatibility.kex = %q, want group14", got)
	}
}

func TestCompatibilityFixesApplyWithHardenedAlgorithmDefaults(t *testing.T) {
	sshConfig := config.SSHHostConfig{
		Options: config.SSHOptions{
			"KexAlgorithms": config.NewSSHOptionItems(
				"sntrup761x25519-sha512@openssh.com",
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
			),
		},
	}
	output := "Unable to negotiate with 192.0.2.1 port 22: no matching key exchange method found. Their offer: diffie-hellman-group14-sha1,diffie-hellman-group1-sha1"

	fixes := compatibilityFixesToApply(sshConfig, output)

	if len(fixes) != 1 {
		t.Fatalf("fixes = %#v, want one kex fix", fixes)
	}
	if fixes[0].Category != compat.CategoryKex || fixes[0].Floor != "diffie-hellman-group14-sha1" {
		t.Fatalf("fix = %#v, want kex group14 floor", fixes[0])
	}
}

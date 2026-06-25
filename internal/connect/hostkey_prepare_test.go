package connect

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

func TestRunHostKeyPreparationAcceptOnceWritesTemporaryKnownHosts(t *testing.T) {
	oldPrompt := hostKeyPromptFunc
	oldScan := scanHostKeyFunc
	defer func() {
		hostKeyPromptFunc = oldPrompt
		scanHostKeyFunc = oldScan
	}()

	line := "edge01 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBcLcMSBE8+TJuxrHFujWBrOCcXrl+/sTqONstg2Jcg7"
	hostKeyPromptFunc = func() connector.HostKeyPromptFunc {
		return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
			if prompt.Host != "edge01" || prompt.KeyType != "ssh-ed25519" || prompt.Fingerprint == "" {
				t.Fatalf("prompt = %+v", prompt)
			}
			if prompt.Stdin == nil {
				t.Fatal("prompt stdin is nil")
			}
			return connector.HostKeyAcceptOnce
		}
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string) (scannedHostKey, error) {
		return scannedHostKey{
			KeyType:     "ssh-ed25519",
			Fingerprint: "SHA256:Gxy6lCTHwEEnHYy0j4LxmDP+NTRPSv6KRjCJj/q6bYs",
			Line:        line,
		}, nil
	}

	prep, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, false)
	if err != nil {
		t.Fatalf("runHostKeyPreparation: %v", err)
	}
	defer prep.Cleanup()

	data, err := os.ReadFile(prep.TempKnownHosts)
	if err != nil {
		t.Fatalf("read temp known_hosts: %v", err)
	}
	if string(data) != line+"\n" {
		t.Fatalf("temp known_hosts = %q, want %q", data, line+"\\n")
	}
	wantArgs := []string{"-o", "UserKnownHostsFile=" + prep.TempKnownHosts, "-o", "StrictHostKeyChecking=yes"}
	if got := prep.SSHArgs(); !slices.Equal(got, wantArgs) {
		t.Fatalf("SSHArgs() = %#v, want %#v", got, wantArgs)
	}
}

func TestRunHostKeyPreparationMarksChangedHostPrompt(t *testing.T) {
	oldPrompt := hostKeyPromptFunc
	oldScan := scanHostKeyFunc
	defer func() {
		hostKeyPromptFunc = oldPrompt
		scanHostKeyFunc = oldScan
	}()

	var sawChanged bool
	hostKeyPromptFunc = func() connector.HostKeyPromptFunc {
		return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
			sawChanged = prompt.Changed
			return connector.HostKeyReject
		}
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string) (scannedHostKey, error) {
		return scannedHostKey{KeyType: "ssh-ed25519", Fingerprint: "SHA256:test", Line: "edge01 ssh-ed25519 AAAA"}, nil
	}

	_, _ = runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, true)
	if !sawChanged {
		t.Fatal("changed host-key preparation did not mark the prompt as changed")
	}
}

func TestRunHostKeyPreparationRejectsHostKey(t *testing.T) {
	oldPrompt := hostKeyPromptFunc
	oldScan := scanHostKeyFunc
	defer func() {
		hostKeyPromptFunc = oldPrompt
		scanHostKeyFunc = oldScan
	}()

	hostKeyPromptFunc = func() connector.HostKeyPromptFunc {
		return func(connector.HostKeyPrompt) connector.HostKeyAction {
			return connector.HostKeyReject
		}
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string) (scannedHostKey, error) {
		return scannedHostKey{KeyType: "ssh-ed25519", Fingerprint: "SHA256:test", Line: "edge01 ssh-ed25519 AAAA"}, nil
	}

	prep, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, false)
	if err == nil {
		t.Fatal("runHostKeyPreparation returned nil error for reject")
	}
	if prep != nil {
		t.Fatalf("prep = %+v, want nil", prep)
	}
}

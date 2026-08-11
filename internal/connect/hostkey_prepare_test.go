package connect

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"golang.org/x/crypto/ssh"
)

func TestParseNegotiatedHostKey(t *testing.T) {
	algorithm, fingerprint, err := parseNegotiatedHostKey([]byte("debug1: Server host key: ssh-rsa SHA256:acceptedRSA123\n"))
	if err != nil {
		t.Fatalf("parseNegotiatedHostKey: %v", err)
	}
	if algorithm != "ssh-rsa" || fingerprint != "SHA256:acceptedRSA123" {
		t.Fatalf("negotiated key = %q %q", algorithm, fingerprint)
	}
}

func TestKeyScanTypeForNegotiatedAlgorithm(t *testing.T) {
	tests := map[string]string{
		ssh.KeyAlgoRSASHA512: "rsa",
		ssh.KeyAlgoRSA:       "rsa",
		ssh.KeyAlgoECDSA256:  "ecdsa",
		ssh.KeyAlgoED25519:   "ed25519",
	}
	for algorithm, want := range tests {
		if got := keyScanTypeForAlgorithm(algorithm); got != want {
			t.Errorf("keyScanTypeForAlgorithm(%q) = %q, want %q", algorithm, got, want)
		}
	}
}

func TestScannedHostKeyByFingerprintSelectsNegotiatedKey(t *testing.T) {
	ecdsaPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	rsaPrivate, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	ecdsaKey, err := ssh.NewPublicKey(&ecdsaPrivate.PublicKey)
	if err != nil {
		t.Fatalf("create ECDSA SSH key: %v", err)
	}
	rsaKey, err := ssh.NewPublicKey(&rsaPrivate.PublicKey)
	if err != nil {
		t.Fatalf("create RSA SSH key: %v", err)
	}
	output := append([]byte("edge01 "), ssh.MarshalAuthorizedKey(ecdsaKey)...)
	output = append(output, []byte("edge01 ")...)
	output = append(output, ssh.MarshalAuthorizedKey(rsaKey)...)

	got, err := scannedHostKeyByFingerprint(output, ssh.FingerprintSHA256(rsaKey))
	if err != nil {
		t.Fatalf("scannedHostKeyByFingerprint: %v", err)
	}
	if got.KeyType != ssh.KeyAlgoRSA || got.Fingerprint != ssh.FingerprintSHA256(rsaKey) {
		t.Fatalf("selected key = %+v, want negotiated RSA key", got)
	}
}

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
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{
			KeyType:     "ssh-ed25519",
			Algorithm:   "ssh-ed25519",
			Fingerprint: "SHA256:Gxy6lCTHwEEnHYy0j4LxmDP+NTRPSv6KRjCJj/q6bYs",
			Line:        line,
		}, nil
	}

	prep, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, false, nil)
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
	wantArgs := []string{"-o", "HostKeyAlgorithms=ssh-ed25519", "-o", "UserKnownHostsFile=" + prep.TempKnownHosts, "-o", "StrictHostKeyChecking=yes"}
	if got := prep.SSHArgs(); !slices.Equal(got, wantArgs) {
		t.Fatalf("SSHArgs() = %#v, want %#v", got, wantArgs)
	}
}

func TestRunHostKeyPreparationAcceptAlwaysReplacesChangedKnownHosts(t *testing.T) {
	oldPrompt := hostKeyPromptFunc
	oldScan := scanHostKeyFunc
	oldRemove := removeKnownHostsEntryFunc
	defer func() {
		hostKeyPromptFunc = oldPrompt
		scanHostKeyFunc = oldScan
		removeKnownHostsEntryFunc = oldRemove
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	if err := os.WriteFile(knownHostsPath, []byte("edge01 ecdsa-sha2-nistp256 stale\n"), 0600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	line := "edge01 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBcLcMSBE8+TJuxrHFujWBrOCcXrl+/sTqONstg2Jcg7"
	hostKeyPromptFunc = func() connector.HostKeyPromptFunc {
		return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
			if !prompt.Changed {
				t.Fatal("prompt was not marked changed")
			}
			return connector.HostKeyAcceptAlways
		}
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{
			KeyType:     "ssh-ed25519",
			Fingerprint: "SHA256:Gxy6lCTHwEEnHYy0j4LxmDP+NTRPSv6KRjCJj/q6bYs",
			Line:        line,
		}, nil
	}
	var removed []string
	removeKnownHostsEntryFunc = func(target, path string) error {
		if path != knownHostsPath {
			t.Fatalf("known_hosts path = %q, want %q", path, knownHostsPath)
		}
		removed = append(removed, target)
		return os.WriteFile(path, nil, 0600)
	}

	prep, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, true, nil)
	if err != nil {
		t.Fatalf("runHostKeyPreparation: %v", err)
	}
	if prep != nil {
		t.Fatalf("prep = %+v, want nil", prep)
	}
	if !slices.Equal(removed, []string{"edge01"}) {
		t.Fatalf("removed targets = %#v, want edge01", removed)
	}
	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if string(data) != line+"\n" {
		t.Fatalf("known_hosts = %q, want replacement line", data)
	}
}

func TestRunHostKeyPreparationAcceptOnceForChangedHostDoesNotReplaceKnownHosts(t *testing.T) {
	oldPrompt := hostKeyPromptFunc
	oldScan := scanHostKeyFunc
	oldRemove := removeKnownHostsEntryFunc
	defer func() {
		hostKeyPromptFunc = oldPrompt
		scanHostKeyFunc = oldScan
		removeKnownHostsEntryFunc = oldRemove
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	stale := "edge01 ecdsa-sha2-nistp256 stale\n"
	if err := os.WriteFile(knownHostsPath, []byte(stale), 0600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	line := "edge01 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBcLcMSBE8+TJuxrHFujWBrOCcXrl+/sTqONstg2Jcg7"
	hostKeyPromptFunc = func() connector.HostKeyPromptFunc {
		return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
			if !prompt.Changed {
				t.Fatal("prompt was not marked changed")
			}
			return connector.HostKeyAcceptOnce
		}
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{
			KeyType:     "ssh-ed25519",
			Algorithm:   "ssh-ed25519",
			Fingerprint: "SHA256:Gxy6lCTHwEEnHYy0j4LxmDP+NTRPSv6KRjCJj/q6bYs",
			Line:        line,
		}, nil
	}
	removeKnownHostsEntryFunc = func(target, path string) error {
		t.Fatalf("removeKnownHostsEntryFunc(%q, %q) called for accept once", target, path)
		return nil
	}

	prep, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, true, nil)
	if err != nil {
		t.Fatalf("runHostKeyPreparation: %v", err)
	}
	defer prep.Cleanup()

	data, err := os.ReadFile(prep.TempKnownHosts)
	if err != nil {
		t.Fatalf("read temp known_hosts: %v", err)
	}
	if string(data) != line+"\n" {
		t.Fatalf("temp known_hosts = %q, want replacement line", data)
	}
	data, err = os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if string(data) != stale {
		t.Fatalf("known_hosts = %q, want stale entry untouched", data)
	}
}

func TestRunHostKeyPreparationAcceptAlwaysDoesNotRemoveForNewHost(t *testing.T) {
	oldPrompt := hostKeyPromptFunc
	oldScan := scanHostKeyFunc
	oldRemove := removeKnownHostsEntryFunc
	defer func() {
		hostKeyPromptFunc = oldPrompt
		scanHostKeyFunc = oldScan
		removeKnownHostsEntryFunc = oldRemove
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)
	line := "edge01 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBcLcMSBE8+TJuxrHFujWBrOCcXrl+/sTqONstg2Jcg7"
	hostKeyPromptFunc = func() connector.HostKeyPromptFunc {
		return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
			if prompt.Changed {
				t.Fatal("prompt was marked changed")
			}
			return connector.HostKeyAcceptAlways
		}
	}
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{KeyType: "ssh-ed25519", Fingerprint: "SHA256:test", Line: line}, nil
	}
	removeKnownHostsEntryFunc = func(target, path string) error {
		t.Fatalf("removeKnownHostsEntryFunc(%q, %q) called for new host", target, path)
		return nil
	}

	if _, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, false, nil); err != nil {
		t.Fatalf("runHostKeyPreparation: %v", err)
	}
}

func TestKnownHostsRemovalTargetsUsesExplicitPort(t *testing.T) {
	got := knownHostsRemovalTargets(&ResolvedHost{Hostname: "edge01", Port: 22}, []string{"-p", "2200"})
	want := []string{"[edge01]:2200", "edge01"}
	if !slices.Equal(got, want) {
		t.Fatalf("knownHostsRemovalTargets() = %#v, want %#v", got, want)
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
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{KeyType: "ssh-ed25519", Fingerprint: "SHA256:test", Line: "edge01 ssh-ed25519 AAAA"}, nil
	}

	_, _ = runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, true, nil)
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
	scanHostKeyFunc = func(context.Context, *ResolvedHost, []string, *config.Config, Options, []string) (scannedHostKey, error) {
		return scannedHostKey{KeyType: "ssh-ed25519", Fingerprint: "SHA256:test", Line: "edge01 ssh-ed25519 AAAA"}, nil
	}

	prep, err := runHostKeyPreparation(context.Background(), &ResolvedHost{Hostname: "edge01", Port: 22}, nil, config.DefaultConfig(), Options{}, false, nil)
	if err == nil {
		t.Fatal("runHostKeyPreparation returned nil error for reject")
	}
	if prep != nil {
		t.Fatalf("prep = %+v, want nil", prep)
	}
}

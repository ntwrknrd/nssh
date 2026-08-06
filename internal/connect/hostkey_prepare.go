//go:build unix

package connect

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"golang.org/x/crypto/ssh"
)

type scannedHostKey struct {
	KeyType     string
	Algorithm   string
	Fingerprint string
	Line        string
}

var negotiatedHostKeyRE = regexp.MustCompile(`(?m)Server host key: ([^\s]+) (SHA256:[A-Za-z0-9+/=]+)`)

func runHostKeyPreparation(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options, changed bool, proxyEnv []string) (*connector.HostKeyPreparation, error) {
	slog.Debug("preparing host key", "host", resolved.Hostname)
	key, err := scanHostKeyFunc(ctx, resolved, sshArgs, cfg, opts, proxyEnv)
	if err != nil {
		return nil, err
	}
	slog.Debug("host key scanned", "host", resolved.Hostname, "key_type", key.KeyType, "fingerprint", key.Fingerprint)
	action := hostKeyPromptFunc()(connector.HostKeyPrompt{
		Host:        resolved.Hostname,
		KeyType:     key.KeyType,
		Fingerprint: key.Fingerprint,
		Changed:     changed,
		Stdin:       os.Stdin,
	})
	switch action {
	case connector.HostKeyReject:
		return nil, exit.ErrAuthFailed
	case connector.HostKeyAcceptOnce:
		return writeTemporaryKnownHosts(key.Line, key.Algorithm)
	case connector.HostKeyAcceptAlways:
		var err error
		if changed {
			err = replaceKnownHosts(resolved, sshArgs, key.Line)
		} else {
			err = appendKnownHosts(key.Line)
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, exit.ErrAuthFailed
	}
}

func writeTemporaryKnownHosts(line, algorithm string) (*connector.HostKeyPreparation, error) {
	file, err := os.CreateTemp("", "nssh-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("create temp known_hosts: %w", err)
	}
	path := file.Name()
	if _, err := file.WriteString(strings.TrimSpace(line) + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write temp known_hosts: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close temp known_hosts: %w", err)
	}
	return &connector.HostKeyPreparation{TempKnownHosts: path, HostKeyAlgorithm: algorithm}, nil
}

func appendKnownHosts(line string) error {
	path, err := userKnownHostsPath()
	if err != nil {
		return err
	}
	return appendKnownHostsLine(path, line)
}

func replaceKnownHosts(resolved *ResolvedHost, sshArgs []string, line string) error {
	path, err := userKnownHostsPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat known_hosts: %w", err)
		}
	} else {
		for _, target := range knownHostsRemovalTargets(resolved, sshArgs) {
			if err := removeKnownHostsEntryFunc(target, path); err != nil {
				return err
			}
		}
	}
	return appendKnownHostsLine(path, line)
}

func userKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

func appendKnownHostsLine(path string, line string) error {
	sshDir := filepath.Dir(path)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("create .ssh directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(strings.TrimSpace(line) + "\n"); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	return nil
}

func knownHostsRemovalTargets(resolved *ResolvedHost, sshArgs []string) []string {
	if resolved == nil || strings.TrimSpace(resolved.Hostname) == "" {
		return nil
	}
	host := strings.TrimSpace(resolved.Hostname)
	port := fmt.Sprintf("%d", resolved.Port)
	if explicit := explicitConnectSSHPort(sshArgs); explicit != "" {
		port = explicit
	}
	if port == "" || port == "0" || port == "22" {
		return []string{host}
	}
	return []string{fmt.Sprintf("[%s]:%s", host, port), host}
}

func removeKnownHostsEntry(target, path string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	output, err := exec.Command("ssh-keygen", "-R", target, "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove stale known_hosts entry for %s: %w (%s)", target, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func scanHostKey(ctx context.Context, resolved *ResolvedHost, sshArgs []string, cfg *config.Config, opts Options, proxyEnv []string) (scannedHostKey, error) {
	probeArgs := append([]string{"-vv"}, buildHostKeyProbeArgs(resolved, sshArgs, cfg, opts)...)
	probe := exec.CommandContext(ctx, "ssh", probeArgs...)
	probe.Env = append(withoutAskpassEnv(os.Environ()), proxyEnv...)
	probeOutput, _ := probe.CombinedOutput()
	algorithm, fingerprint, err := parseNegotiatedHostKey(probeOutput)
	if err != nil {
		return scannedHostKey{}, err
	}

	host := resolved.Hostname
	port := fmt.Sprintf("%d", resolved.Port)
	if explicit := explicitConnectSSHPort(sshArgs); explicit != "" {
		port = explicit
	}
	if port == "" || port == "0" {
		port = "22"
	}

	args := []string{"-T", "5", "-t", keyScanTypeForAlgorithm(algorithm)}
	if port != "22" {
		args = append(args, "-p", port)
	}
	args = append(args, host)
	slog.Debug("executing ssh-keyscan", "argv", append([]string{"ssh-keyscan"}, args...))
	output, err := exec.CommandContext(ctx, "ssh-keyscan", args...).CombinedOutput()
	if err != nil {
		return scannedHostKey{}, fmt.Errorf("ssh-keyscan failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	key, err := scannedHostKeyByFingerprint(output, fingerprint)
	if err != nil {
		return scannedHostKey{}, err
	}
	key.Algorithm = algorithm
	return key, nil
}

func parseNegotiatedHostKey(output []byte) (algorithm, fingerprint string, err error) {
	matches := negotiatedHostKeyRE.FindSubmatch(output)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("failed to identify the host key negotiated by OpenSSH")
	}
	return string(matches[1]), string(matches[2]), nil
}

func keyScanTypeForAlgorithm(algorithm string) string {
	switch algorithm {
	case ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA:
		return "rsa"
	case "ssh-dss":
		return "dsa"
	case ssh.KeyAlgoED25519:
		return "ed25519"
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return "ecdsa"
	case ssh.KeyAlgoSKECDSA256:
		return "ecdsa-sk"
	case ssh.KeyAlgoSKED25519:
		return "ed25519-sk"
	default:
		return algorithm
	}
}

func scannedHostKeyByFingerprint(output []byte, fingerprint string) (scannedHostKey, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		if ssh.FingerprintSHA256(pubKey) != fingerprint {
			continue
		}
		return scannedHostKey{
			KeyType:     pubKey.Type(),
			Fingerprint: fingerprint,
			Line:        line,
		}, nil
	}
	if err := scanner.Err(); err != nil {
		return scannedHostKey{}, fmt.Errorf("read ssh-keyscan output: %w", err)
	}
	return scannedHostKey{}, fmt.Errorf("ssh-keyscan did not return the host key negotiated by OpenSSH")
}

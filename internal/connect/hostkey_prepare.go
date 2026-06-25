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
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"golang.org/x/crypto/ssh"
)

type scannedHostKey struct {
	KeyType     string
	Fingerprint string
	Line        string
}

func runHostKeyPreparation(ctx context.Context, resolved *ResolvedHost, sshArgs []string, _ *config.Config, _ Options, changed bool) (*connector.HostKeyPreparation, error) {
	slog.Debug("preparing host key", "host", resolved.Hostname)
	key, err := scanHostKeyFunc(ctx, resolved, sshArgs)
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
		return writeTemporaryKnownHosts(key.Line)
	case connector.HostKeyAcceptAlways:
		if err := appendKnownHosts(key.Line); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, exit.ErrAuthFailed
	}
}

func writeTemporaryKnownHosts(line string) (*connector.HostKeyPreparation, error) {
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
	return &connector.HostKeyPreparation{TempKnownHosts: path}, nil
}

func appendKnownHosts(line string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home directory: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("create .ssh directory: %w", err)
	}
	path := filepath.Join(sshDir, "known_hosts")
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

func scanHostKey(ctx context.Context, resolved *ResolvedHost, sshArgs []string) (scannedHostKey, error) {
	host := resolved.Hostname
	port := fmt.Sprintf("%d", resolved.Port)
	if explicit := explicitConnectSSHPort(sshArgs); explicit != "" {
		port = explicit
	}
	if port == "" || port == "0" {
		port = "22"
	}

	args := []string{"-T", "5"}
	if port != "22" {
		args = append(args, "-p", port)
	}
	args = append(args, host)
	slog.Debug("executing ssh-keyscan", "argv", append([]string{"ssh-keyscan"}, args...))
	output, err := exec.CommandContext(ctx, "ssh-keyscan", args...).CombinedOutput()
	if err != nil {
		return scannedHostKey{}, fmt.Errorf("ssh-keyscan failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	key, err := firstScannedHostKey(output)
	if err != nil {
		return scannedHostKey{}, err
	}
	return key, nil
}

func firstScannedHostKey(output []byte) (scannedHostKey, error) {
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
		return scannedHostKey{
			KeyType:     pubKey.Type(),
			Fingerprint: ssh.FingerprintSHA256(pubKey),
			Line:        line,
		}, nil
	}
	if err := scanner.Err(); err != nil {
		return scannedHostKey{}, fmt.Errorf("read ssh-keyscan output: %w", err)
	}
	return scannedHostKey{}, fmt.Errorf("ssh-keyscan returned no usable host keys")
}

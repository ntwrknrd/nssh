package credential

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type passProvider struct {
	command   string
	prefix    string
	hostRefs  map[string]config.CredentialRefConfig
	groupRefs map[string]config.CredentialRefConfig
	runner    passRunner
}

type passRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type passCLIRunner struct {
	command string
}

func newPassProvider(cfg config.CredentialProviderConfig) Provider {
	command := strings.TrimSpace(cfg.Config.Command)
	if command == "" {
		command = "pass"
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Config.Prefix), "/")
	if prefix == "" {
		prefix = "nssh"
	}
	return &passProvider{
		command: command,
		prefix:  prefix,
		runner:  passCLIRunner{command: command},
	}
}

func (r passCLIRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.command, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", r.command, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (p *passProvider) GetHost(host string) (*Record, error) {
	return p.get(p.hostPath(host))
}

func (p *passProvider) GetGroup(group string) (*Record, error) {
	return p.get(p.groupPath(group))
}

func (p *passProvider) get(path string) (*Record, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	out, err := p.runnerOrDefault().Run(context.Background(), nil, "show", path)
	if err != nil {
		if isPassMissing(out, err) {
			return nil, nil
		}
		return nil, err
	}
	record, err := parsePassRecord(path, string(out))
	if err != nil {
		return nil, err
	}
	return record, nil
}

func parsePassRecord(ref, text string) (*Record, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, errors.New("pass entry has empty password")
	}
	username := ""
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "username") {
			username = strings.TrimSpace(value)
			break
		}
	}
	if username == "" {
		return nil, errors.New("pass entry has empty username")
	}
	return &Record{
		Username: username,
		Secret:   secret.NewFromString(lines[0]),
		Ref:      ref,
	}, nil
}

func (p *passProvider) hostPath(host string) string {
	if ref := p.hostRefs[host]; strings.TrimSpace(ref.Ref) != "" {
		return ref.Ref
	}
	return ""
}

func (p *passProvider) groupPath(group string) string {
	if ref := p.groupRefs[group]; strings.TrimSpace(ref.Ref) != "" {
		return ref.Ref
	}
	return ""
}

func (p *passProvider) commandName() string {
	if strings.TrimSpace(p.command) == "" {
		return "pass"
	}
	return p.command
}

func (p *passProvider) runnerOrDefault() passRunner {
	if p.runner != nil {
		return p.runner
	}
	command := p.commandName()
	p.runner = passCLIRunner{command: command}
	return p.runner
}

func isPassMissing(out []byte, err error) bool {
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not in the password store") ||
		strings.Contains(text, "not found") ||
		strings.Contains(text, "is not in the password store")
}

package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type bitwardenProvider struct {
	hostRefs  map[string]config.CredentialRefConfig
	groupRefs map[string]config.CredentialRefConfig
	runner    bwRunner
}

type bwRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type bwCLIRunner struct{}

type bitwardenItem struct {
	ID    string         `json:"id,omitempty"`
	Type  int            `json:"type"`
	Name  string         `json:"name"`
	Login bitwardenLogin `json:"login"`
}

type bitwardenLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func newBitwardenProvider(config.CredentialProviderConfig) Provider {
	return &bitwardenProvider{runner: bwCLIRunner{}}
}

func (bwCLIRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bw", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("bw %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (p *bitwardenProvider) GetHost(host string) (*Record, error) {
	return p.get(p.hostName(host))
}

func (p *bitwardenProvider) GetGroup(group string) (*Record, error) {
	return p.get(p.groupName(group))
}

func (p *bitwardenProvider) get(name string) (*Record, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	item, err := p.getItem(name)
	if err != nil || item == nil {
		return nil, err
	}
	if item.Login.Username == "" && item.Login.Password == "" {
		return nil, nil
	}
	return &Record{
		Username: item.Login.Username,
		Secret:   secret.NewFromString(item.Login.Password),
		Ref:      item.Name,
	}, nil
}

func (p *bitwardenProvider) getItem(name string) (*bitwardenItem, error) {
	out, err := p.runnerOrDefault().Run(context.Background(), nil, "get", "item", name)
	if err != nil {
		if isBitwardenMissing(out, err) {
			return nil, nil
		}
		return nil, err
	}
	var item bitwardenItem
	if err := json.Unmarshal(out, &item); err != nil {
		return nil, fmt.Errorf("parse Bitwarden item %q: %w", name, err)
	}
	return &item, nil
}

func (p *bitwardenProvider) runnerOrDefault() bwRunner {
	if p.runner != nil {
		return p.runner
	}
	p.runner = bwCLIRunner{}
	return p.runner
}

func (p *bitwardenProvider) hostName(host string) string {
	if ref := p.hostRefs[host]; strings.TrimSpace(ref.Ref) != "" {
		return ref.Ref
	}
	return ""
}

func (p *bitwardenProvider) groupName(group string) string {
	if ref := p.groupRefs[group]; strings.TrimSpace(ref.Ref) != "" {
		return ref.Ref
	}
	return ""
}

func isBitwardenMissing(out []byte, err error) bool {
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "couldn't find") ||
		strings.Contains(text, "could not find")
}

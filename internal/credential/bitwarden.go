package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func (p *bitwardenProvider) SetHost(host string, record *Record) error {
	return p.set(p.hostName(host), record)
}

func (p *bitwardenProvider) RemoveHost(host string) (bool, error) {
	return p.remove(p.hostName(host))
}

func (p *bitwardenProvider) GetGroup(group string) (*Record, error) {
	return p.get(p.groupName(group))
}

func (p *bitwardenProvider) SetGroup(group string, record *Record) error {
	return p.set(p.groupName(group), record)
}

func (p *bitwardenProvider) RemoveGroup(group string) (bool, error) {
	return p.remove(p.groupName(group))
}

func (p *bitwardenProvider) Status() Status {
	out, err := p.runnerOrDefault().Run(context.Background(), nil, "status")
	if err != nil {
		return Status{Type: config.CredentialProviderBitwarden, Available: false, Detail: err.Error()}
	}
	return Status{Type: config.CredentialProviderBitwarden, Available: true, Detail: strings.TrimSpace(string(out))}
}

func (p *bitwardenProvider) Capabilities() Capabilities {
	return Capabilities{
		ProviderSessionPolicy: config.ProviderSessionExternal,
		SupportsHostCRUD:      true,
		SupportsGroupCRUD:     true,
		SupportsStatusCheck:   true,
	}
}

func (p *bitwardenProvider) get(name string) (*Record, error) {
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

func (p *bitwardenProvider) set(name string, record *Record) error {
	if record == nil || record.Secret == nil {
		return errors.New("Bitwarden credential requires a secret value")
	}
	if strings.TrimSpace(record.Username) == "" {
		return errors.New("Bitwarden credential requires a username")
	}
	password := ""
	if err := record.Secret.UseString(func(s string) error {
		password = s
		return nil
	}); err != nil {
		return err
	}
	if password == "" {
		return errors.New("Bitwarden credential requires a password")
	}
	item := bitwardenItem{
		Type: 1,
		Name: name,
		Login: bitwardenLogin{
			Username: record.Username,
			Password: password,
		},
	}
	existing, err := p.getItem(name)
	if err != nil {
		return err
	}
	if existing != nil {
		item.ID = existing.ID
	}
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal Bitwarden item: %w", err)
	}
	encoded, err := p.runnerOrDefault().Run(context.Background(), data, "encode")
	if err != nil {
		return err
	}
	encodedArg := strings.TrimSpace(string(encoded))
	if existing == nil {
		_, err = p.runnerOrDefault().Run(context.Background(), nil, "create", "item", encodedArg)
		return err
	}
	target := existing.ID
	if target == "" {
		target = name
	}
	_, err = p.runnerOrDefault().Run(context.Background(), nil, "edit", "item", target, encodedArg)
	return err
}

func (p *bitwardenProvider) remove(name string) (bool, error) {
	item, err := p.getItem(name)
	if err != nil {
		return false, err
	}
	if item == nil {
		return false, nil
	}
	target := item.ID
	if target == "" {
		target = name
	}
	_, err = p.runnerOrDefault().Run(context.Background(), nil, "delete", "item", target)
	return err == nil, err
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

func bitwardenHostName(host string) string {
	return "nssh host " + host
}

func bitwardenGroupName(group string) string {
	return "nssh group " + group
}

func (p *bitwardenProvider) hostName(host string) string {
	if ref := p.hostRefs[host]; strings.TrimSpace(ref.Ref) != "" {
		return ref.Ref
	}
	return bitwardenHostName(host)
}

func (p *bitwardenProvider) groupName(group string) string {
	if ref := p.groupRefs[group]; strings.TrimSpace(ref.Ref) != "" {
		return ref.Ref
	}
	return bitwardenGroupName(group)
}

func isBitwardenMissing(out []byte, err error) bool {
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "couldn't find") ||
		strings.Contains(text, "could not find")
}

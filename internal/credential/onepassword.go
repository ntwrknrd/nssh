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

type onePasswordProvider struct {
	account string
	vault   string
	runner  opRunner
}

type opRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type opCLIRunner struct{}

type onePasswordItem struct {
	Title    string             `json:"title"`
	Category string             `json:"category,omitempty"`
	Fields   []onePasswordField `json:"fields"`
}

type onePasswordField struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Label   string `json:"label"`
	Value   string `json:"value"`
}

func newOnePasswordProvider(cfg config.CredentialProviderDetailConfig) Provider {
	return &onePasswordProvider{account: cfg.Account, vault: cfg.Vault, runner: opCLIRunner{}}
}

func (opCLIRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "op", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("op %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (p *onePasswordProvider) GetHost(host string) (*Record, error) {
	return p.get(scopeHost, host)
}

func (p *onePasswordProvider) SetHost(host string, record *Record) error {
	return p.set(scopeHost, host, record)
}

func (p *onePasswordProvider) RemoveHost(host string) (bool, error) {
	return p.remove(scopeHost, host)
}

func (p *onePasswordProvider) GetGroup(group string) (*Record, error) {
	return p.get(scopeGroup, group)
}

func (p *onePasswordProvider) SetGroup(group string, record *Record) error {
	return p.set(scopeGroup, group, record)
}

func (p *onePasswordProvider) RemoveGroup(group string) (bool, error) {
	return p.remove(scopeGroup, group)
}

func (p *onePasswordProvider) Status() Status {
	if p.runner == nil {
		p.runner = opCLIRunner{}
	}
	if _, err := p.runner.Run(context.Background(), nil, "--version"); err != nil {
		return Status{Type: config.CredentialProvider1Password, Available: false, Detail: err.Error()}
	}
	return Status{Type: config.CredentialProvider1Password, Available: true, Detail: "1Password vault " + p.vault}
}

type credentialScope string

const (
	scopeHost  credentialScope = "host"
	scopeGroup credentialScope = "group"
)

func (p *onePasswordProvider) get(scope credentialScope, name string) (*Record, error) {
	item, err := p.getItem(scope, name)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	username, password := itemField(item, "username"), itemField(item, "password")
	if username == "" && password == "" {
		return nil, nil
	}
	return &Record{Username: username, Secret: secret.NewFromString(password), Ref: itemTitle(scope, name)}, nil
}

func (p *onePasswordProvider) set(scope credentialScope, name string, record *Record) error {
	if record == nil || record.Secret == nil {
		return errors.New("1Password credential requires a secret value")
	}
	password := ""
	if err := record.Secret.UseString(func(s string) error {
		password = s
		return nil
	}); err != nil {
		return err
	}
	item := buildItem(itemTitle(scope, name), record.Username, password)
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal 1Password item: %w", err)
	}

	existing, err := p.getItem(scope, name)
	if err != nil {
		return err
	}
	if existing == nil {
		args := append([]string{"item", "create"}, p.scopeArgs()...)
		args = append(args, "-")
		_, err = p.run(context.Background(), data, args...)
		return err
	}

	args := append([]string{"item", "edit", itemTitle(scope, name)}, p.scopeArgs()...)
	args = append(args, "-")
	_, err = p.run(context.Background(), data, args...)
	return err
}

func (p *onePasswordProvider) remove(scope credentialScope, name string) (bool, error) {
	existing, err := p.getItem(scope, name)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	args := append([]string{"item", "delete", itemTitle(scope, name)}, p.scopeArgs()...)
	_, err = p.run(context.Background(), nil, args...)
	return err == nil, err
}

func (p *onePasswordProvider) getItem(scope credentialScope, name string) (*onePasswordItem, error) {
	args := append([]string{"item", "get", itemTitle(scope, name)}, p.scopeArgs()...)
	args = append(args, "--format", "json", "--reveal")
	out, err := p.run(context.Background(), nil, args...)
	if err != nil {
		if isItemNotFound(out, err) {
			return nil, nil
		}
		return nil, err
	}
	var item onePasswordItem
	if err := json.Unmarshal(out, &item); err != nil {
		return nil, fmt.Errorf("parse 1Password item %q: %w", itemTitle(scope, name), err)
	}
	return &item, nil
}

func (p *onePasswordProvider) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	if p.runner == nil {
		p.runner = opCLIRunner{}
	}
	return p.runner.Run(ctx, stdin, args...)
}

func (p *onePasswordProvider) scopeArgs() []string {
	var args []string
	if p.vault != "" {
		args = append(args, "--vault", p.vault)
	}
	if p.account != "" {
		args = append(args, "--account", p.account)
	}
	return args
}

func itemTitle(scope credentialScope, name string) string {
	return "nssh " + string(scope) + " " + name
}

func buildItem(title, username, password string) onePasswordItem {
	return onePasswordItem{
		Title:    title,
		Category: "LOGIN",
		Fields: []onePasswordField{
			{ID: "username", Type: "STRING", Purpose: "USERNAME", Label: "username", Value: username},
			{ID: "password", Type: "CONCEALED", Purpose: "PASSWORD", Label: "password", Value: password},
		},
	}
}

func itemField(item *onePasswordItem, label string) string {
	for _, field := range item.Fields {
		if strings.EqualFold(field.Label, label) || strings.EqualFold(field.ID, label) {
			return field.Value
		}
	}
	return ""
}

func isItemNotFound(out []byte, err error) bool {
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "isn't an item") ||
		strings.Contains(text, "is not an item") ||
		strings.Contains(text, "does not exist")
}

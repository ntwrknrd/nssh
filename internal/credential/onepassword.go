package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type onePasswordProvider struct {
	name           string
	account        string
	vault          string
	session        string
	hostRefs       map[string]config.CredentialRefConfig
	groupRefs      map[string]config.CredentialRefConfig
	runner         opRunner
	autoStartAgent bool
}

type opRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type agentProviderClient interface {
	ProviderRequest(agent.ProviderRequest) (*agent.ProviderResponse, error)
	Close() error
}

var (
	connectProviderAgent = func() (agentProviderClient, error) { return agent.Connect() }
	spawnRuntimeAgent    = agent.SpawnRuntime
)

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

func newOnePasswordProviderNamed(name string, cfg config.CredentialConfig) Provider {
	session := strings.TrimSpace(cfg.Config.Session)
	if session == "" {
		session = config.ProviderSessionAgentOwned
	}
	return &onePasswordProvider{
		name:           name,
		account:        cfg.Config.Account,
		vault:          cfg.Config.Vault,
		session:        session,
		hostRefs:       cfg.Host,
		groupRefs:      cfg.Group,
		runner:         opCLIRunner{},
		autoStartAgent: true,
	}
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

func (p *onePasswordProvider) GetGroup(group string) (*Record, error) {
	return p.get(scopeGroup, group)
}

func (p *onePasswordProvider) GetRef(ref config.CredentialRefConfig) (*Record, error) {
	if p.usesAgentSession() {
		return p.agentGet(scopeHost, "", ref)
	}
	return p.getRef(ref)
}

type credentialScope string

const (
	scopeHost  credentialScope = "host"
	scopeGroup credentialScope = "group"
)

func (p *onePasswordProvider) get(scope credentialScope, name string) (*Record, error) {
	if ref := p.refForScope(scope, name); ref.Ref != "" {
		if p.usesAgentSession() {
			return p.agentGet(scope, name, ref)
		}
		return p.getRef(ref)
	}
	return nil, nil
}

func (p *onePasswordProvider) usesAgentSession() bool {
	return p.name != "" && strings.TrimSpace(p.session) == config.ProviderSessionAgentOwned
}

func (p *onePasswordProvider) agentGet(scope credentialScope, name string, ref config.CredentialRefConfig) (*Record, error) {
	client, err := connectProviderAgent()
	if errors.Is(err, agent.ErrAgentNotRunning) && p.autoStartAgent {
		if spawnErr := spawnRuntimeAgent(); spawnErr != nil {
			return nil, spawnErr
		}
		client, err = connectProviderAgent()
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	resp, err := client.ProviderRequest(agent.ProviderRequest{
		Provider:    p.name,
		Action:      "get",
		Scope:       string(scope),
		Name:        name,
		Ref:         ref.Ref,
		Username:    ref.Username,
		UsernameRef: ref.UsernameRef,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || !resp.Found {
		return nil, nil
	}
	return &Record{Username: resp.Username, Secret: secret.NewFromString(string(resp.Secret)), Ref: resp.Ref}, nil
}

func (p *onePasswordProvider) getRef(ref config.CredentialRefConfig) (*Record, error) {
	if isOnePasswordItemBaseRef(ref.Ref) {
		username, err := p.resolveUsernameForItemBase(ref)
		if err != nil {
			return nil, err
		}
		password, err := p.readSecretRef(onePasswordFieldRef(ref.Ref, "password"))
		if err != nil {
			return nil, err
		}
		if username == "" && password == "" {
			return nil, nil
		}
		return &Record{Username: username, Secret: secret.NewFromString(password), Ref: ref.Ref}, nil
	}
	if isOnePasswordSecretRef(ref.Ref) {
		username, err := p.resolveUsername(ref)
		if err != nil {
			return nil, err
		}
		password, err := p.readSecretRef(ref.Ref)
		if err != nil {
			return nil, err
		}
		if username == "" && password == "" {
			return nil, nil
		}
		return &Record{Username: username, Secret: secret.NewFromString(password), Ref: ref.Ref}, nil
	}

	item, err := p.getItemByRef(ref.Ref)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, nil
	}
	username := strings.TrimSpace(ref.Username)
	if username == "" && ref.UsernameRef != "" {
		username, err = p.readSecretRef(ref.UsernameRef)
		if err != nil {
			return nil, err
		}
	}
	if username == "" {
		username = itemField(item, "username")
	}
	password := itemField(item, "password")
	if username == "" && password == "" {
		return nil, nil
	}
	return &Record{Username: username, Secret: secret.NewFromString(password), Ref: ref.Ref}, nil
}

func (p *onePasswordProvider) getItemByRef(ref string) (*onePasswordItem, error) {
	args := append([]string{"item", "get", ref}, p.scopeArgs()...)
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
		return nil, fmt.Errorf("parse 1Password item %q: %w", ref, err)
	}
	return &item, nil
}

func (p *onePasswordProvider) readSecretRef(ref string) (string, error) {
	args := []string{"read", ref}
	if p.account != "" {
		args = append(args, "--account", p.account)
	}
	out, err := p.run(context.Background(), nil, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *onePasswordProvider) resolveUsername(ref config.CredentialRefConfig) (string, error) {
	if ref.Username != "" {
		return ref.Username, nil
	}
	usernameRef := ref.UsernameRef
	if usernameRef == "" {
		usernameRef = siblingSecretRef(ref.Ref, "username")
	}
	if usernameRef == "" {
		return "", nil
	}
	return p.readSecretRef(usernameRef)
}

func (p *onePasswordProvider) resolveUsernameForItemBase(ref config.CredentialRefConfig) (string, error) {
	if ref.Username != "" {
		return ref.Username, nil
	}
	usernameRef := ref.UsernameRef
	if usernameRef == "" {
		usernameRef = onePasswordFieldRef(ref.Ref, "username")
	}
	if usernameRef == "" {
		return "", nil
	}
	return p.readSecretRef(usernameRef)
}

func (p *onePasswordProvider) refForScope(scope credentialScope, name string) config.CredentialRefConfig {
	if scope == scopeHost && p.hostRefs != nil {
		return p.hostRefs[name]
	}
	if scope == scopeGroup && p.groupRefs != nil {
		return p.groupRefs[name]
	}
	return config.CredentialRefConfig{}
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

func isOnePasswordSecretRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "op://")
}

func isOnePasswordItemBaseRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	return strings.HasPrefix(ref, "op://") && strings.HasSuffix(ref, "/")
}

func onePasswordFieldRef(ref, field string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasSuffix(ref, "/") {
		return ref + field
	}
	return siblingSecretRef(ref, field)
}

func siblingSecretRef(ref, field string) string {
	ref = strings.TrimSpace(ref)
	idx := strings.LastIndex(ref, "/")
	if idx == -1 || idx == len(ref)-1 {
		return ""
	}
	return ref[:idx+1] + field
}

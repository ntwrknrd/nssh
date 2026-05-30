//go:build linux || darwin

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/ntwrknrd/nssh/internal/config"
)

type RuntimeProvider struct {
	mu          sync.RWMutex
	onePassword map[string]OnePasswordSessionConfig
}

type OnePasswordSessionConfig struct {
	Account string
	Vault   string
	Runner  OnePasswordRunner
}

type OnePasswordRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type opCLIRunner struct{}

type onePasswordSessionItem struct {
	Title  string                    `json:"title"`
	Fields []onePasswordSessionField `json:"fields"`
}

type onePasswordSessionField struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
}

func NewRuntimeProvider() *RuntimeProvider {
	return &RuntimeProvider{onePassword: make(map[string]OnePasswordSessionConfig)}
}

func NewConfiguredRuntimeProvider(cfg *config.Config) *RuntimeProvider {
	provider := NewRuntimeProvider()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	for name, providerCfg := range cfg.Credential.Provider {
		if providerCfg.Type != config.CredentialProvider1Password {
			continue
		}
		session := strings.TrimSpace(providerCfg.Config.Session)
		if session == "" {
			session = config.ProviderSessionAgentOwned
		}
		if session != config.ProviderSessionAgentOwned {
			continue
		}
		provider.Register1Password(name, OnePasswordSessionConfig{
			Account: providerCfg.Config.Account,
			Vault:   providerCfg.Config.Vault,
			Runner:  opCLIRunner{},
		})
	}
	return provider
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

func (p *RuntimeProvider) Mode() string {
	return ModeRuntime
}

func (p *RuntimeProvider) Register1Password(name string, cfg OnePasswordSessionConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.onePassword == nil {
		p.onePassword = make(map[string]OnePasswordSessionConfig)
	}
	p.onePassword[name] = cfg
}

func (p *RuntimeProvider) HandleProviderRequest(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	if req.Provider == "" {
		return ProviderResponse{}, errors.New("provider is required")
	}
	p.mu.RLock()
	cfg, ok := p.onePassword[req.Provider]
	p.mu.RUnlock()
	if !ok {
		return ProviderResponse{}, fmt.Errorf("unknown provider session %q", req.Provider)
	}
	if cfg.Runner == nil {
		return ProviderResponse{}, fmt.Errorf("provider session %q has no runner", req.Provider)
	}
	switch req.Action {
	case "get":
		return p.handleOnePasswordGet(ctx, cfg, req)
	default:
		return ProviderResponse{}, fmt.Errorf("unsupported provider action %q", req.Action)
	}
}

func (p *RuntimeProvider) Close() error {
	return nil
}

func (p *RuntimeProvider) SessionCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.onePassword)
}

func (p *RuntimeProvider) handleOnePasswordGet(ctx context.Context, cfg OnePasswordSessionConfig, req ProviderRequest) (ProviderResponse, error) {
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = "nssh " + req.Scope + " " + req.Name
	}
	username := strings.TrimSpace(req.Username)
	if username == "" && strings.TrimSpace(req.UsernameRef) != "" {
		resolved, err := readOnePasswordSecretRef(ctx, cfg, req.UsernameRef)
		if err != nil {
			return ProviderResponse{}, err
		}
		username = resolved
	}
	if isOnePasswordSecretRef(ref) {
		password, err := readOnePasswordSecretRef(ctx, cfg, ref)
		if err != nil {
			return ProviderResponse{}, err
		}
		if username == "" && password == "" {
			return ProviderResponse{Found: false}, nil
		}
		return ProviderResponse{Found: true, Username: username, Secret: []byte(password), Ref: ref}, nil
	}
	args := []string{"item", "get", ref}
	if cfg.Vault != "" {
		args = append(args, "--vault", cfg.Vault)
	}
	if cfg.Account != "" {
		args = append(args, "--account", cfg.Account)
	}
	args = append(args, "--format", "json", "--reveal")
	out, err := cfg.Runner.Run(ctx, nil, args...)
	if err != nil {
		if isRuntimeItemNotFound(out, err) {
			return ProviderResponse{Found: false}, nil
		}
		return ProviderResponse{}, err
	}
	var item onePasswordSessionItem
	if err := json.Unmarshal(out, &item); err != nil {
		return ProviderResponse{}, fmt.Errorf("parse 1Password item %q: %w", ref, err)
	}
	if username == "" {
		username = sessionItemField(item, "username")
	}
	password := sessionItemField(item, "password")
	if username == "" && password == "" {
		return ProviderResponse{Found: false}, nil
	}
	return ProviderResponse{Found: true, Username: username, Secret: []byte(password), Ref: ref}, nil
}

func readOnePasswordSecretRef(ctx context.Context, cfg OnePasswordSessionConfig, ref string) (string, error) {
	args := []string{"read", strings.TrimSpace(ref)}
	if cfg.Account != "" {
		args = append(args, "--account", cfg.Account)
	}
	out, err := cfg.Runner.Run(ctx, nil, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isOnePasswordSecretRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "op://")
}

func sessionItemField(item onePasswordSessionItem, label string) string {
	for _, field := range item.Fields {
		if strings.EqualFold(field.Label, label) || strings.EqualFold(field.ID, label) {
			return field.Value
		}
	}
	return ""
}

func isRuntimeItemNotFound(out []byte, err error) bool {
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "isn't an item") ||
		strings.Contains(text, "is not an item") ||
		strings.Contains(text, "does not exist")
}

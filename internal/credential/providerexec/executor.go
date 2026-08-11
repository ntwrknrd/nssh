package providerexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/sopsdoc"
)

// ErrBitwardenNotAuthenticated is returned when a Bitwarden provider needs a
// BW_SESSION token before it can resolve credential refs.
const ErrBitwardenNotAuthenticated = "bitwarden credential provider is not authenticated"

// ProviderRequest describes a provider-scoped credential operation.
type ProviderRequest struct {
	Provider    string `json:"provider"`
	Action      string `json:"action"`
	Scope       string `json:"scope,omitempty"`
	Name        string `json:"name,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Username    string `json:"username,omitempty"`
	UsernameRef string `json:"username_ref,omitempty"`
	Session     string `json:"session,omitempty"`
}

// ProviderResponse returns a request-scoped credential record.
type ProviderResponse struct {
	Found    bool   `json:"found"`
	Username string `json:"username,omitempty"`
	Secret   []byte `json:"secret,omitempty"`
	Ref      string `json:"ref,omitempty"`
}

type Executor struct {
	mu          sync.RWMutex
	onePassword map[string]*OnePasswordProviderConfig
	sopsAge     map[string]*SOPSAgeProviderConfig
	bitwarden   map[string]*BitwardenProviderConfig
}

type OnePasswordProviderConfig struct {
	Account string
	Vault   string
	Runner  OnePasswordRunner
}

type OnePasswordRunner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

type SOPSAgeProviderConfig struct {
	File       string
	AgeKeyFile string
	Runner     sopsdoc.Runner
}

type BitwardenProviderConfig struct {
	Runner BitwardenRunner
}

type BitwardenRunner interface {
	Run(ctx context.Context, env []string, stdin []byte, args ...string) ([]byte, error)
}

type OPCLIRunner struct{}
type BWCLIRunner struct{}

type onePasswordSessionItem struct {
	Title  string                    `json:"title"`
	Fields []onePasswordSessionField `json:"fields"`
}

type onePasswordSessionField struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type bitwardenSessionItem struct {
	Name  string                `json:"name"`
	Login bitwardenSessionLogin `json:"login"`
}

type bitwardenSessionLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func NewExecutor() *Executor {
	return &Executor{
		onePassword: make(map[string]*OnePasswordProviderConfig),
		sopsAge:     make(map[string]*SOPSAgeProviderConfig),
		bitwarden:   make(map[string]*BitwardenProviderConfig),
	}
}

func NewConfiguredExecutor(cfg *config.Config) *Executor {
	executor := NewExecutor()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	for name, providerCfg := range cfg.Credential.Provider {
		switch providerCfg.Type {
		case config.CredentialProvider1Password:
			executor.Register1Password(name, OnePasswordProviderConfig{
				Account: firstNonEmpty(providerCfg.Account, providerCfg.Config.Account),
				Vault:   firstNonEmpty(providerCfg.Vault, providerCfg.Config.Vault),
				Runner:  OPCLIRunner{},
			})
		case config.CredentialProviderSOPSAge:
			executor.RegisterSOPSAge(name, SOPSAgeProviderConfig{
				File:       firstNonEmpty(providerCfg.File, providerCfg.Config.File),
				AgeKeyFile: firstNonEmpty(providerCfg.AgeKeyFile, providerCfg.Config.AgeKeyFile),
				Runner:     sopsdoc.CLIRunner{Command: "sops"},
			})
		case config.CredentialProviderBitwarden:
			executor.RegisterBitwarden(name, BitwardenProviderConfig{
				Runner: BWCLIRunner{},
			})
		}
	}
	return executor
}

func (OPCLIRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
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

func (BWCLIRunner) Run(ctx context.Context, env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bw", args...)
	cmd.Env = append(cmd.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("bw %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (e *Executor) Register1Password(name string, cfg OnePasswordProviderConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.onePassword == nil {
		e.onePassword = make(map[string]*OnePasswordProviderConfig)
	}
	if cfg.Runner == nil {
		cfg.Runner = OPCLIRunner{}
	}
	e.onePassword[name] = &cfg
}

func (e *Executor) RegisterSOPSAge(name string, cfg SOPSAgeProviderConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sopsAge == nil {
		e.sopsAge = make(map[string]*SOPSAgeProviderConfig)
	}
	if cfg.Runner == nil {
		cfg.Runner = sopsdoc.CLIRunner{Command: "sops"}
	}
	e.sopsAge[name] = &cfg
}

func (e *Executor) RegisterBitwarden(name string, cfg BitwardenProviderConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bitwarden == nil {
		e.bitwarden = make(map[string]*BitwardenProviderConfig)
	}
	if cfg.Runner == nil {
		cfg.Runner = BWCLIRunner{}
	}
	e.bitwarden[name] = &cfg
}

func (e *Executor) ProviderCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.onePassword) + len(e.sopsAge) + len(e.bitwarden)
}

func (e *Executor) ProviderNames() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.onePassword)+len(e.sopsAge)+len(e.bitwarden))
	for name := range e.onePassword {
		names = append(names, name)
	}
	for name := range e.sopsAge {
		names = append(names, name)
	}
	for name := range e.bitwarden {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e *Executor) HandleProviderRequest(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	if req.Provider == "" {
		return ProviderResponse{}, errors.New("provider is required")
	}
	e.mu.RLock()
	opCfg, ok := e.onePassword[req.Provider]
	sopsCfg, sopsOK := e.sopsAge[req.Provider]
	bwCfg, bwOK := e.bitwarden[req.Provider]
	e.mu.RUnlock()
	if !ok && !sopsOK && !bwOK {
		return ProviderResponse{}, fmt.Errorf("unknown credential provider %q", req.Provider)
	}
	switch req.Action {
	case "get":
		if ok {
			if opCfg.Runner == nil {
				return ProviderResponse{}, fmt.Errorf("credential provider %q has no runner", req.Provider)
			}
			return e.handleOnePasswordGet(ctx, opCfg, req)
		}
		if sopsOK {
			return e.handleSOPSAgeGet(ctx, sopsCfg, req)
		}
		return e.handleBitwardenGet(ctx, bwCfg, req)
	default:
		return ProviderResponse{}, fmt.Errorf("unsupported provider action %q", req.Action)
	}
}

func (e *Executor) handleBitwardenGet(ctx context.Context, cfg *BitwardenProviderConfig, req ProviderRequest) (ProviderResponse, error) {
	if cfg == nil {
		return ProviderResponse{}, errors.New("credential provider is nil")
	}
	if cfg.Runner == nil {
		return ProviderResponse{}, errors.New("bitwarden credential provider has no runner")
	}
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return ProviderResponse{}, errors.New(ErrBitwardenNotAuthenticated)
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		return ProviderResponse{Found: false}, nil
	}
	out, err := cfg.Runner.Run(ctx, []string{"BW_SESSION=" + session}, nil, "get", "item", ref)
	if err != nil {
		if isRuntimeItemNotFound(out, err) {
			return ProviderResponse{Found: false}, nil
		}
		return ProviderResponse{}, err
	}
	var item bitwardenSessionItem
	if err := json.Unmarshal(out, &item); err != nil {
		return ProviderResponse{}, fmt.Errorf("parse Bitwarden item %q: %w", ref, err)
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = item.Login.Username
	}
	password := item.Login.Password
	if username == "" && password == "" {
		return ProviderResponse{Found: false}, nil
	}
	return ProviderResponse{Found: true, Username: username, Secret: []byte(password), Ref: item.Name}, nil
}

func (e *Executor) handleSOPSAgeGet(ctx context.Context, cfg *SOPSAgeProviderConfig, req ProviderRequest) (ProviderResponse, error) {
	if cfg == nil {
		return ProviderResponse{}, errors.New("credential provider is nil")
	}
	doc, err := sopsdoc.Decrypt(ctx, cfg.Runner, cfg.File, cfg.AgeKeyFile)
	if err != nil {
		return ProviderResponse{}, err
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = defaultSOPSRef(req.Scope, req.Name)
	}
	password, ok, err := doc.Lookup(ref)
	if err != nil || !ok {
		return ProviderResponse{Found: false}, err
	}
	username := strings.TrimSpace(req.Username)
	if username == "" && strings.TrimSpace(req.UsernameRef) != "" {
		resolved, usernameOK, err := doc.Lookup(req.UsernameRef)
		if err != nil {
			return ProviderResponse{}, err
		}
		if usernameOK {
			username = resolved
		}
	}
	return ProviderResponse{Found: true, Username: username, Secret: []byte(password), Ref: ref}, nil
}

func (e *Executor) handleOnePasswordGet(ctx context.Context, cfg *OnePasswordProviderConfig, req ProviderRequest) (ProviderResponse, error) {
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
	if isOnePasswordItemBaseRef(ref) {
		if username == "" {
			resolved, err := readOnePasswordSecretRef(ctx, cfg, onePasswordFieldRef(ref, "username"))
			if err != nil {
				return ProviderResponse{}, err
			}
			username = resolved
		}
		password, err := readOnePasswordSecretRef(ctx, cfg, onePasswordFieldRef(ref, "password"))
		if err != nil {
			return ProviderResponse{}, err
		}
		if username == "" && password == "" {
			return ProviderResponse{Found: false}, nil
		}
		return ProviderResponse{Found: true, Username: username, Secret: []byte(password), Ref: ref}, nil
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
	out, err := runOnePasswordWithSignin(ctx, cfg, args...)
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

func readOnePasswordSecretRef(ctx context.Context, cfg *OnePasswordProviderConfig, ref string) (string, error) {
	args := []string{"read", strings.TrimSpace(ref)}
	if cfg.Account != "" {
		args = append(args, "--account", cfg.Account)
	}
	out, err := runOnePasswordWithSignin(ctx, cfg, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runOnePasswordWithSignin(ctx context.Context, cfg *OnePasswordProviderConfig, args ...string) ([]byte, error) {
	out, err := cfg.Runner.Run(ctx, nil, args...)
	if err == nil || !isOnePasswordNotSignedIn(out, err) {
		return out, err
	}
	signinArgs := []string{"signin"}
	if cfg.Account != "" {
		signinArgs = append(signinArgs, "--account", cfg.Account)
	}
	if _, signinErr := cfg.Runner.Run(ctx, nil, signinArgs...); signinErr != nil {
		return nil, signinErr
	}
	return cfg.Runner.Run(ctx, nil, args...)
}

func isOnePasswordNotSignedIn(out []byte, err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(text, "not signed in")
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
	idx := strings.LastIndex(ref, "/")
	if idx == -1 || idx == len(ref)-1 {
		return ""
	}
	return ref[:idx+1] + field
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

func defaultSOPSRef(scope, name string) string {
	name = strings.Trim(strings.ReplaceAll(strings.TrimSpace(name), "/", "."), ".")
	switch strings.TrimSpace(scope) {
	case "group":
		if name == "" {
			return ""
		}
		return "groups." + name + ".password"
	case "host":
		if name == "" {
			return ""
		}
		return "hosts." + name + ".password"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

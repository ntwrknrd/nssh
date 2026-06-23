//go:build linux || darwin

package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
	"github.com/ntwrknrd/nssh/internal/credential/sopsdoc"
)

type RuntimeProvider struct {
	mu          sync.RWMutex
	executor    *providerexec.Executor
	onePassword map[string]*OnePasswordProviderConfig
	bitwarden   map[string]*BitwardenProviderConfig
}

type OnePasswordProviderConfig struct {
	Account           string
	Vault             string
	Runner            OnePasswordRunner
	Keepalive         bool
	KeepaliveInterval time.Duration
	KeepaliveTimeout  time.Duration

	mu                   sync.RWMutex
	keepaliveState       string
	keepaliveCancel      context.CancelFunc
	keepaliveLastSuccess time.Time
	keepaliveNext        time.Time
	keepaliveLastError   string
}

type OnePasswordRunner = providerexec.OnePasswordRunner

type SOPSAgeProviderConfig = providerexec.SOPSAgeProviderConfig

type BitwardenProviderConfig struct {
	Runner      BitwardenRunner
	WarmSession bool
	Session     string
	mu          sync.RWMutex
}

type BitwardenRunner = providerexec.BitwardenRunner

type opCLIRunner = providerexec.OPCLIRunner
type bwAgentCLIRunner = providerexec.BWCLIRunner

const (
	onePasswordKeepaliveDisabled  = "disabled"
	onePasswordKeepaliveIdle      = "idle"
	onePasswordKeepaliveActive    = "active"
	onePasswordKeepaliveSuspended = "suspended"
)

type AccessStatus struct {
	Name                 string
	Type                 string
	OnePasswordKeepalive bool
	OnePasswordState     string
	KeepaliveInterval    int64
	KeepaliveNextUnix    int64
	KeepaliveLastSuccess int64
	BitwardenWarmSession bool
	BitwardenWarmActive  bool
	LastError            string
}

func NewRuntimeProvider() *RuntimeProvider {
	return &RuntimeProvider{
		executor:    providerexec.NewExecutor(),
		onePassword: make(map[string]*OnePasswordProviderConfig),
		bitwarden:   make(map[string]*BitwardenProviderConfig),
	}
}

func NewConfiguredRuntimeProvider(cfg *config.Config) *RuntimeProvider {
	provider := NewRuntimeProvider()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	for name, providerCfg := range cfg.Credential.Provider {
		switch providerCfg.Type {
		case config.CredentialProvider1Password:
			provider.Register1Password(name, OnePasswordProviderConfig{
				Account:           firstNonEmpty(providerCfg.Account, providerCfg.Config.Account),
				Vault:             firstNonEmpty(providerCfg.Vault, providerCfg.Config.Vault),
				Runner:            opCLIRunner{},
				Keepalive:         providerCfg.Keepalive,
				KeepaliveInterval: providerCfg.KeepaliveInterval.Duration(),
				KeepaliveTimeout:  providerCfg.KeepaliveTimeout.Duration(),
			})
		case config.CredentialProviderSOPSAge:
			provider.RegisterSOPSAge(name, SOPSAgeProviderConfig{
				File:       firstNonEmpty(providerCfg.File, providerCfg.Config.File),
				AgeKeyFile: firstNonEmpty(providerCfg.AgeKeyFile, providerCfg.Config.AgeKeyFile),
				Runner:     sopsdoc.CLIRunner{Command: "sops"},
			})
		case config.CredentialProviderBitwarden:
			provider.RegisterBitwarden(name, BitwardenProviderConfig{
				Runner:      bwAgentCLIRunner{},
				WarmSession: providerCfg.WarmSession,
			})
		}
	}
	return provider
}

func (p *RuntimeProvider) Register1Password(name string, cfg OnePasswordProviderConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executor == nil {
		p.executor = providerexec.NewExecutor()
	}
	if p.onePassword == nil {
		p.onePassword = make(map[string]*OnePasswordProviderConfig)
	}
	if cfg.Runner == nil {
		cfg.Runner = opCLIRunner{}
	}
	if cfg.KeepaliveInterval == 0 {
		cfg.KeepaliveInterval = 5 * time.Minute
	}
	if cfg.KeepaliveTimeout == 0 {
		cfg.KeepaliveTimeout = 10 * time.Second
	}
	if cfg.Keepalive {
		cfg.keepaliveState = onePasswordKeepaliveIdle
	} else {
		cfg.keepaliveState = onePasswordKeepaliveDisabled
	}
	p.executor.Register1Password(name, providerexec.OnePasswordProviderConfig{
		Account: cfg.Account,
		Vault:   cfg.Vault,
		Runner:  cfg.Runner,
	})
	p.onePassword[name] = &cfg
}

func (p *RuntimeProvider) RegisterSOPSAge(name string, cfg SOPSAgeProviderConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executor == nil {
		p.executor = providerexec.NewExecutor()
	}
	p.executor.RegisterSOPSAge(name, cfg)
}

func (p *RuntimeProvider) RegisterBitwarden(name string, cfg BitwardenProviderConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executor == nil {
		p.executor = providerexec.NewExecutor()
	}
	if p.bitwarden == nil {
		p.bitwarden = make(map[string]*BitwardenProviderConfig)
	}
	if cfg.Runner == nil {
		cfg.Runner = bwAgentCLIRunner{}
	}
	p.executor.RegisterBitwarden(name, providerexec.BitwardenProviderConfig{
		Runner: cfg.Runner,
	})
	p.bitwarden[name] = &cfg
}

func (p *RuntimeProvider) HandleProviderRequest(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	if req.Provider == "" {
		return ProviderResponse{}, errors.New("provider is required")
	}
	p.mu.RLock()
	opCfg := p.onePassword[req.Provider]
	bwCfg := p.bitwarden[req.Provider]
	executor := p.executor
	p.mu.RUnlock()
	if executor == nil {
		return ProviderResponse{}, fmt.Errorf("unknown credential provider %q", req.Provider)
	}
	switch req.Action {
	case "auth":
		if bwCfg == nil {
			return ProviderResponse{}, fmt.Errorf("credential provider %q does not support auth", req.Provider)
		}
		return p.handleBitwardenAuth(bwCfg, req)
	case "get":
		if bwCfg != nil && strings.TrimSpace(req.Session) == "" {
			bwCfg.mu.RLock()
			req.Session = strings.TrimSpace(bwCfg.Session)
			bwCfg.mu.RUnlock()
		}
		resp, err := executor.HandleProviderRequest(ctx, req)
		if err == nil && resp.Found && opCfg != nil {
			opCfg.armKeepalive()
		}
		return resp, err
	default:
		return ProviderResponse{}, fmt.Errorf("unsupported provider action %q", req.Action)
	}
}

func (p *RuntimeProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, cfg := range p.bitwarden {
		if cfg == nil {
			continue
		}
		cfg.mu.Lock()
		cfg.Session = ""
		cfg.mu.Unlock()
	}
	for _, cfg := range p.onePassword {
		if cfg == nil {
			continue
		}
		cfg.stopKeepalive()
	}
	return nil
}

func (p *RuntimeProvider) ProviderCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.executor == nil {
		return 0
	}
	return p.executor.ProviderCount()
}

func (p *RuntimeProvider) ProviderNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.executor == nil {
		return nil
	}
	return p.executor.ProviderNames()
}

func (p *RuntimeProvider) AccessStatus() []AccessStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entries := make([]AccessStatus, 0)
	for name, cfg := range p.onePassword {
		if cfg == nil || !cfg.Keepalive {
			continue
		}
		entries = append(entries, cfg.accessStatus(name))
	}
	for name, cfg := range p.bitwarden {
		if cfg == nil || !cfg.WarmSession {
			continue
		}
		cfg.mu.RLock()
		sessionActive := strings.TrimSpace(cfg.Session) != ""
		cfg.mu.RUnlock()
		entries = append(entries, AccessStatus{
			Name:                 name,
			Type:                 config.CredentialProviderBitwarden,
			BitwardenWarmSession: true,
			BitwardenWarmActive:  sessionActive,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (p *RuntimeProvider) handleBitwardenAuth(cfg *BitwardenProviderConfig, req ProviderRequest) (ProviderResponse, error) {
	if cfg == nil {
		return ProviderResponse{}, errors.New("credential provider is nil")
	}
	session := strings.TrimSpace(req.Session)
	if session == "" {
		return ProviderResponse{}, errors.New("bitwarden session is required")
	}
	if !cfg.WarmSession {
		return ProviderResponse{Found: true}, nil
	}
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	cfg.Session = session
	return ProviderResponse{Found: true}, nil
}

func (cfg *OnePasswordProviderConfig) armKeepalive() {
	if cfg == nil || !cfg.Keepalive {
		return
	}
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	if cfg.keepaliveState == onePasswordKeepaliveActive {
		return
	}
	if cfg.keepaliveCancel != nil {
		cfg.keepaliveCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cfg.keepaliveCancel = cancel
	cfg.keepaliveState = onePasswordKeepaliveActive
	cfg.keepaliveLastError = ""
	cfg.keepaliveNext = time.Now().Add(cfg.KeepaliveInterval)
	go cfg.runKeepalive(ctx)
}

func (cfg *OnePasswordProviderConfig) stopKeepalive() {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	if cfg.keepaliveCancel != nil {
		cfg.keepaliveCancel()
		cfg.keepaliveCancel = nil
	}
}

func (cfg *OnePasswordProviderConfig) runKeepalive(ctx context.Context) {
	for {
		cfg.mu.RLock()
		interval := cfg.KeepaliveInterval
		cfg.mu.RUnlock()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := cfg.runKeepaliveTick(ctx); err != nil {
			cfg.mu.Lock()
			cfg.keepaliveState = onePasswordKeepaliveSuspended
			cfg.keepaliveLastError = sanitizeProviderError(err)
			cfg.keepaliveCancel = nil
			cfg.mu.Unlock()
			return
		}
		cfg.mu.Lock()
		cfg.keepaliveLastSuccess = time.Now()
		cfg.keepaliveNext = cfg.keepaliveLastSuccess.Add(cfg.KeepaliveInterval)
		cfg.keepaliveLastError = ""
		cfg.mu.Unlock()
	}
}

func (cfg *OnePasswordProviderConfig) runKeepaliveTick(parent context.Context) error {
	timeout := cfg.KeepaliveTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	args := []string{"whoami"}
	if strings.TrimSpace(cfg.Account) != "" {
		args = append(args, "--account", cfg.Account)
	}
	_, err := cfg.Runner.Run(ctx, nil, args...)
	return err
}

func (cfg *OnePasswordProviderConfig) accessStatus(name string) AccessStatus {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	status := AccessStatus{
		Name:                 name,
		Type:                 config.CredentialProvider1Password,
		OnePasswordKeepalive: true,
		OnePasswordState:     cfg.keepaliveState,
		KeepaliveInterval:    int64(cfg.KeepaliveInterval.Seconds()),
		LastError:            cfg.keepaliveLastError,
	}
	if !cfg.keepaliveLastSuccess.IsZero() {
		status.KeepaliveLastSuccess = cfg.keepaliveLastSuccess.Unix()
	}
	if !cfg.keepaliveNext.IsZero() {
		status.KeepaliveNextUnix = cfg.keepaliveNext.Unix()
	}
	return status
}

func sanitizeProviderError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "deadline") || strings.Contains(text, "timeout"):
		return "timeout"
	case strings.Contains(text, "not signed in") || strings.Contains(text, "signin"):
		return "not signed in"
	default:
		return "provider command failed"
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

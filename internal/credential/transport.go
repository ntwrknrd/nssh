package credential

import (
	"context"
	"errors"
	"time"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
)

type providerRequestExecutor interface {
	HandleProviderRequest(context.Context, providerexec.ProviderRequest) (providerexec.ProviderResponse, error)
}

type providerTransport interface {
	ProviderRequest(providerexec.ProviderRequest) (*providerexec.ProviderResponse, error)
}

type directProviderTransport struct {
	executor providerRequestExecutor
	timeout  time.Duration
	ctx      context.Context
}

type agentProviderTransport struct {
	autoStart bool
	ctx       context.Context
}

var newConfiguredProviderExecutor = func(cfg *config.Config) providerRequestExecutor {
	return providerexec.NewConfiguredExecutor(cfg)
}

func newProviderTransport(providerCfg config.CredentialProviderConfig, cfg *config.Config, executor providerRequestExecutor) providerTransport {
	if usesAgentTransport(providerCfg) {
		return agentProviderTransport{autoStart: cfg.Agent.AutoStart}
	}
	timeout := cfg.Agent.ProviderRequestTimeout.Duration()
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return directProviderTransport{executor: executor, timeout: timeout}
}

func usesAgentTransport(providerCfg config.CredentialProviderConfig) bool {
	switch providerCfg.Type {
	case config.CredentialProvider1Password:
		return providerCfg.Keepalive
	case config.CredentialProviderBitwarden:
		return providerCfg.WarmSession
	default:
		return false
	}
}

func (t directProviderTransport) ProviderRequest(req providerexec.ProviderRequest) (*providerexec.ProviderResponse, error) {
	if t.executor == nil {
		return nil, errors.New("direct credential provider transport has no executor")
	}
	ctx := t.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if t.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
	}
	defer cancel()
	resp, err := t.executor.HandleProviderRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (t agentProviderTransport) ProviderRequest(req providerexec.ProviderRequest) (*providerexec.ProviderResponse, error) {
	if t.ctx != nil && t.ctx.Err() != nil {
		return nil, t.ctx.Err()
	}
	client, err := connectProviderAgent()
	if errors.Is(err, agent.ErrAgentNotRunning) && t.autoStart {
		if t.ctx != nil && t.ctx.Err() != nil {
			return nil, t.ctx.Err()
		}
		if spawnErr := spawnRuntimeAgent(); spawnErr != nil {
			return nil, spawnErr
		}
		if t.ctx != nil && t.ctx.Err() != nil {
			return nil, t.ctx.Err()
		}
		client, err = connectProviderAgent()
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	if t.ctx != nil {
		if err := t.ctx.Err(); err != nil {
			return nil, err
		}
		stop := context.AfterFunc(t.ctx, func() { _ = client.Close() })
		defer stop()
	}
	return client.ProviderRequest(req)
}

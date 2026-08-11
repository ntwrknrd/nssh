package credential

import (
	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type onePasswordProvider struct {
	name      string
	hostRefs  map[string]config.CredentialRefConfig
	groupRefs map[string]config.CredentialRefConfig
	transport providerTransport
}

type agentProviderClient interface {
	ProviderRequest(agent.ProviderRequest) (*agent.ProviderResponse, error)
	Close() error
}

var (
	connectProviderAgent = func() (agentProviderClient, error) { return agent.Connect() }
	spawnRuntimeAgent    = agent.SpawnRuntime
)

func newOnePasswordProviderNamed(name string, cfg config.CredentialConfig) Provider {
	return &onePasswordProvider{
		name:      name,
		hostRefs:  cfg.Host,
		groupRefs: cfg.Group,
		transport: agentProviderTransport{autoStart: true},
	}
}

func (p *onePasswordProvider) GetHost(host string) (*Record, error) {
	return p.get(scopeHost, host)
}

func (p *onePasswordProvider) GetGroup(group string) (*Record, error) {
	return p.get(scopeGroup, group)
}

func (p *onePasswordProvider) GetRef(ref config.CredentialRefConfig) (*Record, error) {
	return p.transportGet(scopeHost, "", ref)
}

type credentialScope string

const (
	scopeHost  credentialScope = "host"
	scopeGroup credentialScope = "group"
)

func (p *onePasswordProvider) get(scope credentialScope, name string) (*Record, error) {
	if ref := p.refForScope(scope, name); ref.Ref != "" {
		return p.transportGet(scope, name, ref)
	}
	return nil, nil
}

func (p *onePasswordProvider) transportGet(scope credentialScope, name string, ref config.CredentialRefConfig) (*Record, error) {
	transport := p.transport
	if transport == nil {
		transport = agentProviderTransport{autoStart: true}
	}
	resp, err := transport.ProviderRequest(providerexec.ProviderRequest{
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

func (p *onePasswordProvider) refForScope(scope credentialScope, name string) config.CredentialRefConfig {
	if scope == scopeHost && p.hostRefs != nil {
		return p.hostRefs[name]
	}
	if scope == scopeGroup && p.groupRefs != nil {
		return p.groupRefs[name]
	}
	return config.CredentialRefConfig{}
}

package credential

import (
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type sopsAgeProvider struct {
	name      string
	hostRefs  map[string]config.CredentialRefConfig
	groupRefs map[string]config.CredentialRefConfig
	transport providerTransport
}

func newSOPSAgeProvider(_ config.CredentialProviderConfig) Provider {
	return &sopsAgeProvider{}
}

func newSOPSAgeProviderNamed(name string, cfg config.CredentialProviderConfig) Provider {
	provider := newSOPSAgeProvider(cfg).(*sopsAgeProvider)
	provider.name = name
	return provider
}

func (p *sopsAgeProvider) GetHost(host string) (*Record, error) {
	return p.get(scopeHost, host, p.hostRefs[host])
}

func (p *sopsAgeProvider) GetGroup(group string) (*Record, error) {
	return p.get(scopeGroup, group, p.groupRefs[group])
}

func (p *sopsAgeProvider) GetRef(ref config.CredentialRefConfig) (*Record, error) {
	return p.get(scopeHost, "", ref)
}

func (p *sopsAgeProvider) get(scope credentialScope, name string, ref config.CredentialRefConfig) (*Record, error) {
	if strings.TrimSpace(ref.Ref) == "" {
		return nil, nil
	}
	return p.transportGet(scope, name, ref)
}

func (p *sopsAgeProvider) transportGet(scope credentialScope, name string, ref config.CredentialRefConfig) (*Record, error) {
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

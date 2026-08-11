package credential

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential/providerexec"
	"github.com/ntwrknrd/nssh/internal/secret"
)

var unlockBitwardenProvider = runBitwardenUnlock

type bitwardenProvider struct {
	name        string
	hostRefs    map[string]config.CredentialRefConfig
	groupRefs   map[string]config.CredentialRefConfig
	warmSession bool
	transport   providerTransport
}

func newBitwardenProviderNamed(name string, cfg config.CredentialProviderConfig) Provider {
	return &bitwardenProvider{
		name:        name,
		hostRefs:    map[string]config.CredentialRefConfig{},
		groupRefs:   map[string]config.CredentialRefConfig{},
		warmSession: cfg.WarmSession,
		transport:   agentProviderTransport{autoStart: true},
	}
}

func (p *bitwardenProvider) GetHost(host string) (*Record, error) {
	if ref := p.refForScope(scopeHost, host); ref.Ref != "" {
		return p.transportGet(scopeHost, host, ref)
	}
	return nil, nil
}

func (p *bitwardenProvider) GetGroup(group string) (*Record, error) {
	if ref := p.refForScope(scopeGroup, group); ref.Ref != "" {
		return p.transportGet(scopeGroup, group, ref)
	}
	return nil, nil
}

func (p *bitwardenProvider) GetRef(ref config.CredentialRefConfig) (*Record, error) {
	return p.transportGet(scopeHost, "", ref)
}

func (p *bitwardenProvider) transportGet(scope credentialScope, name string, ref config.CredentialRefConfig) (*Record, error) {
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
	if isBitwardenAuthRequired(err) {
		session, unlockErr := unlockBitwardenProvider()
		if unlockErr != nil {
			return nil, unlockErr
		}
		if p.warmSession {
			if _, authErr := transport.ProviderRequest(providerexec.ProviderRequest{
				Provider: p.name,
				Action:   "auth",
				Session:  session,
			}); authErr != nil {
				return nil, authErr
			}
		}
		resp, err = transport.ProviderRequest(providerexec.ProviderRequest{
			Provider:    p.name,
			Action:      "get",
			Scope:       string(scope),
			Name:        name,
			Ref:         ref.Ref,
			Username:    ref.Username,
			UsernameRef: ref.UsernameRef,
			Session:     session,
		})
	}
	if err != nil {
		return nil, err
	}
	if resp == nil || !resp.Found {
		return nil, nil
	}
	return &Record{Username: resp.Username, Secret: secret.NewFromString(string(resp.Secret)), Ref: resp.Ref}, nil
}

func runBitwardenUnlock() (string, error) {
	cmd := exec.Command("bw", "unlock", "--raw")
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("bw unlock --raw: %w", err)
	}
	session := strings.TrimSpace(string(out))
	if session == "" {
		return "", fmt.Errorf("bw unlock --raw returned an empty session")
	}
	return session, nil
}

func isBitwardenAuthRequired(err error) bool {
	return err != nil && strings.Contains(err.Error(), providerexec.ErrBitwardenNotAuthenticated)
}

func (p *bitwardenProvider) refForScope(scope credentialScope, name string) config.CredentialRefConfig {
	switch scope {
	case scopeHost:
		return p.hostRefs[name]
	case scopeGroup:
		return p.groupRefs[name]
	default:
		return config.CredentialRefConfig{}
	}
}

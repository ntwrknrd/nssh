// Package credential provides the user-facing SSH target credential provider
// abstraction used by the cred command and connect-time credential resolution.
package credential

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

// Record is one SSH target credential attached to a host or group.
type Record struct {
	Username string
	Secret   *secret.Secret
	Ref      string
}

// Status describes the active credential provider backend.
type Status struct {
	Type      string
	Available bool
	Detail    string
}

// Provider stores and retrieves host/group SSH target credentials.
type Provider interface {
	GetHost(host string) (*Record, error)
	SetHost(host string, record *Record) error
	RemoveHost(host string) (bool, error)
	GetGroup(group string) (*Record, error)
	SetGroup(group string, record *Record) error
	RemoveGroup(group string) (bool, error)
	Status() Status
}

// NewProvider constructs the single active credential backend selected by
// config. The backend is global; records never select their own provider.
func NewProvider(cfg *config.Config) (Provider, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	credCfg := cfg.Credential
	if err := credCfg.Validate(); err != nil {
		return nil, err
	}

	switch credCfg.Type {
	case config.CredentialProviderAge:
		return newAgeProvider(), nil
	case config.CredentialProvider1Password:
		return newOnePasswordProvider(credCfg), nil
	default:
		return nil, fmt.Errorf("unsupported credential provider %q", credCfg.Type)
	}
}

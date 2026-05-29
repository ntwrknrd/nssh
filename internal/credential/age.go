package credential

import (
	"errors"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/vault"
)

var errWritableSessionRequired = errors.New("credential backend requires an unlocked writable session")

type ageProvider struct {
	mgr *vault.Manager
}

func newAgeProvider() Provider {
	return &ageProvider{}
}

// NewAgeProvider returns an age-backed provider using an already-opened vault
// manager. Tests and CLI code use this to keep unlock/session policy outside
// the provider abstraction.
func NewAgeProvider(mgr *vault.Manager) Provider {
	return &ageProvider{mgr: mgr}
}

func (p *ageProvider) GetHost(host string) (*Record, error) {
	if p.mgr == nil {
		return nil, errWritableSessionRequired
	}
	creds, err := p.mgr.GetHostCredentials(host)
	if err != nil || len(creds) == 0 {
		return nil, err
	}
	cred := creds[0]
	for _, candidate := range creds {
		if candidate.Default {
			cred = candidate
			break
		}
	}
	return toRecord(&cred), nil
}

func (p *ageProvider) SetHost(host string, record *Record) error {
	if p.mgr == nil {
		return errWritableSessionRequired
	}
	if record == nil || record.Secret == nil {
		return errors.New("age credential requires a secret value")
	}
	if updated, err := p.mgr.UpdateHostCredential(host, record.Username, record.Secret); err != nil {
		return err
	} else if updated {
		return p.mgr.SetHostDefaultCredential(host, record.Username)
	}
	if err := p.mgr.AddHostCredential(host, record.Username, record.Secret); err != nil {
		return err
	}
	return p.mgr.SetHostDefaultCredential(host, record.Username)
}

func (p *ageProvider) RemoveHost(host string) (bool, error) {
	if p.mgr == nil {
		return false, errWritableSessionRequired
	}
	return p.mgr.DeleteHostCredentials(host)
}

func (p *ageProvider) GetGroup(group string) (*Record, error) {
	if p.mgr == nil {
		return nil, errWritableSessionRequired
	}
	cred, err := p.mgr.GetGroupCredential(group)
	if err != nil || cred == nil {
		return nil, err
	}
	return toRecord(cred), nil
}

func (p *ageProvider) SetGroup(group string, record *Record) error {
	if p.mgr == nil {
		return errWritableSessionRequired
	}
	if record == nil || record.Secret == nil {
		return errors.New("age credential requires a secret value")
	}
	return p.mgr.SetGroupCredential(group, record.Username, record.Secret)
}

func (p *ageProvider) RemoveGroup(group string) (bool, error) {
	if p.mgr == nil {
		return false, errWritableSessionRequired
	}
	return p.mgr.RemoveGroupCredential(group)
}

func (p *ageProvider) Status() Status {
	return Status{Type: config.CredentialProviderAge, Available: true, Detail: "age-encrypted local credentials"}
}

func toRecord(cred *vault.Credential) *Record {
	if cred == nil {
		return nil
	}
	return &Record{
		Username: cred.Username,
		Secret:   secret.NewFromString(cred.Password),
	}
}

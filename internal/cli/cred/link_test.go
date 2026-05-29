package cred

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestSetCredentialLinkStoresGroupRef(t *testing.T) {
	cfg := &config.Config{}
	scope := credentialScope{Group: "custcbb"}

	err := setCredentialLink(cfg, scope, config.CredentialRefConfig{Ref: "Network Shared Admin", Username: "netops"})
	if err != nil {
		t.Fatalf("setCredentialLink: %v", err)
	}

	got := cfg.Credential.Group["custcbb"]
	if got.Ref != "Network Shared Admin" || got.Username != "netops" {
		t.Fatalf("group ref = %+v", got)
	}
}

func TestSetCredentialLinkStoresHostSecretRefs(t *testing.T) {
	cfg := &config.Config{}
	scope := credentialScope{Host: "edge01"}

	err := setCredentialLink(cfg, scope, config.CredentialRefConfig{
		Ref:         "op://Network/Edge 01/password",
		UsernameRef: "op://Network/Edge 01/username",
	})
	if err != nil {
		t.Fatalf("setCredentialLink: %v", err)
	}

	got := cfg.Credential.Host["edge01"]
	if got.Ref != "op://Network/Edge 01/password" || got.UsernameRef != "op://Network/Edge 01/username" {
		t.Fatalf("host ref = %+v", got)
	}
}

func TestClearCredentialLinkRemovesOnlyTargetScope(t *testing.T) {
	cfg := &config.Config{Credential: config.CredentialConfig{
		Host: map[string]config.CredentialRefConfig{
			"edge01": {Ref: "Edge 01"},
		},
		Group: map[string]config.CredentialRefConfig{
			"custcbb": {Ref: "Network Shared Admin"},
		},
	}}

	if !clearCredentialLink(cfg, credentialScope{Host: "edge01"}) {
		t.Fatal("expected host link removal")
	}
	if _, ok := cfg.Credential.Host["edge01"]; ok {
		t.Fatalf("host ref still exists: %+v", cfg.Credential.Host)
	}
	if cfg.Credential.Group["custcbb"].Ref == "" {
		t.Fatalf("group ref was removed: %+v", cfg.Credential.Group)
	}
}

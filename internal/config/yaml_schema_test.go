package config

import (
	"path/filepath"
	"testing"
)

func TestApprovedYAMLSchemaDecodes(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
include: []
credentials:
  op-expedient:
    type: 1password
    session: agent
    vault: Expedient
inventory:
  providers:
    netbox-prod:
      type: netbox
      config:
        url_env: NETBOX_URL
        token_env: NETBOX_TOKEN
      groups:
        cbb:
          match:
            domain_suffix: [.expedient.com]
          auth:
            mode: password
            username: chris.jones
            credential_provider: op-expedient
            password_ref: op://Expedient/item/password
      hosts:
        701-sw37r103c608.expedient.com:
          group: cbb
          aliases: [701-sw37]
          ssh:
            compat: [legacy-kex, legacy-macs]
            options:
              Ciphers: aes256-ctr
ssh:
  defaults:
    identities_only: true
    identity_agent:
      path: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
    identity_files:
      - ~/.ssh/ed25519-1Password-Personal.pub
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Credentials["op-expedient"].Vault; got != "Expedient" {
		t.Fatalf("vault = %q, want Expedient", got)
	}
	host := cfg.Inventory.Providers["netbox-prod"].Hosts["701-sw37r103c608.expedient.com"]
	if got := host.Group; got != "cbb" {
		t.Fatalf("host group = %q, want cbb", got)
	}
	if got := cfg.SSH.Defaults.IdentityAgent.Path; got == "" {
		t.Fatalf("identity agent path not decoded")
	}
}

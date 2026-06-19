package config

import (
	"path/filepath"
	"strings"
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
          ssh:
            options:
              ProxyJump: bastion
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
            compatibility:
              kex: diffie-hellman-group14-sha1
              mac: hmac-sha1
            options:
              Ciphers:
                - aes256-ctr
ssh:
  defaults:
    options:
      IdentitiesOnly: true
      IdentityAgent: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
      IdentityFile:
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
	if got := cfg.SSH.Defaults.Options["IdentityAgent"].Scalar; got == "" {
		t.Fatalf("identity agent path not decoded")
	}
	if got := cfg.Inventory.Providers["netbox-prod"].Groups["cbb"].SSH.Options["ProxyJump"].Scalar; got != "bastion" {
		t.Fatalf("group ssh proxy_jump = %q, want bastion", got)
	}
}

func TestLoggingExportGIFSchemaDecodes(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
logging:
  export:
    gif:
      window_size: 145x30
      font_size: 18
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Logging.Export.GIF.WindowSize; got != "145x30" {
		t.Fatalf("logging.export.gif.window_size = %q, want 145x30", got)
	}
	if got := cfg.Logging.Export.GIF.FontSize; got != 18 {
		t.Fatalf("logging.export.gif.font_size = %d, want 18", got)
	}
}

func TestLoggingSessionWindowSizeFieldIsRejected(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
logging:
  session:
    window_size: 145x30
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field window_size not found") {
		t.Fatalf("Load error = %v, want window_size unknown field", err)
	}
}

func TestLegacySSHOptionFieldsAreRejected(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
ssh:
  defaults:
    identity_agent: ~/agent.sock
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field identity_agent not found") {
		t.Fatalf("Load error = %v, want identity_agent unknown field", err)
	}
}

func TestInventoryHostnameFieldIsRejected(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
inventory:
  providers:
    local:
      type: local
      groups:
        lab: {}
      hosts:
        edge01:
          group: lab
          hostname: edge01.example.com
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field hostname not found") {
		t.Fatalf("Load error = %v, want hostname unknown field", err)
	}
}

func TestInventoryAuthModeFieldIsRejected(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
inventory:
  providers:
    local:
      type: local
      groups:
        lab:
          auth:
            auth_mode: password
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load succeeded, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field auth_mode not found") {
		t.Fatalf("Load error = %v, want auth_mode unknown field", err)
	}
}

func TestKnownSSHOptionTypesAreValidated(t *testing.T) {
	tests := []struct {
		name    string
		options string
		wantErr string
	}{
		{
			name: "boolean rejects string",
			options: `
      IdentitiesOnly: "yes"
`,
			wantErr: "ssh.defaults.options.IdentitiesOnly must be a boolean",
		},
		{
			name: "setenv rejects scalar",
			options: `
      SetEnv: TERM=xterm-256color
`,
			wantErr: "ssh.defaults.options.SetEnv must be a map",
		},
		{
			name: "identity agent rejects list",
			options: `
      IdentityAgent:
        - ~/agent.sock
`,
			wantErr: "ssh.defaults.options.IdentityAgent must be a string",
		},
		{
			name: "unknown option rejects map",
			options: `
      VendorOption:
        nested: value
`,
			wantErr: "ssh.defaults.options.VendorOption must be a scalar, boolean, or list",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			path := filepath.Join(tmp, "config.yaml")
			writeConfigFile(t, path, "ssh:\n  defaults:\n    options:\n"+tt.options)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load succeeded, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestUnknownSSHOptionScalarBoolAndListAreAccepted(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
ssh:
  defaults:
    options:
      VendorScalar: value
      VendorBool: true
      VendorList:
        - one
        - two
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

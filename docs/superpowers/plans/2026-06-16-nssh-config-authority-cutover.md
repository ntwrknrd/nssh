# nssh Config Authority Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move host lookup, auth policy, routing, compatibility fixes, and OpenSSH argv generation under nssh config authority while cutting over the config format from TOML to YAML.

**Architecture:** `internal/config` becomes the runtime authority for SSH behavior with strict YAML loading, provider-scoped includes, unified provider-level `hosts`, singular group inheritance, and deterministic merge semantics. `internal/connect` resolves hosts from nssh config plus provider state without reading generated or native SSH config, and `internal/ssh/connector` renders a complete OpenSSH argv with `-F none`, typed SSH options, compatibility fixes, and runtime verbosity.

**Tech Stack:** Go 1.25, Cobra, `gopkg.in/yaml.v3`, OpenSSH transport, existing provider state under `internal/inventory`, existing credential providers under `internal/credential`.

---

## Source Inputs

- Approved schema: `docs/nssh-config-yaml-mockup.md`
- Decision log: `docs/nssh-config-decisions.md`
- Current config loader: `internal/config/settings.go`, `internal/config/include.go`, `internal/config/inventory.go`, `internal/config/writer.go`
- Current resolver: `internal/connect/lookup.go`, `internal/connect/resolve.go`, `internal/connect/connect.go`
- Current transport argv builder: `internal/ssh/connector/args_unix.go`
- Current SSH config parser/import material: `internal/ssh/sshconfig/parser.go`, `internal/ssh/sshconfig/include.go`
- Current provider refresh/state: `internal/inventory/refresh.go`, `internal/inventory/state.go`, `internal/inventory/engine.go`

## File Structure

- Modify `go.mod` and `go.sum`: add `gopkg.in/yaml.v3`.
- Modify `internal/config/paths.go`: canonical config file becomes `config.yaml`.
- Modify `internal/config/settings.go`: root struct gains YAML tags and approved `ssh.defaults`.
- Modify `internal/config/inventory.go`: credentials, providers, groups, and provider-level `hosts` match approved YAML.
- Replace `internal/config/include.go`: strict YAML include loader with source tracking.
- Replace or heavily modify `internal/config/writer.go`: write sparse YAML, not TOML.
- Create `internal/config/ssh.go`: typed SSH option structs, merge helpers, compatibility fix config rendering inputs.
- Create `internal/connect/resolver.go`: nssh-native host catalog, singular group selection, merge order.
- Modify `internal/connect/lookup.go`: fuzzy lookup uses nssh host catalog, not `~/.ssh/config`.
- Modify `internal/connect/resolve.go`: `ResolveHostForConnect` consumes nssh-native resolved host data.
- Modify `internal/connect/connect.go`: no SSH config lookup, no generated config rewrite on compat fixes.
- Create `internal/ssh/connector/options.go`: render typed SSH config to OpenSSH argv.
- Modify `internal/ssh/connector/args_unix.go`: always use `-F none` and resolved argv.
- Modify `internal/ssh/compat/compat.go`: approved fix names and exact `-o` translations.
- Modify `internal/inventory/refresh.go`: provider refresh writes provider state only.
- Modify `internal/inventory/state.go`: provider state stores discovered facts only; per-host operator config lives in YAML.
- Modify `internal/cli/inv/*.go`: local inventory reads/writes `inventory.providers.local.hosts`, not SSH Host blocks.
- Create `internal/cli/self/import_ssh_config.go`: minimal SSH config import command.
- Modify `internal/app/command.go`: `-v` ladder controls nssh and OpenSSH verbosity.
- Replace `docs/examples/config/config.example.toml` with `docs/examples/config/config.example.yaml`.
- Modify `docs/examples/config/embed.go`: embed YAML example.
- Modify tests in the touched packages; remove or rewrite TOML-only tests.

## Implementation Rules

- Do not add TOML compatibility. This is a clean `release-0.3` cutover.
- Do not read `~/.ssh/config` at runtime for `nssh connect`, host lookup, auth, routing, or compatibility fixes.
- Do not write generated provider SSH config.
- Do not add raw argv config.
- Preserve OpenSSH as transport by rendering typed config to argv.
- Keep commits small: one task or coherent subtask per commit.

---

### Task 1: Add YAML Dependency And Canonical Config Path

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/config/paths.go`
- Modify: `internal/config/doc.go`
- Modify: `docs/examples/config/embed.go`
- Rename: `docs/examples/config/config.example.toml` -> `docs/examples/config/config.example.yaml`
- Test: `internal/config/paths_test.go`
- Test: `internal/config/embed_test.go`

- [ ] **Step 1: Add the YAML module**

Run:

```bash
go get gopkg.in/yaml.v3@v3.0.1
```

Expected: `go.mod` contains `gopkg.in/yaml.v3 v3.0.1`.

- [ ] **Step 2: Write the failing config path test**

Create `internal/config/paths_test.go` with:

```go
package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPathsUsesConfigYAML(t *testing.T) {
	paths := resolvePaths()
	if !strings.HasSuffix(paths.ConfigFile, filepath.Join("nssh", "config.yaml")) {
		t.Fatalf("ConfigFile = %q, want config.yaml", paths.ConfigFile)
	}
}
```

- [ ] **Step 3: Run the failing path test**

Run:

```bash
go test ./internal/config -run TestDefaultPathsUsesConfigYAML -count=1
```

Expected: FAIL because `ConfigFile` still ends with `config.toml`.

- [ ] **Step 4: Change the default config filename**

In `internal/config/paths.go`, change:

```go
ConfigFile: filepath.Join(configDir, "config.toml"),
```

to:

```go
ConfigFile: filepath.Join(configDir, "config.yaml"),
```

Update `internal/config/doc.go` references from `config.toml` to `config.yaml`.

- [ ] **Step 5: Replace the embedded config example**

Rename the example file:

```bash
mv docs/examples/config/config.example.toml docs/examples/config/config.example.yaml
```

Replace the embed directive in `docs/examples/config/embed.go`:

```go
//go:embed config.example.yaml
var ExampleConfig string
```

Put this minimal approved YAML content in `docs/examples/config/config.example.yaml`:

```yaml
# nssh Configuration File
# Location: ~/.config/nssh/config.yaml

include: [credentials/*.yaml, inventory/*.yaml]

agent:
  auto_start: true
  idle_timeout: 1h
  activity_increment: 15m
  max_lifetime: 24h

credentials:
  pass-local:
    type: pass
    session: external
    command: pass
    prefix: nssh

inventory:
  providers:
    local:
      type: local
      groups:
        default:
          auth:
            mode: password
            credential_provider: pass-local
            password_ref: nssh/groups/default
            username: netops
      hosts: {}

ssh:
  connection:
    timeout: 30s
    password_timeout: 10s
    idle_timeout: 0s
  security:
    host_key_policy: pin
    accept_once_mode: pin
    compat_persist_probes: false

logging:
  audit:
    enabled: true
    max_size: 10MB
  session:
    enabled: false
    append_mode: true
    dir: ~/.local/state/nssh/casts
    auto_export_txt: true
```

- [ ] **Step 6: Update the embed test**

In `internal/config/embed_test.go`, assert the embedded file is `config.example.yaml`:

```go
docsPath := filepath.Join(projectRoot, "docs", "examples", "config", "config.example.yaml")
```

- [ ] **Step 7: Verify task 1**

Run:

```bash
go test ./internal/config -run 'TestDefaultPathsUsesConfigYAML|TestExampleConfigUsesDocsSource' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit task 1**

```bash
git add go.mod go.sum internal/config/paths.go internal/config/doc.go internal/config/paths_test.go internal/config/embed_test.go docs/examples/config/embed.go docs/examples/config/config.example.yaml
git add -u docs/examples/config/config.example.toml
git commit -m "feat(config): make yaml config path canonical"
```

---

### Task 2: Replace TOML Loader With Strict YAML Includes

**Files:**
- Modify: `internal/config/include.go`
- Modify: `internal/config/settings.go`
- Modify: `internal/config/inventory.go`
- Test: `internal/config/include_test.go`
- Test: `internal/config/settings_test.go`

- [ ] **Step 1: Write strict YAML loader tests**

Replace TOML-oriented include tests with YAML tests covering include order, strict unknown keys, and cycle detection:

```go
func TestLoadYAMLIncludesInOrder(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "credentials", "op.yaml"), `
credentials:
  op-expedient:
    type: 1password
    session: agent
    vault: Expedient
`)
	writeConfigFile(t, filepath.Join(tmp, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        homelab:
          auth:
            mode: key
            username: cj
      hosts:
        rpi-a:
          group: homelab
          hostname: rpi-a.lan
`)
	root := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, root, `include: [credentials/*.yaml, inventory/*.yaml]`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.Credentials["op-expedient"]; !ok {
		t.Fatalf("missing credential provider")
	}
	if got := cfg.Inventory.Providers["local"].Hosts["rpi-a"].Group; got != "homelab" {
		t.Fatalf("local host group = %q, want homelab", got)
	}
}

func TestLoadYAMLRejectsUnknownKeys(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, root, `
ssh:
  defaults:
    not_a_real_key: true
`)
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "not_a_real_key") {
		t.Fatalf("Load error = %v, want unknown key error", err)
	}
}

func TestLoadYAMLIncludeCycle(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "a.yaml"), `include: [b.yaml]`)
	writeConfigFile(t, filepath.Join(tmp, "b.yaml"), `include: [a.yaml]`)
	_, err := Load(filepath.Join(tmp, "a.yaml"))
	if err == nil || !strings.Contains(err.Error(), "include cycle") {
		t.Fatalf("Load error = %v, want include cycle", err)
	}
}
```

- [ ] **Step 2: Run strict YAML loader tests to verify failure**

Run:

```bash
go test ./internal/config -run 'TestLoadYAMLIncludesInOrder|TestLoadYAMLRejectsUnknownKeys|TestLoadYAMLIncludeCycle' -count=1
```

Expected: FAIL because loader still uses TOML and old struct fields.

- [ ] **Step 3: Replace TOML map reading with YAML map reading**

In `internal/config/include.go`, replace BurntSushi TOML usage with `gopkg.in/yaml.v3`:

```go
func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var table map[string]any
	if err := yaml.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if table == nil {
		table = make(map[string]any)
	}
	return table, nil
}
```

Rename `readTOMLMap` calls to `readYAMLMap`.

- [ ] **Step 4: Decode the merged YAML document strictly**

In `decodeConfigDocument`, marshal `doc.effective` to YAML and decode with `KnownFields(true)`:

```go
func decodeConfigDocument(path string, doc *configDocument, cfg *Config) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(doc.effective); err != nil {
		return fmt.Errorf("encode merged config %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode merged config %s: %w", path, err)
	}
	dec := yaml.NewDecoder(&buf)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("decode config %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 5: Keep source tracking intact**

Keep `configDocument.sources`, `ConfigFiles`, and `InventoryProviderSource` behavior by retaining source path generation during recursive include resolution. Update helper names from TOML to YAML but keep their output semantics.

- [ ] **Step 6: Verify task 2**

Run:

```bash
go test ./internal/config -run 'TestLoadYAMLIncludesInOrder|TestLoadYAMLRejectsUnknownKeys|TestLoadYAMLIncludeCycle' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit task 2**

```bash
git add internal/config/include.go internal/config/include_test.go internal/config/settings.go internal/config/inventory.go internal/config/settings_test.go
git commit -m "feat(config): load strict yaml with includes"
```

---

### Task 3: Implement Approved YAML Schema Structs

**Files:**

- Modify: `internal/config/settings.go`
- Modify: `internal/config/inventory.go`
- Create: `internal/config/ssh.go`
- Test: `internal/config/yaml_schema_test.go`

- [ ] **Step 1: Write schema decode tests**

Create `internal/config/yaml_schema_test.go` with:

```go
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
```

- [ ] **Step 2: Run schema test to verify failure**

Run:

```bash
go test ./internal/config -run TestApprovedYAMLSchemaDecodes -count=1
```

Expected: FAIL because current structs use TOML-era fields such as `Credential.Provider`, `Inventory.Provider`, and `Inventory.Host`.

- [ ] **Step 3: Update root config struct**

In `internal/config/settings.go`, make the root match YAML:

```go
type Config struct {
	Include     []string                         `yaml:"include,omitempty"`
	Agent       AgentConfig                      `yaml:"agent,omitempty"`
	Credentials map[string]CredentialProvider    `yaml:"credentials,omitempty"`
	Inventory   InventoryConfig                  `yaml:"inventory,omitempty"`
	Logging     LoggingConfig                    `yaml:"logging,omitempty"`
	SSH         SSHConfig                        `yaml:"ssh,omitempty"`

	document *configDocument
}
```

Keep compatibility methods inside Go code by adding helper methods where needed instead of preserving TOML fields.

- [ ] **Step 4: Replace credential structs**

In `internal/config/inventory.go`, replace `CredentialConfig` and `CredentialProviderConfig` with:

```go
type CredentialProvider struct {
	Type    string `yaml:"type"`
	Session string `yaml:"session,omitempty"`
	Account string `yaml:"account,omitempty"`
	Vault   string `yaml:"vault,omitempty"`
	Command string `yaml:"command,omitempty"`
	Prefix  string `yaml:"prefix,omitempty"`
}
```

Validation should enforce:

```go
case CredentialProviderPass:
	command defaults to "pass"
	prefix defaults to "nssh"
	session defaults to external
case CredentialProvider1Password:
	vault is required
	session defaults to agent
case CredentialProviderBitwarden:
	session defaults to external
```

- [ ] **Step 5: Replace inventory structs**

Use the approved names:

```go
type InventoryConfig struct {
	Providers map[string]InventoryProviderConfig `yaml:"providers,omitempty"`
}

type InventoryProviderConfig struct {
	Type    string                         `yaml:"type"`
	Auth    InventoryAuthConfig            `yaml:"auth,omitempty"`
	Config  InventoryProviderDetailConfig  `yaml:"config,omitempty"`
	Groups  map[string]GroupConfig         `yaml:"groups,omitempty"`
	Hosts   map[string]InventoryHostConfig `yaml:"hosts,omitempty"`
}

type InventoryHostConfig struct {
	Group        string              `yaml:"group,omitempty"`
	Hostname     string              `yaml:"hostname,omitempty"`
	Aliases      []string            `yaml:"aliases,omitempty"`
	Port         int                 `yaml:"port,omitempty"`
	Auth         InventoryAuthConfig `yaml:"auth,omitempty"`
	SSH          SSHHostConfig       `yaml:"ssh,omitempty"`
	AuthDisabled bool                `yaml:"auth_disabled,omitempty"`
}

type InventoryAuthConfig struct {
	Mode               string `yaml:"mode,omitempty"`
	CredentialProvider string `yaml:"credential_provider,omitempty"`
	PasswordRef        string `yaml:"password_ref,omitempty"`
	Username           string `yaml:"username,omitempty"`
	UsernameRef        string `yaml:"username_ref,omitempty"`
}
```

Update all internal references from `Provider` to `Providers`, `Group` to `Groups`, and host overrides from `Inventory.Host` to `Inventory.Providers[provider].Hosts`.

- [ ] **Step 6: Add typed SSH structs**

Create `internal/config/ssh.go`:

```go
package config

type SSHConfig struct {
	Connection SSHConnectionConfig `yaml:"connection,omitempty"`
	Security   SSHSecurityConfig   `yaml:"security,omitempty"`
	Defaults   SSHHostConfig       `yaml:"defaults,omitempty"`
}

type SSHHostConfig struct {
	ProxyJump                 string            `yaml:"proxy_jump,omitempty"`
	ProxyCommand              string            `yaml:"proxy_command,omitempty"`
	IdentitiesOnly            *bool             `yaml:"identities_only,omitempty"`
	IdentityAgent             IdentityAgent     `yaml:"identity_agent,omitempty"`
	IdentityFiles             []string          `yaml:"identity_files,omitempty"`
	CertificateFiles          []string          `yaml:"certificate_files,omitempty"`
	ForwardAgent              *bool             `yaml:"forward_agent,omitempty"`
	LocalForwards             []Forward         `yaml:"local_forwards,omitempty"`
	RemoteForwards            []Forward         `yaml:"remote_forwards,omitempty"`
	SetEnv                    map[string]string `yaml:"set_env,omitempty"`
	RemoteCommand             string            `yaml:"remote_command,omitempty"`
	ServerAliveInterval        Duration          `yaml:"server_alive_interval,omitempty"`
	ServerAliveCountMax        int               `yaml:"server_alive_count_max,omitempty"`
	ConnectionTimeout         Duration          `yaml:"connection_timeout,omitempty"`
	ControlMaster             string            `yaml:"control_master,omitempty"`
	ControlPersist            Duration          `yaml:"control_persist,omitempty"`
	ControlPath               string            `yaml:"control_path,omitempty"`
	Ciphers                   []string          `yaml:"ciphers,omitempty"`
	MACs                      []string          `yaml:"macs,omitempty"`
	KexAlgorithms             []string          `yaml:"kex_algorithms,omitempty"`
	HostKeyAlgorithms         []string          `yaml:"host_key_algorithms,omitempty"`
	PubkeyAcceptedAlgorithms  []string          `yaml:"pubkey_accepted_algorithms,omitempty"`
	Compat                    []string          `yaml:"compat,omitempty"`
	Options                   map[string]string `yaml:"options,omitempty"`
}

type IdentityAgent struct {
	Path string `yaml:"path,omitempty"`
}

type Forward struct {
	Bind   string `yaml:"bind"`
	Target string `yaml:"target"`
}
```

- [ ] **Step 7: Update validation**

Validation must enforce:

```go
local provider: each hosts.<name>.group is required
discovered provider: hosts.<name>.group is optional but must reference an existing group when set
all providers: host group references must be singular and valid
compat values: legacy-kex, legacy-macs, legacy-hostkey, legacy-pubkey only
ssh.options: non-empty keys and values only
```

- [ ] **Step 8: Verify task 3**

Run:

```bash
go test ./internal/config -run TestApprovedYAMLSchemaDecodes -count=1
go test ./internal/config -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit task 3**

```bash
git add internal/config/settings.go internal/config/inventory.go internal/config/ssh.go internal/config/yaml_schema_test.go
git commit -m "feat(config): model approved yaml schema"
```

---

### Task 4: Replace Config Writing With Sparse YAML

**Files:**
- Modify: `internal/config/writer.go`
- Modify: `internal/cli/self/init.go`
- Modify: `internal/cli/self/init_plan.go`
- Modify: `internal/cli/self/cfg.go`
- Test: `internal/config/root_save_test.go`
- Test: `internal/cli/self/init_provider_test.go`

- [ ] **Step 1: Write sparse YAML writer test**

In `internal/config/root_save_test.go`, add:

```go
func TestSaveSparseWritesYAMLIncludesAndProviderHosts(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	cfg := DefaultConfig()
	cfg.Include = []string{"credentials/*.yaml", "inventory/*.yaml"}
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		ProviderLocal: {
			Type: ProviderLocal,
			Groups: map[string]GroupConfig{
				"homelab": {Auth: InventoryAuthConfig{Mode: AuthModeKey, Username: "cj"}},
			},
			Hosts: map[string]InventoryHostConfig{
				"rpi-a": {Group: "homelab", Hostname: "rpi-a.lan"},
			},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"include:",
		"providers:",
		"local:",
		"hosts:",
		"rpi-a:",
		"group: homelab",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config missing %q:\n%s", want, text)
		}
	}
}
```

- [ ] **Step 2: Run writer test to verify failure**

Run:

```bash
go test ./internal/config -run TestSaveSparseWritesYAMLIncludesAndProviderHosts -count=1
```

Expected: FAIL because writer emits TOML.

- [ ] **Step 3: Replace sparse TOML writer with YAML writer**

In `internal/config/writer.go`, keep `Save(path, cfg)` and `SaveSparse(path, cfg)` public signatures, but implement them through `yaml.Encoder`:

```go
func MarshalSparse(cfg *Config) (string, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(sparseConfig(cfg)); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}
```

Use a typed sparse struct or `map[string]any`; do not keep TOML table emitters.

- [ ] **Step 4: Update init and config-edit paths**

Update:

```go
paths.ConfigFile
config.Save(paths.ConfigFile, plan.Config)
config.ExampleConfig
```

Callers should keep using `config.Save`; they should not know about YAML internals.

- [ ] **Step 5: Verify task 4**

Run:

```bash
go test ./internal/config -run 'TestSaveSparseWritesYAMLIncludesAndProviderHosts|TestLoadYAMLIncludesInOrder' -count=1
go test ./internal/cli/self -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit task 4**

```bash
git add internal/config/writer.go internal/config/root_save_test.go internal/cli/self/init.go internal/cli/self/init_plan.go internal/cli/self/cfg.go internal/cli/self/init_provider_test.go
git commit -m "feat(config): write sparse yaml config"
```

---

### Task 5: Stop Writing Generated SSH Config During Provider Refresh

**Files:**

- Modify: `internal/inventory/refresh.go`
- Modify: `internal/inventory/state.go`
- Modify: `internal/inventory/engine.go`
- Modify: `internal/inventory/sshwriter.go`
- Modify: `internal/cli/inv/refresh.go`
- Test: `internal/inventory/refresh_test.go`
- Test: `internal/inventory/state_test.go`

- [ ] **Step 1: Write refresh side-effect test**

In `internal/inventory/refresh_test.go`, assert provider refresh saves state but does not write SSH config:

```go
func TestRefreshProviderDoesNotWriteSSHConfig(t *testing.T) {
	tmp := t.TempDir()
	SetStateDir(tmp)
	t.Cleanup(func() { SetStateDir("") })

	provider := fakeProvider{objects: []Object{{
		Provider: "netbox-prod",
		ObjectID: "1",
		Name:     "edge01.example.com",
		HostName: "edge01.example.com",
	}}}
	cfg := config.InventoryProviderConfig{
		Type: config.ProviderNetBox,
		Groups: map[string]config.GroupConfig{
			"cbb": {Match: config.InventoryMatch{"domain_suffix": {".example.com"}}},
		},
	}
	result := RefreshProvider(context.Background(), "netbox-prod", cfg, provider, nil, RefreshOptions{Now: time.Unix(1, 0).UTC()})
	if result.Err != nil {
		t.Fatalf("RefreshProvider: %v", result.Err)
	}
	state, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("LoadProviderState: %v", err)
	}
	if state == nil || len(state.Objects) != 1 {
		t.Fatalf("state objects = %v, want one object", state)
	}
}
```

- [ ] **Step 2: Run refresh test to verify failure or old behavior**

Run:

```bash
go test ./internal/inventory -run TestRefreshProviderDoesNotWriteSSHConfig -count=1
```

Expected: FAIL until `RefreshOptions.WriteSSHConfig` and SSH config writes are removed.

- [ ] **Step 3: Remove `WriteSSHConfig` from refresh options**

In `internal/inventory/refresh.go`, change:

```go
type RefreshOptions struct {
	Now time.Time
}
```

Remove calls to `WriteProviderSSHConfig` and `RemoveProviderSSHConfig`.

- [ ] **Step 4: Make provider state facts-only**

In `internal/inventory/state.go`, remove generated-config authority fields from new writes:

```go
IncludeFile string
CompatFixes []compat.CompatType
```

If removing `IncludeFile` immediately breaks CLI display, leave it as deprecated read-only state for one task and stop setting it in new refreshes. The final cleanup task removes it after resolver no longer needs it.

- [ ] **Step 5: Update refresh command**

In `internal/cli/inv/refresh.go`, remove `sshconfig.NewParser()` and `newConfigOnlyRunner(parser)` from refresh unless a provider needs remote command execution. Use a runner only for Containerlab, and build that runner through nssh resolver in a later task.

- [ ] **Step 6: Verify task 5**

Run:

```bash
go test ./internal/inventory -run 'TestRefreshProviderDoesNotWriteSSHConfig|TestReconcile' -count=1
go test ./internal/cli/inv -run TestRefresh -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit task 5**

```bash
git add internal/inventory/refresh.go internal/inventory/state.go internal/inventory/engine.go internal/inventory/sshwriter.go internal/inventory/refresh_test.go internal/inventory/state_test.go internal/cli/inv/refresh.go
git commit -m "feat(inventory): stop writing generated ssh config"
```

---

### Task 6: Build Native Host Catalog And Resolver

**Files:**
- Create: `internal/connect/catalog.go`
- Create: `internal/connect/catalog_test.go`
- Create: `internal/connect/resolver.go`
- Test: `internal/connect/resolve_test.go`
- Modify: `internal/connect/lookup.go`
- Modify: `internal/connect/resolve.go`

- [ ] **Step 1: Write catalog tests**

Create `internal/connect/catalog_test.go`:

```go
package connect

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

func TestCatalogUsesProviderHostsAsOverlays(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type: config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{
				"cbb": {Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "chris.jones"}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {Group: "cbb", Aliases: []string{"edge01"}},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "netbox-prod",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"1": {ObjectID: "1", Host: "edge01.example.com", HostName: "edge01.example.com", Group: "netbox-prod/cbb"},
		},
	}
	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	if host.Provider != "netbox-prod" || host.Group != "cbb" || host.Username != "chris.jones" {
		t.Fatalf("host = %#v", host)
	}
}

func TestCatalogRequiresLocalHostGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a": {Hostname: "rpi-a.lan"},
			},
		},
	}
	_, err := BuildHostCatalog(cfg)
	if err == nil {
		t.Fatalf("BuildHostCatalog succeeded without local host group")
	}
}
```

- [ ] **Step 2: Run catalog tests to verify failure**

Run:

```bash
go test ./internal/connect -run 'TestCatalogUsesProviderHostsAsOverlays|TestCatalogRequiresLocalHostGroup' -count=1
```

Expected: FAIL because host catalog does not exist.

- [ ] **Step 3: Add catalog types**

Create `internal/connect/catalog.go` with:

```go
type HostCatalog struct {
	hosts   map[string]*ResolvedHostData
	aliases map[string]string
}

type ResolvedHostData struct {
	Query      string
	Canonical string
	Hostname  string
	Aliases   []string
	Provider  string
	Group     string
	Port      int
	Username  string
	Auth      config.InventoryAuthConfig
	SSH       config.SSHHostConfig
}
```

`BuildHostCatalog(cfg *config.Config)` should load provider states and call an internal builder that tests can inject with states.

- [ ] **Step 4: Implement merge order**

Catalog resolution must apply:

```text
global ssh.defaults
-> provider defaults
-> discovered provider facts or local host identity
-> selected group defaults
-> provider.hosts.<host> per-host config
-> CLI flags later in connector/app layer
```

For local hosts, identity fields from `hosts.<name>` are applied before group defaults, but `hosts.<name>.Auth` and `hosts.<name>.SSH` override group defaults.

- [ ] **Step 5: Implement singular group selection**

Rules:

```text
discovered host with hosts.<canonical>.group -> use explicit group
discovered host without explicit group -> use provider state group
multiple implicit matches during refresh -> warning/debug event from inventory engine
local host -> hosts.<name>.group is required
```

Provider state currently stores provider-qualified groups such as `netbox-prod/cbb`; normalize to short group `cbb` inside the catalog.

- [ ] **Step 6: Rewrite `ResolveHostname`**

In `internal/connect/lookup.go`, replace `sshconfig.NewParser().MatchHost` with:

```go
cfg, err := config.LoadDefault()
if err != nil {
	return "", err
}
catalog, err := BuildHostCatalog(cfg)
if err != nil {
	return "", err
}
return catalog.ResolveQuery(hostname)
```

Keep fuzzy selection over catalog-managed hosts only.

- [ ] **Step 7: Rewrite `ResolveHostForConnect`**

In `internal/connect/resolve.go`, remove `sshconfig.NewParser()` and `HostEntry`. Populate `ResolvedHost` from `ResolvedHostData`, including final auth and SSH config.

Extend `ResolvedHost`:

```go
type ResolvedHost struct {
	Query      string
	Hostname   string
	Port       int
	Username   string
	AuthMode   string
	Provider   string
	Group      string
	Aliases    []string
	SSH        config.SSHHostConfig
	Credential *ResolvedCredential
	Config     *config.Config
}
```

- [ ] **Step 8: Verify task 6**

Run:

```bash
go test ./internal/connect -run 'TestCatalog|TestResolve|TestResolveHostname' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit task 6**

```bash
git add internal/connect/catalog.go internal/connect/catalog_test.go internal/connect/resolver.go internal/connect/lookup.go internal/connect/resolve.go internal/connect/resolve_test.go internal/connect/lookup_test.go
git commit -m "feat(connect): resolve hosts from nssh catalog"
```

---

### Task 7: Render Complete OpenSSH Args From Resolved Config

**Files:**

- Create: `internal/ssh/connector/options.go`
- Test: `internal/ssh/connector/options_test.go`
- Modify: `internal/ssh/connector/connector.go`
- Modify: `internal/ssh/connector/args_unix.go`
- Modify: `internal/connect/connect.go`
- Test: `internal/ssh/connector/args_test.go`

- [ ] **Step 1: Write argv rendering tests**

Create `internal/ssh/connector/options_test.go`:

```go
package connector

import (
	"slices"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestRenderSSHOptionsIncludesFNoneAndIdentityAgent(t *testing.T) {
	v := true
	opts := config.SSHHostConfig{
		IdentitiesOnly: &v,
		IdentityAgent:  config.IdentityAgent{Path: "~/agent.sock"},
		IdentityFiles:  []string{"~/.ssh/id_ed25519.pub"},
		Options:        map[string]string{"Compression": "yes"},
	}
	args := RenderSSHOptions(opts, 0)
	for _, want := range []string{"-F", "none", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=~/agent.sock", "-i", "~/.ssh/id_ed25519.pub", "-o", "Compression=yes"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
}

func TestRenderSSHOptionsAddsVerbosity(t *testing.T) {
	args := RenderSSHOptions(config.SSHHostConfig{}, 3)
	if !slices.Contains(args, "-vvv") {
		t.Fatalf("args = %#v, want -vvv", args)
	}
}
```

- [ ] **Step 2: Run argv tests to verify failure**

Run:

```bash
go test ./internal/ssh/connector -run 'TestRenderSSHOptionsIncludesFNoneAndIdentityAgent|TestRenderSSHOptionsAddsVerbosity' -count=1
```

Expected: FAIL because renderer does not exist.

- [ ] **Step 3: Add typed SSH option renderer**

Create `internal/ssh/connector/options.go`:

```go
func RenderSSHOptions(opts config.SSHHostConfig, sshVerbose int) []string {
	args := []string{"-F", "none"}
	if sshVerbose > 3 {
		sshVerbose = 3
	}
	if sshVerbose > 0 {
		args = append(args, "-"+strings.Repeat("v", sshVerbose))
	}
	appendO := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, "-o", key+"="+value)
		}
	}
	if opts.IdentitiesOnly != nil {
		appendO("IdentitiesOnly", yesNo(*opts.IdentitiesOnly))
	}
	appendO("IdentityAgent", opts.IdentityAgent.Path)
	for _, file := range opts.IdentityFiles {
		args = append(args, "-i", file)
	}
	for _, file := range opts.CertificateFiles {
		appendO("CertificateFile", file)
	}
	return appendRemainingTypedOptions(args, opts, appendO)
}
```

`appendRemainingTypedOptions` should render proxy, forwards, set env, remote command, keepalive, control options, algorithms, compat fixes, and sorted `ssh.options`.

- [ ] **Step 4: Update connector to accept resolved SSH config**

In `internal/ssh/connector/connector.go`, add:

```go
sshOptions config.SSHHostConfig
sshVerbosity int
```

Add setters:

```go
func (c *Connector) SetSSHOptions(opts config.SSHHostConfig) { c.sshOptions = opts }
func (c *Connector) SetSSHVerbosity(level int) { c.sshVerbosity = level }
```

- [ ] **Step 5: Update `buildSSHArgs`**

In `internal/ssh/connector/args_unix.go`, start args with:

```go
args := []string{"-tt"}
args = append(args, RenderSSHOptions(c.sshOptions, c.sshVerbosity)...)
```

Keep user passthrough `sshArgs`, but reject `-F` in passthrough for `nssh connect` unless it is part of a remote command after `--`.

- [ ] **Step 6: Wire resolved options from connect**

In `internal/connect/connect.go`, set:

```go
conn.SetSSHOptions(resolved.SSH)
conn.SetResolvedEndpoint(resolved.Hostname, strconv.Itoa(resolved.Port))
```

The target should still be user-facing canonical name or alias for display, but OpenSSH receives `HostName` through `-o HostName=<resolved.Hostname>` or direct target according to renderer design. Use direct target when no alias matching is needed because `-F none` disables Host aliases.

- [ ] **Step 7: Verify task 7**

Run:

```bash
go test ./internal/ssh/connector -run 'TestRenderSSHOptions|TestBuildSSHArgs' -count=1
go test ./internal/connect -run TestResolveHostForConnect -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit task 7**

```bash
git add internal/ssh/connector/options.go internal/ssh/connector/options_test.go internal/ssh/connector/connector.go internal/ssh/connector/args_unix.go internal/ssh/connector/args_test.go internal/connect/connect.go
git commit -m "feat(ssh): render openssh argv from resolved config"
```

---

### Task 8: Implement Approved Compatibility Fix Catalog

**Files:**
- Modify: `internal/ssh/compat/compat.go`
- Modify: `internal/connect/connect.go`
- Test: `internal/ssh/compat/compat_test.go`
- Test: `internal/connect/connect_test.go`

- [ ] **Step 1: Write compatibility catalog test**

In `internal/ssh/compat/compat_test.go`, add:

```go
func TestApprovedCompatCatalog(t *testing.T) {
	want := map[CompatType]string{
		"legacy-kex":     "KexAlgorithms=+diffie-hellman-group14-sha1,+diffie-hellman-group1-sha1",
		"legacy-macs":    "MACs=+hmac-sha1,+hmac-sha1-96",
		"legacy-hostkey": "HostKeyAlgorithms=+ssh-rsa",
		"legacy-pubkey":  "PubkeyAcceptedAlgorithms=+ssh-rsa",
	}
	for ct, option := range want {
		cfg, ok := CompatConfigs[ct]
		if !ok {
			t.Fatalf("missing compat %s", ct)
		}
		if got := cfg.Option; got != option {
			t.Fatalf("%s option = %q, want %q", ct, got, option)
		}
	}
	if _, ok := CompatConfigs["legacy"]; ok {
		t.Fatalf("broad legacy preset must not exist")
	}
}
```

- [ ] **Step 2: Run compatibility test to verify failure**

Run:

```bash
go test ./internal/ssh/compat -run TestApprovedCompatCatalog -count=1
```

Expected: FAIL because current catalog uses `kex`, `macs`, `ciphers`, `hostkey`.

- [ ] **Step 3: Replace compat constants**

In `internal/ssh/compat/compat.go`, use:

```go
const (
	CompatLegacyKex     CompatType = "legacy-kex"
	CompatLegacyMACs    CompatType = "legacy-macs"
	CompatLegacyHostKey CompatType = "legacy-hostkey"
	CompatLegacyPubkey  CompatType = "legacy-pubkey"
)
```

`CompatConfig` should expose `Option string` for argv rendering instead of SSH config lines.

- [ ] **Step 4: Update auto-fix persistence**

In `internal/connect/connect.go`, replace generated SSH config mutation with a config write that appends new compat names to:

```text
inventory.providers.<provider>.hosts.<canonical>.ssh.compat
```

For local hosts, write to:

```text
inventory.providers.local.hosts.<name>.ssh.compat
```

Use the config source file for the provider when available; otherwise write to the root config file.

- [ ] **Step 5: Verify task 8**

Run:

```bash
go test ./internal/ssh/compat -count=1
go test ./internal/connect -run Compat -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit task 8**

```bash
git add internal/ssh/compat/compat.go internal/ssh/compat/compat_test.go internal/connect/connect.go internal/connect/connect_test.go
git commit -m "feat(ssh): use named yaml compatibility fixes"
```

---

### Task 9: Implement Runtime Verbosity Ladder

**Files:**

- Modify: `internal/app/command.go`
- Modify: `internal/app/app.go`
- Modify: `internal/connect/connect.go`
- Test: `internal/app/app_test.go`
- Test: `internal/app/boundary_test.go`

- [ ] **Step 1: Write verbosity parsing test**

In `internal/app/boundary_test.go`, add:

```go
func TestPreprocessVerbosityLadder(t *testing.T) {
	got := PreprocessArgs([]string{"-vvv", "edge01"})
	want := []string{"-vvv", "smart-connect", "edge01"}
	if !slices.Equal(got, want) {
		t.Fatalf("PreprocessArgs = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run verbosity test to verify failure**

Run:

```bash
go test ./internal/app -run TestPreprocessVerbosityLadder -count=1
```

Expected: FAIL because only `-v` is treated as a global flag.

- [ ] **Step 3: Count repeated verbose flags**

Replace global `verbose bool` with:

```go
verboseCount int
```

Register Cobra flag:

```go
rootCmd.PersistentFlags().CountVarP(&verboseCount, "verbose", "v", "Increase debug verbosity")
```

`initLogging(verboseCount > 0)` keeps nssh debug at `-v` and above.

- [ ] **Step 4: Preserve `-vv` through preprocessing**

Update `PreprocessArgs` so any arg matching `^-v+$` is a global flag. `-V` remains version.

- [ ] **Step 5: Pass SSH verbosity into connect**

Derive:

```go
sshVerbosity := verboseCount - 1
if sshVerbosity < 0 {
	sshVerbosity = 0
}
if sshVerbosity > 3 {
	sshVerbosity = 3
}
```

Thread this into `connect.ConnectHost` through a small options struct:

```go
type Options struct {
	SSHVerbosity int
}
```

- [ ] **Step 6: Verify task 9**

Run:

```bash
go test ./internal/app -run Verbosity -count=1
go test ./internal/ssh/connector -run TestRenderSSHOptionsAddsVerbosity -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit task 9**

```bash
git add internal/app/command.go internal/app/app.go internal/app/app_test.go internal/app/boundary_test.go internal/connect/connect.go
git commit -m "feat(cli): add nssh and ssh verbosity ladder"
```

---

### Task 10: Add Minimal SSH Config Import Command

**Files:**
- Create: `internal/cli/self/import_ssh_config.go`
- Create: `internal/cli/self/import_ssh_config_test.go`
- Modify: `internal/app/command.go`
- Modify: `internal/ssh/sshconfig/parser.go`
- Test: `internal/ssh/sshconfig/parser_test.go`

- [ ] **Step 1: Write import mapping test**

Create `internal/cli/self/import_ssh_config_test.go`:

```go
package self

import (
	"strings"
	"testing"
)

func TestImportSSHConfigMapsApprovedDirectives(t *testing.T) {
	input := `
Host *
  IdentityAgent ~/agent.sock
  ServerAliveInterval 240

Host edge01
  HostName edge01.example.com
  User netops
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  CertificateFile ~/.ssh/id_ed25519-cert.pub
  ProxyJump bastion
  ForwardAgent no
  LocalForward 127.0.0.1:15432 db:5432
`
	out, warnings, err := importSSHConfigText("local", strings.NewReader(input))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	for _, want := range []string{
		"identity_agent:",
		"path: ~/agent.sock",
		"edge01:",
		"group: imported",
		"hostname: edge01.example.com",
		"username: netops",
		"port: 2222",
		"proxy_jump: bastion",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("import output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run import test to verify failure**

Run:

```bash
go test ./internal/cli/self -run TestImportSSHConfigMapsApprovedDirectives -count=1
```

Expected: FAIL because import command does not exist.

- [ ] **Step 3: Implement importer function**

Create `internal/cli/self/import_ssh_config.go` with:

```go
func importSSHConfigText(provider string, r io.Reader) (string, []string, error)
```

Mapping rules:

```text
HostName -> hostname
User -> auth.username
Port -> port
ProxyJump -> ssh.proxy_jump
ProxyCommand -> ssh.proxy_command
IdentityAgent -> ssh.identity_agent.path
IdentityFile -> ssh.identity_files
CertificateFile -> ssh.certificate_files
IdentitiesOnly -> ssh.identities_only
ForwardAgent -> ssh.forward_agent
LocalForward -> ssh.local_forwards
RemoteForward -> ssh.remote_forwards
SetEnv -> ssh.set_env
RemoteCommand -> ssh.remote_command
ServerAliveInterval -> ssh.server_alive_interval
ServerAliveCountMax -> ssh.server_alive_count_max
ConnectTimeout -> ssh.connection_timeout
ControlMaster -> ssh.control_master
ControlPersist -> ssh.control_persist
ControlPath -> ssh.control_path
Ciphers -> ssh.ciphers
MACs -> ssh.macs
KexAlgorithms -> ssh.kex_algorithms
HostKeyAlgorithms -> ssh.host_key_algorithms
PubkeyAcceptedAlgorithms -> ssh.pubkey_accepted_algorithms
```

Simple unknown `Key Value` directives become `ssh.options.Key: Value`.

Warnings only, no import:

```text
Match
Include
CanonicalizeHostname
shell-expanded conditionals
order-sensitive behavior
```

- [ ] **Step 4: Add Cobra command**

Add `nssh self import ssh-config` with flags:

```text
--source PATH      default ~/.ssh/config
--out PATH         default ~/.config/nssh/inventory/imported-ssh.yaml
--provider NAME    default local
--dry-run          print YAML instead of writing
```

Default write path is under `inventory/` so root `include: [inventory/*.yaml]` picks it up.

- [ ] **Step 5: Verify task 10**

Run:

```bash
go test ./internal/cli/self -run ImportSSHConfig -count=1
go test ./internal/ssh/sshconfig -run Parser -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit task 10**

```bash
git add internal/cli/self/import_ssh_config.go internal/cli/self/import_ssh_config_test.go internal/app/command.go internal/ssh/sshconfig/parser.go internal/ssh/sshconfig/parser_test.go
git commit -m "feat(self): import openssh config to yaml"
```

---

### Task 11: Convert Local Inventory Commands To YAML

**Files:**

- Modify: `internal/cli/inv/local.go`
- Modify: `internal/cli/inv/set.go`
- Modify: `internal/cli/inv/remove.go`
- Modify: `internal/cli/inv/list.go`
- Modify: `internal/cli/inv/get.go`
- Modify: `internal/cli/inv/local_refresh.go`
- Test: `internal/cli/inv/local_test.go`
- Test: `internal/cli/inv/list_test.go`
- Test: `internal/cli/inv/remove_test.go`

- [ ] **Step 1: Write local inventory write test**

In `internal/cli/inv/local_test.go`, add:

```go
func TestUpsertLocalHostWritesYAMLProviderHost(t *testing.T) {
	tmp := t.TempDir()
	paths := &config.Paths{ConfigFile: filepath.Join(tmp, "config.yaml"), ConfigDir: tmp}
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"homelab": {Auth: config.InventoryAuthConfig{Mode: config.AuthModeKey, Username: "cj"}},
			},
		},
	}
	err := upsertLocalHostYAML(cfg, paths, hostPatch{Host: "rpi-a", HostName: "rpi-a.lan", Group: "homelab"})
	if err != nil {
		t.Fatalf("upsertLocalHostYAML: %v", err)
	}
	host := cfg.Inventory.Providers[config.ProviderLocal].Hosts["rpi-a"]
	if host.Group != "homelab" || host.Hostname != "rpi-a.lan" {
		t.Fatalf("host = %#v", host)
	}
}
```

- [ ] **Step 2: Run local inventory test to verify failure**

Run:

```bash
go test ./internal/cli/inv -run TestUpsertLocalHostWritesYAMLProviderHost -count=1
```

Expected: FAIL because local host upsert still writes SSH Host blocks.

- [ ] **Step 3: Replace local SSH Host mutation with YAML mutation**

Add helper:

```go
func upsertLocalHostYAML(cfg *config.Config, paths *config.Paths, patch hostPatch) error {
	provider := cfg.Inventory.Providers[config.ProviderLocal]
	if provider.Hosts == nil {
		provider.Hosts = make(map[string]config.InventoryHostConfig)
	}
	host := provider.Hosts[patch.Host]
	host.Group = patch.Group
	host.Hostname = patch.HostName
	host.Port = patch.Port
	host.Auth = patch.Auth
	host.AuthDisabled = patch.AuthDisabled
	host.SSH.Compat = compatTypesToStrings(patch.CompatFixes)
	provider.Hosts[patch.Host] = host
	cfg.Inventory.Providers[config.ProviderLocal] = provider
	return config.Save(paths.ConfigFile, cfg)
}
```

- [ ] **Step 4: Update list/get/remove/local-refresh**

Replace `sshconfig.NewParser()` host reads with the host catalog from Task 6. `inv list` should show local YAML hosts and discovered provider hosts through the same catalog.

- [ ] **Step 5: Verify task 11**

Run:

```bash
go test ./internal/cli/inv -run 'Local|List|Get|Remove' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit task 11**

```bash
git add internal/cli/inv/local.go internal/cli/inv/set.go internal/cli/inv/remove.go internal/cli/inv/list.go internal/cli/inv/get.go internal/cli/inv/local_refresh.go internal/cli/inv/local_test.go internal/cli/inv/list_test.go internal/cli/inv/remove_test.go
git commit -m "feat(inv): manage local inventory in yaml"
```

---

### Task 12: Remove Runtime SSH Config Dependency

**Files:**
- Modify: `internal/connect/connect.go`
- Modify: `internal/connect/lookup.go`
- Modify: `internal/connect/resolve.go`
- Modify: `internal/cli/self/init.go`
- Modify: `internal/ssh/sshconfig/*`
- Test: `internal/connect/connect_test.go`
- Test: `internal/cli/self/init_provider_test.go`

- [ ] **Step 1: Write no-runtime-ssh-config test**

In `internal/connect/resolve_test.go`, add:

```go
func TestResolveDoesNotReadSSHConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg := config.DefaultConfig()
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"homelab": {Auth: config.InventoryAuthConfig{Mode: config.AuthModeKey, Username: "cj"}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a": {Group: "homelab", Hostname: "rpi-a.lan"},
			},
		},
	}
	resolved, err := ResolveHostForConnect("rpi-a", "", cfg)
	if err != nil {
		t.Fatalf("ResolveHostForConnect: %v", err)
	}
	if resolved.Hostname != "rpi-a.lan" {
		t.Fatalf("hostname = %q, want rpi-a.lan", resolved.Hostname)
	}
}
```

- [ ] **Step 2: Run no-runtime-ssh-config test**

Run:

```bash
go test ./internal/connect -run TestResolveDoesNotReadSSHConfig -count=1
```

Expected: PASS after Tasks 6 and 7.

- [ ] **Step 3: Remove init-time SSH include setup**

In `internal/cli/self/init.go`, remove `ensureSSHConfigInclude(paths)` from `runInit`. The app no longer requires `~/.ssh/config Include ~/.ssh/nssh.d/*`.

- [ ] **Step 4: Retain SSH config parser only for import**

Keep `internal/ssh/sshconfig` package for `nssh self import ssh-config` and tests. Remove runtime call sites from `internal/connect` and provider refresh.

- [ ] **Step 5: Verify task 12**

Run:

```bash
rg -n 'NewParser\\(|FindHost\\(|MatchHost\\(|WriteProviderSSHConfig|ensureSSHConfigInclude' internal/connect internal/inventory internal/cli/self internal/cli/inv
```

Expected: matches only in import-specific code or tests that explicitly cover import.

- [ ] **Step 6: Commit task 12**

```bash
git add internal/connect internal/inventory internal/cli/self internal/cli/inv internal/ssh/sshconfig
git commit -m "refactor: remove runtime ssh config dependency"
```

---

### Task 13: Update Documentation And User-Facing Text

**Files:**

- Modify: `README.md`
- Modify: `docs/examples/config/config.example.yaml`
- Modify: `docs/examples/output/benchmark-ssh.txt`
- Modify: `internal/cli/self/status.go`
- Modify: `internal/cli/self/cfg_test.go`
- Modify: `internal/cli/self/bench/common.go`

- [ ] **Step 1: Replace TOML mentions**

Run:

```bash
rg -n 'config\\.toml|\\.toml|TOML|toml' README.md docs internal
```

Update user-facing text to YAML when it refers to nssh config format. Keep package names or historical plan docs unchanged if they are old artifacts under `docs/superpowers/plans/`.

- [ ] **Step 2: Update status and cfg command tests**

Update expected suffixes from:

```text
nssh/config.toml
```

to:

```text
nssh/config.yaml
```

- [ ] **Step 3: Verify docs and text**

Run:

```bash
go test ./internal/cli/self -run 'TestConfig|TestStatus' -count=1
/Users/cj/.local/bin/validate-markdown --check docs/nssh-config-yaml-mockup.md
/Users/cj/.local/bin/validate-markdown --check docs/nssh-config-decisions.md
```

Expected: PASS.

- [ ] **Step 4: Commit task 13**

```bash
git add README.md docs/examples/config/config.example.yaml docs/examples/output/benchmark-ssh.txt internal/cli/self/status.go internal/cli/self/cfg_test.go internal/cli/self/bench/common.go
git commit -m "docs: update config references for yaml cutover"
```

---

### Task 14: Full Verification

**Files:**
- No source edits unless verification exposes failures.

- [ ] **Step 1: Run package tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run vet**

Run:

```bash
go vet ./...
```

Expected: no output and exit code 0.

- [ ] **Step 3: Run gofmt**

Run:

```bash
gofmt -w .
```

Expected: no command output.

- [ ] **Step 4: Run full make test**

Run:

```bash
make test
```

Expected: PASS.

- [ ] **Step 5: Run targeted grep checks**

Run:

```bash
rg -n 'config\\.toml|WriteProviderSSHConfig|ensureSSHConfigInclude|sshconfig.NewParser\\(' internal cmd docs README.md
```

Expected:

- `config.toml` appears only in old historical docs under `docs/superpowers/plans/`, if at all.
- `WriteProviderSSHConfig` has no runtime call sites.
- `ensureSSHConfigInclude` does not exist.
- `sshconfig.NewParser(` appears only in SSH config import code and its tests.

- [ ] **Step 6: Commit verification fixes**

If verification required fixes:

```bash
git add .
git commit -m "test: finish yaml config cutover verification"
```

If no fixes were required, do not create an empty commit.

---

## Self-Review

Spec coverage:

- YAML canonical config: Tasks 1, 2, 3, 4, 13.
- Strict YAML loading and includes: Task 2.
- Provider-level `hosts`: Tasks 3, 6, 11.
- Singular group resolution: Tasks 3 and 6.
- Merge order: Tasks 6 and 7.
- OpenSSH argv rendering with no generated SSH config dependency: Tasks 5, 7, 12.
- SSH config import boundary: Task 10.
- Compatibility fix catalog: Task 8.
- Verbosity behavior: Task 9.
- Tests: every task includes targeted tests, with full verification in Task 14.

No raw argv config is introduced. OpenSSH remains the transport. Runtime behavior comes from nssh YAML config and provider state only.

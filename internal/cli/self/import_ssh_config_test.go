package self

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
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
	out, warnings, err := importSSHConfigText(strings.NewReader(input), "imported")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	for _, want := range []string{
		"options:",
		"IdentityAgent: ~/agent.sock",
		"ServerAliveInterval: 240s",
		"edge01.example.com:",
		"aliases:",
		"- edge01",
		"username: netops",
		"port: 2222",
		"ProxyJump: bastion",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("import output missing %q:\n%s", want, out)
		}
	}
}

func TestImportSSHConfigOmitsPasswordAuthTransportOptions(t *testing.T) {
	input := `
Host edge01
  HostName edge01.example.com
  User netops
  PreferredAuthentications keyboard-interactive,password
  PubkeyAuthentication no
`
	out, warnings, err := importSSHConfigText(strings.NewReader(input), "imported")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	for _, want := range []string{
		"edge01.example.com:",
		"auth:",
		"mode: password",
		"username: netops",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("import output missing %q:\n%s", want, out)
		}
	}
	for _, reject := range []string{"PreferredAuthentications:", "PubkeyAuthentication:"} {
		if strings.Contains(out, reject) {
			t.Fatalf("import output should omit %q when auth.mode is password:\n%s", reject, out)
		}
	}
}

func TestSSHConfigImportCommandHasNoSelectionFlags(t *testing.T) {
	cmd := NewImportSSHConfigCmd()

	for _, name := range []string{"source", "provider", "out", "dry-run"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("ssh-config import should not expose --%s", name)
		}
	}
}

func TestRunImportSSHConfigImportsApprovedIncludesIntoLocalProvider(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "conf.d"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host *
  IdentityAgent "~/agent.sock"

Include conf.d/work
Include nssh.d/generated

Host root-host
  HostName root.example.com
  User cj
`)
	writeFile(t, filepath.Join(sshDir, "conf.d", "work"), `
Host work-host work-alias
  HostName work.example.com
  User netops
`)
	writeFile(t, filepath.Join(sshDir, "nssh.d", "generated"), `
Host stale-host
  HostName stale.example.com
`)

	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigDir:  sshDir,
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	prompter := &fakeSSHImportPrompter{
		reviews: map[string]bool{
			"Import SSH defaults from " + paths.SSHConfigFile + "?":                       true,
			"Import SSH hosts from " + paths.SSHConfigFile + "?":                          true,
			"Import SSH hosts from " + filepath.Join(sshDir, "conf.d", "work") + "?":      true,
			"Import SSH hosts from " + filepath.Join(sshDir, "nssh.d", "generated") + "?": false,
		},
	}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	rootBytes, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read root config: %v", err)
	}
	root := string(rootBytes)
	for _, want := range []string{
		"ssh:",
		"defaults:",
		"options:",
		"IdentityAgent: ~/agent.sock",
	} {
		if !strings.Contains(root, want) {
			t.Fatalf("root config missing %q:\n%s", want, root)
		}
	}

	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory file: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{
		"providers:",
		"local:",
		"root.example.com:",
		"- root-host",
		"work.example.com:",
		"aliases:",
		"- work-alias",
		"username: netops",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("import output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "stale-host") {
		t.Fatalf("denied include was imported:\n%s", got)
	}
	if strings.Contains(got, "defaults:") {
		t.Fatalf("inventory import file should not contain ssh defaults:\n%s", got)
	}
	if strings.Contains(got, "group:") {
		t.Fatalf("hosts without a matching local group should not get group assignment:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "inventory", "ssh-import.yaml")); !os.IsNotExist(err) {
		t.Fatalf("ssh-import.yaml should not be written, stat error = %v", err)
	}
}

func TestRunImportSSHConfigMergesExistingLocalInventory(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge02
  HostName edge02.lab.local
  User netops
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        lab: {}
      hosts:
        edge01.lab.local:
          group: lab
          aliases:
            - edge01
`)
	prompter := &fakeSSHImportPrompter{}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory file: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{"edge01.lab.local:", "edge02.lab.local:", "group: lab", "username: netops"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged local inventory missing %q:\n%s", want, got)
		}
	}
}

func TestRunImportSSHConfigSkipsExistingInventoryHostnameMatch(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host imported-edge
  HostName edge01.lab.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, paths.ConfigFile, `include: [inventory/*.yaml]`)
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      hosts:
        edge01.lab.local:
          aliases:
            - edge01
`)
	prompter := &fakeSSHImportPrompter{}
	out := &strings.Builder{}

	if err := runImportSSHConfig(paths, prompter, out); err != nil {
		t.Fatalf("run import: %v", err)
	}

	if _, ok := prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"]; ok {
		t.Fatalf("duplicate hostname should not prompt for host import:\n%s", prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"])
	}
	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory: %v", err)
	}
	if got := string(gotBytes); strings.Contains(got, "imported-edge") {
		t.Fatalf("duplicate hostname was imported:\n%s", got)
	}
	if got := out.String(); !strings.Contains(got, `skipping imported host "edge01.lab.local": host "edge01.lab.local" already exists in inventory host "edge01.lab.local"`) {
		t.Fatalf("missing duplicate warning:\n%s", got)
	}
}

func TestRunImportSSHConfigSkipsExistingInventoryAliasMatch(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge01-alias
  HostName new-edge.lab.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, paths.ConfigFile, `include: [inventory/*.yaml]`)
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      hosts:
        edge01.lab.local:
          aliases:
            - edge01
            - edge01-alias
`)
	prompter := &fakeSSHImportPrompter{}
	out := &strings.Builder{}

	if err := runImportSSHConfig(paths, prompter, out); err != nil {
		t.Fatalf("run import: %v", err)
	}

	if _, ok := prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"]; ok {
		t.Fatalf("duplicate alias should not prompt for host import:\n%s", prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"])
	}
	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory: %v", err)
	}
	if got := string(gotBytes); strings.Contains(got, "new-edge.lab.local") {
		t.Fatalf("duplicate alias was imported:\n%s", got)
	}
	if got := out.String(); !strings.Contains(got, `skipping imported host "new-edge.lab.local": alias "edge01-alias" already exists in inventory host "edge01.lab.local"`) {
		t.Fatalf("missing duplicate warning:\n%s", got)
	}
}

func TestRunImportSSHConfigSkipsProviderStateHostnameMatch(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host imported-edge
  HostName edge01.netbox.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	inventory.SetStateDir(filepath.Join(tmp, "state"))
	t.Cleanup(func() { inventory.SetStateDir("") })
	if err := inventory.SaveProviderState(&inventory.ProviderState{
		Version:     inventory.StateVersion,
		Provider:    "netbox-prod",
		Type:        config.ProviderNetBox,
		LastRefresh: time.Now(),
		IncludeFile: inventory.ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*inventory.ProviderHost{
			"device:1": {
				ObjectID: "device:1",
				Host:     "edge01",
				Patterns: []string{"edge01"},
				Group:    "netbox-prod/customer",
				HostName: "edge01.netbox.local",
			},
		},
	}); err != nil {
		t.Fatalf("save provider state: %v", err)
	}
	prompter := &fakeSSHImportPrompter{}
	out := &strings.Builder{}

	if err := runImportSSHConfig(paths, prompter, out); err != nil {
		t.Fatalf("run import: %v", err)
	}

	if _, ok := prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"]; ok {
		t.Fatalf("provider state hostname duplicate should not prompt:\n%s", prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"])
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "inventory", "local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("local.yaml should not be written, stat error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, `skipping imported host "edge01.netbox.local": hostname "edge01.netbox.local" already exists in inventory host "netbox-prod/edge01"`) {
		t.Fatalf("missing provider state duplicate warning:\n%s", got)
	}
}

func TestRunImportSSHConfigSkipsProviderStatePatternMatch(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge01-alt
  HostName new-edge.netbox.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	inventory.SetStateDir(filepath.Join(tmp, "state"))
	t.Cleanup(func() { inventory.SetStateDir("") })
	if err := inventory.SaveProviderState(&inventory.ProviderState{
		Version:     inventory.StateVersion,
		Provider:    "netbox-prod",
		Type:        config.ProviderNetBox,
		LastRefresh: time.Now(),
		IncludeFile: inventory.ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*inventory.ProviderHost{
			"device:1": {
				ObjectID: "device:1",
				Host:     "edge01",
				Patterns: []string{"edge01", "edge01-alt"},
				Group:    "netbox-prod/customer",
				HostName: "edge01.netbox.local",
			},
		},
	}); err != nil {
		t.Fatalf("save provider state: %v", err)
	}
	prompter := &fakeSSHImportPrompter{}
	out := &strings.Builder{}

	if err := runImportSSHConfig(paths, prompter, out); err != nil {
		t.Fatalf("run import: %v", err)
	}

	if _, ok := prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"]; ok {
		t.Fatalf("provider state pattern duplicate should not prompt:\n%s", prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"])
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "inventory", "local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("local.yaml should not be written, stat error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, `skipping imported host "new-edge.netbox.local": alias "edge01-alt" already exists in inventory host "netbox-prod/edge01"`) {
		t.Fatalf("missing provider state duplicate warning:\n%s", got)
	}
}

func TestRunImportSSHConfigInfersGroupsFromLocalProviderDomainSuffix(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge01
  HostName edge01.custcbb.local

Host rpi-a
  HostName rpi-a.lan.ntwrknrd.net
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        custcbb:
          match:
            domain_suffix: [.custcbb.local]
        homelab:
          match:
            domain_suffix: [.lan.ntwrknrd.net]
`)
	prompter := &fakeSSHImportPrompter{}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory file: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{
		"edge01.custcbb.local:",
		"group: custcbb",
		"rpi-a.lan.ntwrknrd.net:",
		"group: homelab",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("local inventory missing inferred group %q:\n%s", want, got)
		}
	}
	if len(prompter.inputPrompts) != 0 {
		t.Fatalf("import should not prompt for groups, got %#v", prompter.inputPrompts)
	}
}

func TestRunImportSSHConfigImportsHostWithoutGroupWhenDomainSuffixDoesNotMatch(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge01
  HostName edge01.unknown.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        custcbb:
          match:
            domain_suffix: [.custcbb.local]
`)

	if err := runImportSSHConfig(paths, &fakeSSHImportPrompter{}, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory file: %v", err)
	}
	got := string(gotBytes)
	if !strings.Contains(got, "edge01.unknown.local:") {
		t.Fatalf("unmatched host should be imported without a group:\n%s", got)
	}
	if strings.Contains(got, "group:") {
		t.Fatalf("unmatched host should not get a group assignment:\n%s", got)
	}
	if !strings.Contains(got, "groups:") || !strings.Contains(got, "custcbb:") {
		t.Fatalf("existing local groups should be preserved:\n%s", got)
	}
}

func TestRunImportSSHConfigMergesLocalInventoryWithoutCredentialIncludes(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge02
  HostName edge02.lab.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        lab:
          auth:
            mode: password
            credential_provider: op-expedient
            password_ref: op://Expedient/item/password
      hosts:
        edge01.lab.local:
          group: lab
          aliases:
            - edge01
`)
	prompter := &fakeSSHImportPrompter{}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	gotBytes, err := os.ReadFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"))
	if err != nil {
		t.Fatalf("read local inventory file: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{"edge01.lab.local:", "edge02.lab.local:", "credential_provider: op-expedient"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged local inventory missing %q:\n%s", want, got)
		}
	}
}

func TestRunImportSSHConfigSkipsHostWriteWhenDiffRejected(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge01
  HostName edge01.lab.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	prompter := &fakeSSHImportPrompter{
		reviews: map[string]bool{
			"Import SSH hosts from " + paths.SSHConfigFile + "?": false,
		},
	}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "inventory", "local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("local.yaml should not be written, stat error = %v", err)
	}
	got := prompter.reviewBodies["Import SSH hosts from "+paths.SSHConfigFile+"?"]
	for _, want := range []string{
		"--- " + filepath.Join(paths.ConfigDir, "inventory", "local.yaml"),
		"+++ " + filepath.Join(paths.ConfigDir, "inventory", "local.yaml"),
		"+        edge01.lab.local:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff review missing %q:\n%s", want, got)
		}
	}
}

func TestRunImportSSHConfigWritesApprovedDefaultBeforeNextPrompt(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host *
  IdentityAgent "~/agent.sock"

Host edge01
  HostName edge01.lab.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	checkedBeforeHostPrompt := false
	prompter := &fakeSSHImportPrompter{
		reviews: map[string]bool{
			"Import SSH defaults from " + paths.SSHConfigFile + "?": true,
			"Import SSH hosts from " + paths.SSHConfigFile + "?":    false,
		},
		beforeReview: func(title string) {
			if title != "Import SSH hosts from "+paths.SSHConfigFile+"?" {
				return
			}
			checkedBeforeHostPrompt = true
			gotBytes, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatalf("approved defaults should be written before host prompt: %v", err)
			}
			if got := string(gotBytes); !strings.Contains(got, "IdentityAgent: ~/agent.sock") {
				t.Fatalf("approved defaults missing before host prompt:\n%s", got)
			}
		},
	}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}
	if !checkedBeforeHostPrompt {
		t.Fatal("host prompt was not shown")
	}
	if _, err := os.Stat(filepath.Join(paths.ConfigDir, "inventory", "local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("rejected host diff should not write local.yaml, stat error = %v", err)
	}
}

func TestRunImportSSHConfigShowsDefaultDiffBeforeConfirm(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host *
  IdentityAgent "~/agent.sock"
  LogLevel ERROR
  ServerAliveInterval 240
  ServerAliveCountMax 2
  SetEnv TERM=xterm-256color
  SetEnv COLORTERM=truecolor
  ControlMaster auto
  ControlPath ~/.ssh/sockets/%r@%h:%p
  ControlPersist 12h
  TCPKeepAlive yes
  GSSAPIAuthentication no
  IdentitiesOnly yes
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	prompter := &fakeSSHImportPrompter{}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	got := prompter.descriptions["Import SSH defaults from "+paths.SSHConfigFile+"?"]
	for _, want := range []string{
		"--- " + paths.ConfigFile,
		"+++ " + paths.ConfigFile,
		"+ssh:",
		"+  defaults:",
		"+    options:",
		"+      ControlMaster: auto",
		"+      ControlPath: ~/.ssh/sockets/%r@%h:%p",
		"+      ControlPersist: 12h",
		"+      GSSAPIAuthentication: false",
		"+      IdentitiesOnly: true",
		"+      IdentityAgent: ~/agent.sock",
		"+      LogLevel: ERROR",
		"+      ServerAliveCountMax: \"2\"",
		"+      ServerAliveInterval: 240s",
		"+      SetEnv:",
		"+        COLORTERM: truecolor",
		"+        TERM: xterm-256color",
		"+      TCPKeepAlive: true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("default preview missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "...") {
		t.Fatalf("default preview should not hide config:\n%s", got)
	}
}

func TestRunImportSSHConfigDefaultDiffPreservesRootComments(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host *
  IdentityAgent "~/agent.sock"
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, paths.ConfigFile, `
# Import credential providers and inventory provider files.
# Keep this root config lean; put provider-specific settings in included files.
include:
  - credential/*.yaml
  - inventory/*.yaml

agent:
  # How long the nssh runtime agent can sit idle before exiting.
  idle_timeout: 4h

ssh:
  security:
    # Host key behavior preset.
    host_key_policy: pin
`)
	prompter := &fakeSSHImportPrompter{
		reviews: map[string]bool{
			"Import SSH defaults from " + paths.SSHConfigFile + "?": false,
		},
	}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	got := prompter.descriptions["Import SSH defaults from "+paths.SSHConfigFile+"?"]
	for _, unwanted := range []string{
		"-# Import credential providers and inventory provider files.",
		"-agent:",
		"-  security:",
		"-    # Host key behavior preset.",
		"\n-\n",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("default diff should not delete existing config/comment %q:\n%s", unwanted, got)
		}
	}
	for _, want := range []string{
		"+  defaults:",
		"+    options:",
		"+      IdentityAgent: ~/agent.sock",
		"    # Host key behavior preset.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("default diff missing %q:\n%s", want, got)
		}
	}
}

func TestRunImportSSHConfigShowsIncludeDiffBeforeConfirm(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	includeDir := filepath.Join(sshDir, "conf.d")
	if err := os.MkdirAll(includeDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), "Include conf.d/work\n")
	includePath := filepath.Join(includeDir, "work")
	writeFile(t, includePath, `
Host work-host work-alias
  HostName work.example.com
  User netops
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        lab:
          match:
            domain_suffix: [.example.com]
`)
	prompter := &fakeSSHImportPrompter{
		reviews: map[string]bool{
			"Import SSH hosts from " + includePath + "?": false,
		},
	}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	got := prompter.descriptions["Import SSH hosts from "+includePath+"?"]
	for _, want := range []string{
		"--- " + filepath.Join(paths.ConfigDir, "inventory", "local.yaml"),
		"+++ " + filepath.Join(paths.ConfigDir, "inventory", "local.yaml"),
		"+        work.example.com:",
		"+          group: lab",
		"+          aliases:",
		"+            - work-host",
		"+            - work-alias",
		"+          auth:",
		"+            username: netops",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("include preview missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "...") {
		t.Fatalf("include preview should not hide config:\n%s", got)
	}
}

func TestRunImportSSHConfigHostDiffPreservesLocalInventoryComments(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sshDir, "config"), `
Host edge02
  HostName edge02.lab.local
`)
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, ".config", "nssh"),
		ConfigFile:    filepath.Join(tmp, ".config", "nssh", "config.yaml"),
		SSHConfigFile: filepath.Join(sshDir, "config"),
	}
	writeFile(t, filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      # Inventory provider type.
      type: local

      groups:
        lab:
          match:
            # Select local hosts with this domain suffix into this group.
            domain_suffix:
              - .lab.local

      hosts:
        edge01.lab.local:
          # Existing host comment.
          group: lab
          aliases:
            - edge01
`)
	prompter := &fakeSSHImportPrompter{
		reviews: map[string]bool{
			"Import SSH hosts from " + paths.SSHConfigFile + "?": false,
		},
	}

	if err := runImportSSHConfig(paths, prompter, ioDiscard{}); err != nil {
		t.Fatalf("run import: %v", err)
	}

	got := prompter.descriptions["Import SSH hosts from "+paths.SSHConfigFile+"?"]
	for _, unwanted := range []string{
		"-      # Inventory provider type.",
		"-      groups:",
		"-            # Select local hosts with this domain suffix into this group.",
		"-      hosts:",
		"-          # Existing host comment.",
		"\n-\n",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("host diff should not delete existing inventory/comment %q:\n%s", unwanted, got)
		}
	}
	for _, want := range []string{
		"+        edge02.lab.local:",
		"+          group: lab",
		"+          aliases:",
		"+            - edge02",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("host diff missing %q:\n%s", want, got)
		}
	}
}

type fakeSSHImportPrompter struct {
	confirms     map[string]bool
	descriptions map[string]string
	inputs       map[string]string
	inputPrompts []string
	reviews      map[string]bool
	reviewBodies map[string]string
	beforeReview func(title string)
}

func (p *fakeSSHImportPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	return p.ConfirmWithDescription(title, "", defaultValue)
}

func (p *fakeSSHImportPrompter) ConfirmWithDescription(title, description string, defaultValue bool) (bool, error) {
	if p.descriptions == nil {
		p.descriptions = make(map[string]string)
	}
	p.descriptions[title] = description
	if p.confirms == nil {
		return defaultValue, nil
	}
	value, ok := p.confirms[title]
	if !ok {
		return defaultValue, nil
	}
	return value, nil
}

func (p *fakeSSHImportPrompter) Review(title, body string, defaultValue bool) (bool, error) {
	if p.beforeReview != nil {
		p.beforeReview(title)
	}
	if p.reviewBodies == nil {
		p.reviewBodies = make(map[string]string)
	}
	if p.descriptions == nil {
		p.descriptions = make(map[string]string)
	}
	p.reviewBodies[title] = body
	p.descriptions[title] = body
	if p.reviews == nil {
		if p.confirms != nil {
			value, ok := p.confirms[title]
			if ok {
				return value, nil
			}
		}
		return defaultValue, nil
	}
	value, ok := p.reviews[title]
	if !ok {
		return defaultValue, nil
	}
	return value, nil
}

func (p *fakeSSHImportPrompter) InputWithDefault(title, defaultValue string) (string, error) {
	p.inputPrompts = append(p.inputPrompts, title)
	if p.inputs == nil {
		return defaultValue, nil
	}
	value, ok := p.inputs[title]
	if !ok {
		return defaultValue, nil
	}
	return value, nil
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(text, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

var _ sshImportPrompter = (*fakeSSHImportPrompter)(nil)

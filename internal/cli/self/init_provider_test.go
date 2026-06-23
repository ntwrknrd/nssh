package self

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestInitPlanDefaultsToSOPSAgeCredentialProviderOnly(t *testing.T) {
	plan, err := buildInitPlan(initPlanRequest{})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	if !strings.Contains(plan.Summary(), "Credential providers: SOPS+age") {
		t.Fatalf("summary missing SOPS+age provider:\n%s", plan.Summary())
	}
	for _, reject := range []string{"Inventory sources", "Credential assignment"} {
		if strings.Contains(plan.Summary(), reject) {
			t.Fatalf("summary should not include %q:\n%s", reject, plan.Summary())
		}
	}

	cfg := plan.Config
	if cfg.Credential.Provider["sops"].Type != config.CredentialProviderSOPSAge {
		t.Fatalf("sops provider = %+v", cfg.Credential.Provider["sops"])
	}
	if got := cfg.Inventory.Provider["local"].Group["default"].Auth; got.IsSet() {
		t.Fatalf("self init should not assign inventory auth, got %+v", got)
	}
}

func TestInitPlanWritesCredentialProvidersWithoutAuthMaterial(t *testing.T) {
	plan, err := buildInitPlan(initPlanRequest{
		CredentialProviders: []initCredentialProviderRequest{
			{Name: "sops", Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
			{Name: "op-network", Type: config.CredentialProvider1Password, Account: "ntwrknrd", Vault: "Network", AuthToken: "should-not-be-stored"},
			{Name: "bw-lab", Type: config.CredentialProviderBitwarden, BWSession: "should-not-be-stored"},
		},
	})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	yamlText := encodeInitPlanConfig(t, plan.Config)
	for _, reject := range []string{"should-not-be-stored", "BW_SESSION", "auth_token", "session:"} {
		if strings.Contains(yamlText, reject) {
			t.Fatalf("config stored auth material %q:\n%s", reject, yamlText)
		}
	}
}

func TestApplyInitPlanBacksUpExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("old: true\n"), 0600); err != nil {
		t.Fatal(err)
	}

	plan, err := buildInitPlan(initPlanRequest{})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}
	if err := applyInitPlan(paths, plan); err != nil {
		t.Fatalf("applyInitPlan: %v", err)
	}

	backup := paths.ConfigFile + ".bak"
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(data) != "old: true\n" {
		t.Fatalf("backup content = %q", data)
	}
}

func TestApplyCredentialProviderSetupMergesExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	localProvider := cfg.Inventory.Provider[config.ProviderLocal]
	defaultGroup := localProvider.Group["default"]
	defaultGroup.Auth = config.InventoryAuthConfig{}
	localProvider.Group["default"] = defaultGroup
	cfg.Inventory.Provider[config.ProviderLocal] = localProvider
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	prompter := &fakeInitPrompter{}
	if err := applyCredentialProviderSetup(paths, cfg, config.CredentialProviderSOPSAge, prompter, false); err != nil {
		t.Fatalf("applyCredentialProviderSetup: %v", err)
	}

	loaded, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := loaded.Credential.Provider["op-network"]; !ok {
		t.Fatalf("existing provider was lost: %+v", loaded.Credential.Provider)
	}
	if loaded.Credential.Provider["sops"].Type != config.CredentialProviderSOPSAge {
		t.Fatalf("sops provider = %+v", loaded.Credential.Provider["sops"])
	}
	if loaded.Inventory.Provider["local"].Group["default"].Auth.IsSet() {
		t.Fatalf("default group auth should not be assigned by provider setup: %+v", loaded.Inventory.Provider["local"].Group["default"].Auth)
	}
	if _, err := os.Stat(paths.ConfigFile + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestApplyCredentialProviderSetupUsesCanonicalSOPSNameWithoutPrompt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PATH", filepath.Join(tmp, "bin"))
	credentialsFile := filepath.Join(tmp, "credentials.sops.yaml")
	ageKeyFile := filepath.Join(tmp, "keys.txt")
	if err := os.WriteFile(credentialsFile, []byte("encrypted: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ageKeyFile, []byte("# public key: age1test\nAGE-SECRET-KEY-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{}
	prompter := &fakeInitPrompter{inputs: map[string]string{
		"SOPS file":         credentialsFile,
		"SOPS age key file": ageKeyFile,
	}}

	if err := applyCredentialProviderSetup(paths, cfg, config.CredentialProviderSOPSAge, prompter, false); err != nil {
		t.Fatalf("applyCredentialProviderSetup: %v", err)
	}
	if prompter.saw("SOPS+age provider name") {
		t.Fatalf("explicit sops-age setup should not ask for provider name; prompts were %v", prompter.prompts)
	}
	for _, want := range []string{"SOPS file", "SOPS age key file"} {
		if !prompter.saw(want) {
			t.Fatalf("explicit sops-age setup should ask for %q; prompts were %v", want, prompter.prompts)
		}
	}
	loaded, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	provider := loaded.Credential.Provider["sops"]
	if provider.Type != config.CredentialProviderSOPSAge {
		t.Fatalf("sops provider = %+v", provider)
	}
	if provider.File != credentialsFile || provider.AgeKeyFile != ageKeyFile {
		t.Fatalf("sops paths = file %q age_key_file %q", provider.File, provider.AgeKeyFile)
	}
}

func TestPromptSOPSAgeProviderDefaultsAgeKeyFileFromEnv(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "env-age-keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", envPath)

	prompter := &fakeInitPrompter{}
	req, err := promptSOPSAgeProvider(prompter)
	if err != nil {
		t.Fatalf("promptSOPSAgeProvider: %v", err)
	}

	if req.AgeKeyFile != envPath {
		t.Fatalf("age key file = %q, want %q", req.AgeKeyFile, envPath)
	}
	if prompter.inputDefaults["SOPS age key file"] != envPath {
		t.Fatalf("age key prompt default = %q, want %q", prompter.inputDefaults["SOPS age key file"], envPath)
	}
}

func TestPromptSOPSAgeProviderDefaultsAgeKeyFileFromExistingConfigPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	existing := filepath.Join(tmp, ".config", "sops", "age", "keys.txt")
	if err := os.MkdirAll(filepath.Dir(existing), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("# public key: age1test\nAGE-SECRET-KEY-test\n"), 0600); err != nil {
		t.Fatal(err)
	}

	prompter := &fakeInitPrompter{}
	req, err := promptSOPSAgeProvider(prompter)
	if err != nil {
		t.Fatalf("promptSOPSAgeProvider: %v", err)
	}

	expected := "~/.config/sops/age/keys.txt"
	if req.AgeKeyFile != expected {
		t.Fatalf("age key file = %q, want %q", req.AgeKeyFile, expected)
	}
	if prompter.inputDefaults["SOPS age key file"] != expected {
		t.Fatalf("age key prompt default = %q, want %q", prompter.inputDefaults["SOPS age key file"], expected)
	}
}

func TestPromptSOPSAgeProviderDefaultsAgeKeyFileToSOPSDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	prompter := &fakeInitPrompter{}
	req, err := promptSOPSAgeProvider(prompter)
	if err != nil {
		t.Fatalf("promptSOPSAgeProvider: %v", err)
	}

	expected := defaultSOPSAgeIdentityPath()
	if req.AgeKeyFile != expected {
		t.Fatalf("age key file = %q, want %q", req.AgeKeyFile, expected)
	}
	if prompter.inputDefaults["SOPS age key file"] != expected {
		t.Fatalf("age key prompt default = %q, want %q", prompter.inputDefaults["SOPS age key file"], expected)
	}
}

func TestApplyCredentialProviderSetupPrintsProviderSection(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PATH", filepath.Join(tmp, "bin"))
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "sops"), "#!/bin/sh\nexit 0\n")
	credentialsFile := filepath.Join(tmp, "credentials.sops.yaml")
	ageKeyFile := filepath.Join(tmp, "keys.txt")
	if err := os.WriteFile(credentialsFile, []byte("encrypted: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ageKeyFile, []byte("# public key: age1test\nAGE-SECRET-KEY-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{}
	prompter := &fakeInitPrompter{inputs: map[string]string{
		"SOPS file":         credentialsFile,
		"SOPS age key file": ageKeyFile,
	}}

	got := captureStdout(t, func() {
		if err := applyCredentialProviderSetup(paths, cfg, config.CredentialProviderSOPSAge, prompter, false); err != nil {
			t.Fatalf("applyCredentialProviderSetup: %v", err)
		}
	})

	wantOrder := []string{
		"Provider binary: ready (" + filepath.Join(binDir, "sops") + ")",
		"Provider config: " + filepath.Join(paths.ConfigDir, "credential", "sops.yaml"),
		"SOPS file: " + credentialsFile,
		"Age key file: " + ageKeyFile,
		"Config file: " + paths.ConfigFile,
	}
	last := -1
	for _, want := range wantOrder {
		idx := strings.Index(got, want)
		if idx == -1 {
			t.Fatalf("provider setup output missing %q:\n%s", want, got)
		}
		if idx < last {
			t.Fatalf("provider setup output printed %q out of order:\n%s", want, got)
		}
		last = idx
	}
	for _, reject := range []string{"[?]", "Credential provider:", "Provider readiness:", "Configured provider:", "Provider type:"} {
		if strings.Contains(got, reject) {
			t.Fatalf("provider setup output should not contain %q:\n%s", reject, got)
		}
	}
	for _, want := range []string{
		"[✓] Provider config:",
		"[✓] SOPS file:",
		"[✓] Age key file:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("provider setup output missing status styling %q:\n%s", want, got)
		}
	}
}

func TestApplyCredentialProviderSetupWritesProviderIncludeFile(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "sops"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)
	credentialsFile := filepath.Join(tmp, "credentials.sops.yaml")
	ageKeyFile := filepath.Join(tmp, "keys.txt")
	if err := os.WriteFile(credentialsFile, []byte("encrypted: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ageKeyFile, []byte("# public key: age1test\nAGE-SECRET-KEY-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "inventory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "inventory", "local.yaml"), []byte("inventory:\n  providers:\n    local:\n      type: local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("include: [inventory/*.yaml]\nagent:\n  auto_start: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prompter := &fakeInitPrompter{inputs: map[string]string{
		"SOPS file":         credentialsFile,
		"SOPS age key file": ageKeyFile,
	}}

	got := captureStdout(t, func() {
		if err := applyCredentialProviderSetup(paths, cfg, config.CredentialProviderSOPSAge, prompter, false); err != nil {
			t.Fatalf("applyCredentialProviderSetup: %v", err)
		}
	})

	providerPath := filepath.Join(paths.ConfigDir, "credential", "sops.yaml")
	providerText, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider config: %v", err)
	}
	for _, want := range []string{
		"credential:",
		"sops:",
		"type: sops-age",
		"file: " + credentialsFile,
		"age_key_file: " + ageKeyFile,
	} {
		if !strings.Contains(string(providerText), want) {
			t.Fatalf("provider file missing %q:\n%s", want, providerText)
		}
	}
	for _, reject := range []string{"agent:", "inventory:", "logging:", "ssh:"} {
		if strings.Contains(string(providerText), reject) {
			t.Fatalf("provider file should not contain %q:\n%s", reject, providerText)
		}
	}
	rootText, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read root config: %v", err)
	}
	for _, want := range []string{"credential/*.yaml", "inventory/*.yaml"} {
		if !strings.Contains(string(rootText), want) {
			t.Fatalf("root config missing include %q:\n%s", want, rootText)
		}
	}
	if strings.Contains(string(rootText), "credential:\n") {
		t.Fatalf("root config should not inline credential providers:\n%s", rootText)
	}
	loaded, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if provider := loaded.Credential.Provider["sops"]; provider.Type != config.CredentialProviderSOPSAge || provider.File != credentialsFile || provider.AgeKeyFile != ageKeyFile {
		t.Fatalf("loaded sops provider = %+v", provider)
	}
	if !strings.Contains(got, "Provider config: "+providerPath) {
		t.Fatalf("provider setup output missing provider config file:\n%s", got)
	}
	for _, reject := range []string{"Configured provider:", "Provider type:"} {
		if strings.Contains(got, reject) {
			t.Fatalf("provider setup output should not contain %q:\n%s", reject, got)
		}
	}
}

func TestApplyCredentialProviderSetupSkipsPromptsWhenSOPSAlreadyConfigured(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "sops"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigDir, "credential"), 0700); err != nil {
		t.Fatal(err)
	}
	providerPath := filepath.Join(paths.ConfigDir, "credential", "sops.yaml")
	if err := os.WriteFile(providerPath, []byte(`
credential:
  provider:
    sops:
      type: sops-age
      file: ~/.local/share/nssh/credentials.sops.yaml
      age_key_file: ~/.config/sops/age/keys.txt
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("include: [credential/*.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prompter := &fakeInitPrompter{}

	got := captureStdout(t, func() {
		if err := applyCredentialProviderSetup(paths, cfg, config.CredentialProviderSOPSAge, prompter, false); err != nil {
			t.Fatalf("applyCredentialProviderSetup: %v", err)
		}
	})

	if len(prompter.prompts) != 0 {
		t.Fatalf("already configured setup should not prompt, got %v", prompter.prompts)
	}
	for _, want := range []string{
		"Provider binary: ready (" + filepath.Join(binDir, "sops") + ")",
		"Provider config: " + providerPath,
		"SOPS file: ~/.local/share/nssh/credentials.sops.yaml",
		"Age key file: ~/.config/sops/age/keys.txt",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("already configured output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Config file:") {
		t.Fatalf("already configured setup should not rewrite root config:\n%s", got)
	}
}

func TestApplyInitPlanWritesCredentialProviderIncludeFiles(t *testing.T) {
	tmp := t.TempDir()
	paths := &config.Paths{
		ConfigDir:  filepath.Join(tmp, "config", "nssh"),
		ConfigFile: filepath.Join(tmp, "config", "nssh", "config.yaml"),
	}
	plan, err := buildInitPlan(initPlanRequest{
		CredentialProviders: []initCredentialProviderRequest{{
			Name:       "sops",
			Type:       config.CredentialProviderSOPSAge,
			File:       "~/.local/share/nssh/credentials.sops.yaml",
			AgeKeyFile: "~/.config/sops/age/keys.txt",
		}},
	})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	if err := applyInitPlan(paths, plan); err != nil {
		t.Fatalf("applyInitPlan: %v", err)
	}

	rootText, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read root config: %v", err)
	}
	if !strings.Contains(string(rootText), "credential/*.yaml") {
		t.Fatalf("root config missing credential include:\n%s", rootText)
	}
	if strings.Contains(string(rootText), "credential:\n") {
		t.Fatalf("root config should not inline credential providers:\n%s", rootText)
	}
	providerPath := filepath.Join(paths.ConfigDir, "credential", "sops.yaml")
	providerText, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider config: %v", err)
	}
	if !strings.Contains(string(providerText), "type: sops-age") || !strings.Contains(string(providerText), "age_key_file: ~/.config/sops/age/keys.txt") {
		t.Fatalf("provider config missing sops fields:\n%s", providerText)
	}
	if strings.Contains(string(providerText), "session:") {
		t.Fatalf("provider config should not contain session:\n%s", providerText)
	}
	for _, reject := range []string{"agent:", "inventory:", "logging:", "ssh:"} {
		if strings.Contains(string(providerText), reject) {
			t.Fatalf("provider file should not contain %q:\n%s", reject, providerText)
		}
	}
	loaded, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if loaded.Credential.Provider["sops"].Type != config.CredentialProviderSOPSAge {
		t.Fatalf("loaded credential providers = %+v", loaded.Credential.Provider)
	}
}

func TestPrepareSOPSAgeSetupCreatesAgeKeyAndEncryptedStarterFile(t *testing.T) {
	tmp := t.TempDir()
	identity := filepath.Join(tmp, "keys.txt")
	credentials := filepath.Join(tmp, "credentials.sops.yaml")
	oldRun := runSelfInitCommand
	defer func() { runSelfInitCommand = oldRun }()
	var calls []string
	var sopsStdin string
	runSelfInitCommand = func(name string, stdin []byte, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch name {
		case "age-keygen":
			if err := os.WriteFile(identity, []byte("# public key: age1testrecipient\nAGE-SECRET-KEY-test\n"), 0600); err != nil {
				return nil, err
			}
		case "sops":
			sopsStdin = string(stdin)
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "--output" {
					if err := os.WriteFile(args[i+1], []byte("encrypted: true\n"), 0600); err != nil {
						return nil, err
					}
				}
			}
		}
		return nil, nil
	}

	provider := initCredentialProviderRequest{
		Name:       "sops",
		Type:       config.CredentialProviderSOPSAge,
		File:       credentials,
		AgeKeyFile: identity,
	}
	if err := prepareCredentialProviderSetup(&provider, &fakeInitPrompter{}, false); err != nil {
		t.Fatalf("prepareCredentialProviderSetup: %v", err)
	}
	if strings.Join(calls, "\n") != "age-keygen -o "+identity+"\nsops --encrypt --age age1testrecipient --input-type yaml --output-type yaml --output "+credentials+" /dev/stdin" {
		t.Fatalf("calls = %v", calls)
	}
	if !strings.Contains(sopsStdin, "password: \"\"") {
		t.Fatalf("starter stdin = %q", sopsStdin)
	}
	data, err := os.ReadFile(credentials)
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if strings.Contains(string(data), "password:") {
		t.Fatalf("credential file contains cleartext starter:\n%s", data)
	}
}

func TestPrepareSOPSAgeSetupSkipsStarterWhenAgeKeyIsDeclined(t *testing.T) {
	tmp := t.TempDir()
	identity := filepath.Join(tmp, "keys.txt")
	credentials := filepath.Join(tmp, "credentials.sops.yaml")
	oldRun := runSelfInitCommand
	defer func() { runSelfInitCommand = oldRun }()
	runSelfInitCommand = func(name string, stdin []byte, args ...string) ([]byte, error) {
		t.Fatalf("runSelfInitCommand should not be called, got %s %v", name, args)
		return nil, nil
	}

	provider := initCredentialProviderRequest{
		Name:       "sops",
		Type:       config.CredentialProviderSOPSAge,
		File:       credentials,
		AgeKeyFile: identity,
	}
	prompter := &fakeInitPrompter{confirms: map[string]bool{
		"Create SOPS age key at " + AbbreviatePath(identity) + "?": false,
	}}
	if err := prepareCredentialProviderSetup(&provider, prompter, false); err != nil {
		t.Fatalf("prepareCredentialProviderSetup: %v", err)
	}
	if _, err := os.Stat(credentials); !os.IsNotExist(err) {
		t.Fatalf("credentials file state error = %v, want missing", err)
	}
}

func TestCredentialProviderSetupLabelKeepsUsefulProviderType(t *testing.T) {
	got := credentialProviderSetupLabel(initCredentialProviderRequest{
		Name: "op-expedient",
		Type: config.CredentialProvider1Password,
	})

	if got != "op-expedient (1Password)" {
		t.Fatalf("setup label = %q", got)
	}
}

func TestRunInitExistingConfigPrintsNextCommands(t *testing.T) {
	if os.Getenv("NSSH_TEST_RUN_INIT_EXISTING_CONFIG") == "1" {
		runInitExistingConfigHelper(t)
		return
	}

	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "nssh"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestRunInitExistingConfigPrintsNextCommands$")
	cmd.Env = append(os.Environ(),
		"NSSH_TEST_RUN_INIT_EXISTING_CONFIG=1",
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+filepath.Join(tmp, "config"),
		"XDG_DATA_HOME="+filepath.Join(tmp, "data"),
		"XDG_STATE_HOME="+filepath.Join(tmp, "state"),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runInit helper failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "nssh:") {
		t.Fatalf("runInit printed app-level error prefix:\n%s", output)
	}
	for _, want := range []string{"Config file:", "nssh self init sops-age", "nssh self init 1password", "nssh self init bitwarden", "nssh inv set <host-or-group>"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("runInit output missing %q:\n%s", want, output)
		}
	}
	for _, reject := range []string{"nssh initialized successfully", "Next Steps"} {
		if strings.Contains(string(output), reject) {
			t.Fatalf("runInit existing-config output should not contain %q:\n%s", reject, output)
		}
	}
}

func TestRunInitProviderSetupStopsBeforeDependencyChecks(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, bin := range []string{"nssh", "sops", "ssh", "scp", "asciinema", "agg", "fzf"} {
		if err := os.WriteFile(filepath.Join(binDir, bin), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("PATH", binDir)

	paths := config.DefaultPaths()
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, ".local", "share", "nssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "config", "sops", "age"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "config", "sops", "age", "keys.txt"), []byte("# public key: age1test\nAGE-SECRET-KEY-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".local", "share", "nssh", "credentials.sops.yaml"), []byte("encrypted: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(paths.ConfigFile, config.DefaultConfig()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(InitOptions{CredentialProviderType: config.CredentialProviderSOPSAge}); err != nil {
			t.Fatalf("runInit: %v", err)
		}
	})

	if !strings.Contains(output, "Provider config:") {
		t.Fatalf("provider setup output missing:\n%s", output)
	}
	for _, reject := range []string{"Dependencies", "Install fzf?", "Status", "Next Steps", "nssh initialized successfully"} {
		if strings.Contains(output, reject) {
			t.Fatalf("provider setup should not continue into %q:\n%s", reject, output)
		}
	}
}

func runInitExistingConfigHelper(t *testing.T) {
	t.Helper()

	paths := config.DefaultPaths()
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	err := runInit(InitOptions{})
	if err != nil {
		t.Fatalf("runInit error = %v, want nil", err)
	}
}

func TestInitCommandDoesNotExposeYesFlag(t *testing.T) {
	cmd := NewInitCmd()

	if flag := cmd.Flags().Lookup("yes"); flag != nil {
		t.Fatalf("init exposes --yes flag")
	}
	if shorthand := cmd.Flags().ShorthandLookup("y"); shorthand != nil {
		t.Fatalf("init exposes -y shorthand")
	}
}

func TestInitCommandAcceptsCredentialProviderArg(t *testing.T) {
	cmd := NewInitCmd()

	for _, provider := range []string{"sops-age", "1password", "bitwarden"} {
		if err := cmd.Args(cmd, []string{provider}); err != nil {
			t.Fatalf("init %s args error = %v", provider, err)
		}
	}
	if err := cmd.Args(cmd, []string{"vault"}); err == nil {
		t.Fatal("init accepted unsupported provider arg")
	}
}

func TestPromptInitPlanRequestShowsCredentialProvidersOnly(t *testing.T) {
	prompter := &fakeInitPrompter{
		selects: map[string]string{
			"Credential providers":            config.CredentialProvider1Password,
			"Add another credential provider": "done",
		},
		inputs: map[string]string{
			"1Password provider name": "op-network",
			"1Password account":       "ntwrknrd",
			"1Password vault":         "Network",
		},
	}

	req, err := promptInitPlanRequest(prompter, nil)
	if err != nil {
		t.Fatalf("promptInitPlanRequest: %v", err)
	}

	for _, want := range []string{"Credential providers", "1Password provider name", "1Password account", "1Password vault"} {
		if !prompter.saw(want) {
			t.Fatalf("prompt flow did not show %q; prompts were %v", want, prompter.prompts)
		}
	}
	for _, reject := range []string{"Inventory sources: NetBox", "Inventory sources: Containerlab", "Credential assignment: local/default"} {
		if prompter.saw(reject) {
			t.Fatalf("prompt flow should not show %q; prompts were %v", reject, prompter.prompts)
		}
	}
	if req.CredentialProviders[0].Type != config.CredentialProvider1Password {
		t.Fatalf("credential providers = %+v", req.CredentialProviders)
	}
}

func encodeInitPlanConfig(t *testing.T, cfg *config.Config) string {
	t.Helper()

	text, err := config.MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("encode yaml: %v", err)
	}
	return text
}

func writeExecutable(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0700); err != nil {
		t.Fatal(err)
	}
}

type fakeInitPrompter struct {
	selects       map[string]string
	inputs        map[string]string
	confirms      map[string]bool
	prompts       []string
	inputDefaults map[string]string
}

func (p *fakeInitPrompter) Select(title string, _ []ui.SelectOption) (string, error) {
	p.prompts = append(p.prompts, title)
	return p.selects[title], nil
}

func (p *fakeInitPrompter) Input(title, defaultValue string) (string, error) {
	p.prompts = append(p.prompts, title)
	if p.inputDefaults == nil {
		p.inputDefaults = make(map[string]string)
	}
	p.inputDefaults[title] = defaultValue
	if v, ok := p.inputs[title]; ok {
		return v, nil
	}
	return defaultValue, nil
}

func (p *fakeInitPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	p.prompts = append(p.prompts, title)
	if v, ok := p.confirms[title]; ok {
		return v, nil
	}
	return defaultValue, nil
}

func (p *fakeInitPrompter) saw(title string) bool {
	for _, prompt := range p.prompts {
		if prompt == title {
			return true
		}
	}
	return false
}

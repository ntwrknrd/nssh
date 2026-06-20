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

func TestInitPlanDefaultsToLocalInventoryAndPassCredentialProvider(t *testing.T) {
	plan, err := buildInitPlan(initPlanRequest{})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	if !strings.Contains(plan.Summary(), "Inventory sources: Local YAML inventory") {
		t.Fatalf("summary missing local inventory:\n%s", plan.Summary())
	}
	if !strings.Contains(plan.Summary(), "Credential providers: Pass") {
		t.Fatalf("summary missing Pass provider:\n%s", plan.Summary())
	}
	if !strings.Contains(plan.Summary(), "Credential assignment: local/default -> pass") {
		t.Fatalf("summary missing default assignment:\n%s", plan.Summary())
	}

	cfg := plan.Config
	if cfg.Credential.Provider["pass"].Type != config.CredentialProviderPass {
		t.Fatalf("pass provider = %+v", cfg.Credential.Provider["pass"])
	}
	if got := cfg.Inventory.Provider["local"].Group["default"].Auth; got.CredentialProvider != "pass" || got.PasswordRef != "nssh/groups/local/default" {
		t.Fatalf("default credential binding = %+v", got)
	}
}

func TestInitPlanWritesMultipleInventorySourcesWithoutSecrets(t *testing.T) {
	plan, err := buildInitPlan(initPlanRequest{
		InventorySources: []initInventorySourceRequest{
			{Type: initInventoryLocal, Groups: []string{"default"}},
			{
				Type:     config.ProviderNetBox,
				Name:     "netbox-prod",
				Groups:   []string{"prod"},
				BaseURL:  "https://netbox.example.com",
				TokenEnv: "NETBOX_TOKEN",
				Token:    "should-not-be-stored",
			},
			{
				Type:                  config.ProviderContainerlab,
				Name:                  "lab01",
				Groups:                []string{"lab"},
				JumpHost:              "jump01",
				Sudo:                  true,
				StrictHostKeyChecking: true,
			},
		},
		CredentialProviders: []initCredentialProviderRequest{
			{Name: "pass", Type: config.CredentialProviderPass},
		},
		GroupCredentialProviders: map[string]string{
			"local/default":    "pass",
			"netbox-prod/prod": "pass",
			"lab01/lab":        "pass",
		},
	})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	cfg := plan.Config
	if len(cfg.Inventory.Provider) != 3 {
		t.Fatalf("inventory provider count = %d, want 3: %+v", len(cfg.Inventory.Provider), cfg.Inventory.Provider)
	}
	if cfg.Inventory.Provider["netbox-prod"].Config.TokenEnv != "NETBOX_TOKEN" {
		t.Fatalf("netbox token_env = %q", cfg.Inventory.Provider["netbox-prod"].Config.TokenEnv)
	}
	if cfg.Inventory.Provider["lab01"].Config.JumpHost != "jump01" {
		t.Fatalf("containerlab jump_host = %q", cfg.Inventory.Provider["lab01"].Config.JumpHost)
	}

	yamlText := encodeInitPlanConfig(t, cfg)
	if strings.Contains(yamlText, "should-not-be-stored") {
		t.Fatalf("config stored NetBox token value:\n%s", yamlText)
	}
}

func TestInitPlanRejectsContainerlabWithoutJumpHost(t *testing.T) {
	_, err := buildInitPlan(initPlanRequest{
		InventorySources: []initInventorySourceRequest{{
			Type:   config.ProviderContainerlab,
			Name:   "lab01",
			Groups: []string{"lab"},
		}},
		CredentialProviders: []initCredentialProviderRequest{
			{Name: "pass", Type: config.CredentialProviderPass},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "jump_host") {
		t.Fatalf("error = %v, want jump_host validation", err)
	}
}

func TestInitPlanSupportsOnePasswordAndBitwardenWithoutAuthMaterial(t *testing.T) {
	plan, err := buildInitPlan(initPlanRequest{
		InventorySources: []initInventorySourceRequest{{Type: initInventoryLocal, Groups: []string{"default", "prod", "lab"}}},
		CredentialProviders: []initCredentialProviderRequest{
			{Name: "pass", Type: config.CredentialProviderPass},
			{Name: "op-network", Type: config.CredentialProvider1Password, Account: "ntwrknrd", Vault: "Network", Session: "agent", AuthToken: "should-not-be-stored"},
			{Name: "bw-lab", Type: config.CredentialProviderBitwarden, Session: "external", BWSession: "should-not-be-stored"},
		},
		GroupCredentialProviders: map[string]string{
			"local/default": "pass",
			"local/prod":    "op-network",
			"local/lab":     "bw-lab",
		},
	})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	cfg := plan.Config
	if cfg.Credential.Provider["op-network"].Config.Session != config.ProviderSessionAgentOwned {
		t.Fatalf("1Password session = %q", cfg.Credential.Provider["op-network"].Config.Session)
	}
	if cfg.Credential.Provider["bw-lab"].Config.Session != config.ProviderSessionExternal {
		t.Fatalf("Bitwarden session = %q", cfg.Credential.Provider["bw-lab"].Config.Session)
	}
	if cfg.Inventory.Provider["local"].Group["prod"].Auth.CredentialProvider != "op-network" || cfg.Inventory.Provider["local"].Group["lab"].Auth.CredentialProvider != "bw-lab" {
		t.Fatalf("group assignments = %+v", cfg.Inventory.Provider["local"].Group)
	}

	yamlText := encodeInitPlanConfig(t, cfg)
	for _, reject := range []string{"should-not-be-stored", "BW_SESSION", "auth_token"} {
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

	prompter := &fakeInitPrompter{inputs: map[string]string{"Pass provider name": "pass"}}
	if err := applyCredentialProviderSetup(paths, cfg, config.CredentialProviderPass, prompter, false); err != nil {
		t.Fatalf("applyCredentialProviderSetup: %v", err)
	}

	loaded, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := loaded.Credential.Provider["op-network"]; !ok {
		t.Fatalf("existing provider was lost: %+v", loaded.Credential.Provider)
	}
	if loaded.Credential.Provider["pass"].Type != config.CredentialProviderPass {
		t.Fatalf("pass provider = %+v", loaded.Credential.Provider["pass"])
	}
	if loaded.Inventory.Provider["local"].Group["default"].Auth.IsSet() {
		t.Fatalf("default group auth should not be assigned by provider setup: %+v", loaded.Inventory.Provider["local"].Group["default"].Auth)
	}
	if _, err := os.Stat(paths.ConfigFile + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
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
	for _, want := range []string{"Config file:", "nssh self init pass", "nssh self init 1password", "nssh self init bitwarden", "nssh inv set <host-or-group>"} {
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

	for _, provider := range []string{"pass", "1password", "bitwarden"} {
		if err := cmd.Args(cmd, []string{provider}); err != nil {
			t.Fatalf("init %s args error = %v", provider, err)
		}
	}
	if err := cmd.Args(cmd, []string{"vault"}); err == nil {
		t.Fatal("init accepted unsupported provider arg")
	}
}

func TestPromptInitPlanRequestShowsSourcesProvidersAndAssignments(t *testing.T) {
	prompter := &fakeInitPrompter{
		selects: map[string]string{
			"Credential providers":            config.CredentialProvider1Password,
			"Credential assignment: default":  "op-network",
			"Add another credential provider": "done",
		},
		inputs: map[string]string{
			"1Password provider name": "op-network",
			"1Password account":       "ntwrknrd",
			"1Password vault":         "Network",
		},
		confirms: map[string]bool{
			"Inventory sources: NetBox":       false,
			"Inventory sources: Containerlab": false,
		},
	}

	req, err := promptInitPlanRequest(prompter, nil)
	if err != nil {
		t.Fatalf("promptInitPlanRequest: %v", err)
	}

	for _, want := range []string{
		"Inventory sources: NetBox",
		"Inventory sources: Containerlab",
		"Credential providers",
		"Credential assignment: local/default",
	} {
		if !prompter.saw(want) {
			t.Fatalf("prompt flow did not show %q; prompts were %v", want, prompter.prompts)
		}
	}
	if req.CredentialProviders[0].Type != config.CredentialProvider1Password {
		t.Fatalf("credential providers = %+v", req.CredentialProviders)
	}
	if req.GroupCredentialProviders["local/default"] != "op-network" {
		t.Fatalf("group assignments = %+v", req.GroupCredentialProviders)
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

type fakeInitPrompter struct {
	selects  map[string]string
	inputs   map[string]string
	confirms map[string]bool
	prompts  []string
}

func (p *fakeInitPrompter) Select(title string, _ []ui.SelectOption) (string, error) {
	p.prompts = append(p.prompts, title)
	return p.selects[title], nil
}

func (p *fakeInitPrompter) Input(title, defaultValue string) (string, error) {
	p.prompts = append(p.prompts, title)
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

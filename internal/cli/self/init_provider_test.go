package self

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestInitYesDefaultsToLocalInventoryAndPassCredentialProvider(t *testing.T) {
	plan, err := buildInitPlan(initPlanRequest{Yes: true})
	if err != nil {
		t.Fatalf("buildInitPlan: %v", err)
	}

	if !strings.Contains(plan.Summary(), "Inventory sources: Local YAML inventory") {
		t.Fatalf("summary missing local inventory:\n%s", plan.Summary())
	}
	if !strings.Contains(plan.Summary(), "Credential providers: Pass") {
		t.Fatalf("summary missing Pass provider:\n%s", plan.Summary())
	}
	if !strings.Contains(plan.Summary(), "Credential assignment: local/default -> pass-local") {
		t.Fatalf("summary missing default assignment:\n%s", plan.Summary())
	}

	cfg := plan.Config
	if cfg.Credential.Provider["pass-local"].Type != config.CredentialProviderPass {
		t.Fatalf("pass-local provider = %+v", cfg.Credential.Provider["pass-local"])
	}
	if got := cfg.Inventory.Provider["local"].Group["default"].Auth; got.CredentialProvider != "pass-local" || got.PasswordRef != "nssh/groups/local/default" {
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
			{Name: "pass-local", Type: config.CredentialProviderPass},
		},
		GroupCredentialProviders: map[string]string{
			"local/default":    "pass-local",
			"netbox-prod/prod": "pass-local",
			"lab01/lab":        "pass-local",
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
			{Name: "pass-local", Type: config.CredentialProviderPass},
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
			{Name: "pass-local", Type: config.CredentialProviderPass},
			{Name: "op-network", Type: config.CredentialProvider1Password, Account: "ntwrknrd", Vault: "Network", Session: "agent", AuthToken: "should-not-be-stored"},
			{Name: "bw-lab", Type: config.CredentialProviderBitwarden, Session: "external", BWSession: "should-not-be-stored"},
		},
		GroupCredentialProviders: map[string]string{
			"local/default": "pass-local",
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

	plan, err := buildInitPlan(initPlanRequest{Yes: true})
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

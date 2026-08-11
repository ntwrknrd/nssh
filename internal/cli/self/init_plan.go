package self

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"gopkg.in/yaml.v3"
)

const (
	credentialIncludePattern = "credential/*.yaml"
	inventoryIncludePattern  = "inventory/*.yaml"
	defaultNetBoxURLEnv      = "NETBOX_URL"
	defaultNetBoxTokenEnv    = "NETBOX_TOKEN"
)

type initPlanRequest struct {
	Existing            *config.Config
	CredentialProviders []initCredentialProviderRequest
	InventoryProviders  []initInventoryProviderRequest
}

type initCredentialProviderRequest struct {
	Name       string
	Type       string
	Command    string
	Prefix     string
	Account    string
	Vault      string
	File       string
	AgeKeyFile string
	AuthToken  string
	BWSession  string
}

type initPlan struct {
	Config              *config.Config
	CredentialProviders []initCredentialProviderRequest
	InventoryProviders  []initInventoryProviderRequest
}

type initInventoryProviderRequest struct {
	Name                  string
	Type                  string
	BaseURL               string
	URLEnv                string
	TokenEnv              string
	EnvFile               string
	JumpHost              string
	Sudo                  bool
	StrictHostKeyChecking bool
}

type initPrompter interface {
	Select(title string, options []ui.SelectOption) (string, error)
	MultiSelect(title string, options []ui.SelectOption) ([]string, error)
	Input(title, defaultValue string) (string, error)
	Confirm(title string, defaultValue bool) (bool, error)
}

type uiInitPrompter struct{}

var runSelfInitCommand = runSelfInitCommandDefault

func (uiInitPrompter) Select(title string, options []ui.SelectOption) (string, error) {
	return ui.Select(title, options)
}

func (uiInitPrompter) MultiSelect(title string, options []ui.SelectOption) ([]string, error) {
	return ui.SelectMulti(title, options)
}

func (uiInitPrompter) Input(title, defaultValue string) (string, error) {
	return ui.InputWithDefaultSilent(title, defaultValue)
}

func (uiInitPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	return ui.Confirm(title, defaultValue)
}

func (p *initPlan) Summary() string {
	if p == nil || p.Config == nil {
		return ""
	}

	credentialSummary := initCredentialProviderRequestSummary(p.CredentialProviders)
	if len(credentialSummary) == 0 {
		credentialSummary = []string{"none"}
	}
	inventorySummary := initInventoryProviderRequestSummary(p.InventoryProviders)
	if len(inventorySummary) == 0 {
		inventorySummary = []string{"none"}
	}
	lines := []string{
		"Credential providers: " + strings.Join(credentialSummary, ", "),
		"Inventory providers: " + strings.Join(inventorySummary, ", "),
	}
	return strings.Join(lines, "\n")
}

func promptInitPlanRequest(prompter initPrompter, existing *config.Config) (initPlanRequest, error) {
	if prompter == nil {
		prompter = uiInitPrompter{}
	}
	req := initPlanRequest{Existing: existing}

	credentialTypes, err := prompter.MultiSelect("Credential providers", credentialProviderOptions())
	if err != nil {
		return req, err
	}
	for _, providerType := range credentialTypes {
		provider, err := promptCredentialProvider(prompter, providerType)
		if err != nil {
			return req, err
		}
		req.CredentialProviders = append(req.CredentialProviders, provider)
	}

	inventoryTypes, err := prompter.MultiSelect("Inventory providers", inventoryProviderOptions())
	if err != nil {
		return req, err
	}
	for _, providerType := range inventoryTypes {
		provider, err := promptInventoryProvider(prompter, providerType)
		if err != nil {
			return req, err
		}
		req.InventoryProviders = append(req.InventoryProviders, provider)
	}

	return req, nil
}

func buildInitPlan(req initPlanRequest) (*initPlan, error) {
	cfg := req.Existing
	if cfg == nil {
		cfg = config.DefaultConfig()
	} else {
		clone := *cfg
		cfg = &clone
	}

	cfg.Credential.Provider = make(map[string]config.CredentialProviderConfig)
	for i := range req.CredentialProviders {
		if err := applyCredentialProvider(cfg, req.CredentialProviders[i]); err != nil {
			return nil, err
		}
	}
	cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	cfg.Inventory.Providers = make(map[string]config.InventoryProviderConfig)
	for i := range req.InventoryProviders {
		if err := applyInventoryProvider(cfg, req.InventoryProviders[i]); err != nil {
			return nil, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &initPlan{
		Config:              cfg,
		CredentialProviders: append([]initCredentialProviderRequest(nil), req.CredentialProviders...),
		InventoryProviders:  append([]initInventoryProviderRequest(nil), req.InventoryProviders...),
	}, nil
}

func promptCredentialProvider(prompter initPrompter, providerType string) (initCredentialProviderRequest, error) {
	switch providerType {
	case config.CredentialProviderSOPSAge:
		return promptSOPSAgeProvider(prompter)
	case config.CredentialProvider1Password:
		name, err := prompter.Input("1Password provider name", "op-network")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		account, err := prompter.Input("1Password account user ID", "")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		vault, err := prompter.Input("1Password vault ID", "")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		return initCredentialProviderRequest{Name: name, Type: config.CredentialProvider1Password, Account: account, Vault: vault}, nil
	case config.CredentialProviderBitwarden:
		name, err := prompter.Input("Bitwarden provider name", "bw-local")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		return initCredentialProviderRequest{Name: name, Type: config.CredentialProviderBitwarden}, nil
	default:
		return initCredentialProviderRequest{}, fmt.Errorf("unsupported credential provider %q", providerType)
	}
}

func promptSOPSAgeProvider(prompter initPrompter) (initCredentialProviderRequest, error) {
	file, err := prompter.Input("SOPS file", "~/.local/share/nssh/credentials.sops.yaml")
	if err != nil {
		return initCredentialProviderRequest{}, err
	}
	ageKeyFile, err := prompter.Input("SOPS age key file", defaultSOPSAgeIdentityPromptPath())
	if err != nil {
		return initCredentialProviderRequest{}, err
	}
	return initCredentialProviderRequest{
		Name:       "sops",
		Type:       config.CredentialProviderSOPSAge,
		File:       file,
		AgeKeyFile: ageKeyFile,
	}, nil
}

func credentialProviderOptions() []ui.SelectOption {
	return []ui.SelectOption{
		{Label: "SOPS+age", Value: config.CredentialProviderSOPSAge},
		{Label: "1Password", Value: config.CredentialProvider1Password},
		{Label: "Bitwarden", Value: config.CredentialProviderBitwarden},
	}
}

func inventoryProviderOptions() []ui.SelectOption {
	return []ui.SelectOption{
		{Label: "Local", Value: config.ProviderLocal},
		{Label: "NetBox", Value: config.ProviderNetBox},
		{Label: "Containerlab", Value: config.ProviderContainerlab},
	}
}

func applyCredentialProvider(cfg *config.Config, provider initCredentialProviderRequest) error {
	if provider.Name == "" {
		return fmt.Errorf("credential provider name is required")
	}
	detail := config.CredentialProviderDetailConfig{
		Command:    provider.Command,
		Prefix:     provider.Prefix,
		Account:    provider.Account,
		Vault:      provider.Vault,
		File:       provider.File,
		AgeKeyFile: provider.AgeKeyFile,
	}
	cfg.Credential.Provider[provider.Name] = config.CredentialProviderConfig{
		Type:       provider.Type,
		File:       provider.File,
		AgeKeyFile: provider.AgeKeyFile,
		Config:     detail,
	}
	return nil
}

func promptInventoryProvider(prompter initPrompter, providerType string) (initInventoryProviderRequest, error) {
	switch providerType {
	case config.ProviderLocal:
		return initInventoryProviderRequest{Name: config.ProviderLocal, Type: config.ProviderLocal}, nil
	case config.ProviderNetBox:
		name, err := prompter.Input("NetBox provider name", "netbox-prod")
		if err != nil {
			return initInventoryProviderRequest{}, err
		}
		urlEnv, err := prompter.Input("NetBox URL environment variable", defaultNetBoxURLEnv)
		if err != nil {
			return initInventoryProviderRequest{}, err
		}
		tokenEnv, err := prompter.Input("NetBox token environment variable", defaultNetBoxTokenEnv)
		if err != nil {
			return initInventoryProviderRequest{}, err
		}
		return initInventoryProviderRequest{
			Name:     name,
			Type:     config.ProviderNetBox,
			URLEnv:   urlEnv,
			TokenEnv: tokenEnv,
		}, nil
	case config.ProviderContainerlab:
		name, err := prompter.Input("Containerlab provider name", "containerlab")
		if err != nil {
			return initInventoryProviderRequest{}, err
		}
		jumpHost, err := prompter.Input("Containerlab jump host", "")
		if err != nil {
			return initInventoryProviderRequest{}, err
		}
		if strings.TrimSpace(jumpHost) == "" {
			return initInventoryProviderRequest{}, fmt.Errorf("containerlab inventory provider requires jump_host")
		}
		return initInventoryProviderRequest{Name: name, Type: config.ProviderContainerlab, JumpHost: jumpHost}, nil
	default:
		return initInventoryProviderRequest{}, fmt.Errorf("unsupported inventory provider %q", providerType)
	}
}

func applyInventoryProvider(cfg *config.Config, provider initInventoryProviderRequest) error {
	if provider.Name == "" {
		return fmt.Errorf("inventory provider name is required")
	}
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	if cfg.Inventory.Providers == nil {
		cfg.Inventory.Providers = make(map[string]config.InventoryProviderConfig)
	}
	detail := config.InventoryProviderDetailConfig{
		BaseURL:               provider.BaseURL,
		URLEnv:                provider.URLEnv,
		TokenEnv:              provider.TokenEnv,
		EnvFile:               provider.EnvFile,
		JumpHost:              provider.JumpHost,
		Sudo:                  provider.Sudo,
		StrictHostKeyChecking: provider.StrictHostKeyChecking,
	}
	cfgProvider := config.InventoryProviderConfig{
		Type:   provider.Type,
		Config: detail,
	}
	if provider.Type == config.ProviderLocal {
		cfgProvider.Group = map[string]config.GroupConfig{"default": {}}
		cfgProvider.Groups = map[string]config.GroupConfig{"default": {}}
	}
	cfg.Inventory.Provider[provider.Name] = cfgProvider
	cfg.Inventory.Providers[provider.Name] = cfgProvider
	return nil
}

func applyCredentialProviderSetup(paths *config.Paths, cfg *config.Config, providerType string, prompter initPrompter, dryRun bool) error {
	if paths == nil {
		return fmt.Errorf("paths are required")
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if provider, providerPath, ok := existingCredentialProviderSetup(cfg, providerType); ok {
		printExistingCredentialProviderSetup(provider, providerPath)
		return nil
	}
	if prompter == nil {
		prompter = uiInitPrompter{}
	}
	provider, err := explicitCredentialProviderRequest(prompter, providerType)
	if err != nil {
		return err
	}
	if err := prepareCredentialProviderSetup(&provider, prompter, dryRun); err != nil {
		return err
	}
	if cfg.Credential.Provider == nil {
		cfg.Credential.Provider = make(map[string]config.CredentialProviderConfig)
	}
	if err := applyCredentialProvider(cfg, provider); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	if dryRun {
		printCredentialProviderSetup(provider, credentialProviderSetupPath(paths, provider), dryRun)
		return nil
	}
	providerPath, err := saveCredentialProviderSetup(paths, provider)
	if err != nil {
		return err
	}
	printCredentialProviderSetup(provider, providerPath, dryRun)
	ui.Success("Config file: %s", AbbreviatePath(paths.ConfigFile))
	return nil
}

func applyInventoryProviderSetup(paths *config.Paths, cfg *config.Config, providerType string, prompter initPrompter, dryRun bool) error {
	if paths == nil {
		return fmt.Errorf("paths are required")
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if provider, providerPath, ok := existingInventoryProviderSetup(cfg, providerType); ok {
		printExistingInventoryProviderSetup(provider, providerPath)
		return nil
	}
	if prompter == nil {
		prompter = uiInitPrompter{}
	}
	provider, err := promptInventoryProvider(prompter, providerType)
	if err != nil {
		return err
	}
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	if cfg.Inventory.Providers == nil {
		cfg.Inventory.Providers = make(map[string]config.InventoryProviderConfig)
	}
	if err := applyInventoryProvider(cfg, provider); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	if dryRun {
		printInventoryProviderSetup(inventoryProviderSetupPath(paths, provider), dryRun)
		return nil
	}
	providerPath, err := saveInventoryProviderSetup(paths, provider)
	if err != nil {
		return err
	}
	printInventoryProviderSetup(providerPath, dryRun)
	ui.Success("Config file: %s", AbbreviatePath(paths.ConfigFile))
	return nil
}

func applyProviderSetups(paths *config.Paths, cfg *config.Config, opts InitOptions, prompter initPrompter) error {
	for _, providerType := range opts.CredentialProviderTypes {
		if err := applyCredentialProviderSetup(paths, cfg, providerType, prompter, opts.DryRun); err != nil {
			return err
		}
		if !opts.DryRun {
			loaded, err := config.Load(paths.ConfigFile)
			if err != nil {
				return err
			}
			cfg = loaded
		}
	}
	for _, providerType := range opts.InventoryProviderTypes {
		if err := applyInventoryProviderSetup(paths, cfg, providerType, prompter, opts.DryRun); err != nil {
			return err
		}
		if !opts.DryRun {
			loaded, err := config.Load(paths.ConfigFile)
			if err != nil {
				return err
			}
			cfg = loaded
		}
	}
	return nil
}

func existingCredentialProviderSetup(cfg *config.Config, providerType string) (initCredentialProviderRequest, string, bool) {
	if cfg == nil {
		return initCredentialProviderRequest{}, "", false
	}
	for _, name := range sortedCredentialProviderNames(cfg.Credential.Provider) {
		provider := cfg.Credential.Provider[name]
		if provider.Type != providerType {
			continue
		}
		source := cfg.CredentialProviderSource(name)
		if strings.TrimSpace(source) == "" {
			continue
		}
		if info, err := os.Stat(source); err != nil || info.IsDir() {
			continue
		}
		return credentialProviderRequestFromConfig(name, provider), source, true
	}
	return initCredentialProviderRequest{}, "", false
}

func existingInventoryProviderSetup(cfg *config.Config, providerType string) (initInventoryProviderRequest, string, bool) {
	if cfg == nil {
		return initInventoryProviderRequest{}, "", false
	}
	for _, name := range sortedInventoryProviderNames(cfg.Inventory.Provider) {
		provider := cfg.Inventory.Provider[name]
		if provider.Type != providerType {
			continue
		}
		source := cfg.InventoryProviderSource(name)
		if strings.TrimSpace(source) == "" {
			continue
		}
		if info, err := os.Stat(source); err != nil || info.IsDir() {
			continue
		}
		return inventoryProviderRequestFromConfig(name, provider), source, true
	}
	return initInventoryProviderRequest{}, "", false
}

func sortedCredentialProviderNames(providers map[string]config.CredentialProviderConfig) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedInventoryProviderNames(providers map[string]config.InventoryProviderConfig) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func printExistingCredentialProviderSetup(provider initCredentialProviderRequest, providerPath string) {
	ui.Warning("Credential provider already configured, skipping init: %s", credentialProviderSetupLabel(provider))
	ui.Info("Provider config: %s", AbbreviatePath(providerPath))
}

func printExistingInventoryProviderSetup(provider initInventoryProviderRequest, providerPath string) {
	ui.Warning("Inventory provider already configured, skipping init: %s", inventoryProviderSetupLabel(provider))
	ui.Info("Provider config: %s", AbbreviatePath(providerPath))
}

func printCredentialProviderSetup(provider initCredentialProviderRequest, providerPath string, dryRun bool) {
	ui.SubSection("Credential Provider")
	if ok, detail := credentialProviderBinaryStatus(provider); ok {
		ui.Success("Provider binary: %s", detail)
	} else {
		ui.Warning("Provider binary: %s", detail)
	}
	if dryRun {
		ui.StatusLineNeutral("Provider config", AbbreviatePath(providerPath)+" (dry run)")
	} else {
		ui.Success("Provider config: %s", AbbreviatePath(providerPath))
	}
	if provider.Type == config.CredentialProviderSOPSAge {
		ui.Success("SOPS file: %s", provider.File)
		if strings.TrimSpace(provider.AgeKeyFile) != "" {
			ui.Success("Age key file: %s", provider.AgeKeyFile)
		}
	}
}

func printInventoryProviderSetup(providerPath string, dryRun bool) {
	ui.SubSection("Inventory Provider")
	if dryRun {
		ui.StatusLineNeutral("Provider config", AbbreviatePath(providerPath)+" (dry run)")
	} else {
		ui.Success("Provider config: %s", AbbreviatePath(providerPath))
	}
}

func credentialProviderBinaryStatus(provider initCredentialProviderRequest) (bool, string) {
	cfg := config.CredentialProviderConfig{
		Type:    provider.Type,
		Command: provider.Command,
		Config: config.CredentialProviderDetailConfig{
			Command: provider.Command,
		},
	}
	command := credentialProviderCommand(cfg)
	if command == "" {
		return false, "unsupported provider type"
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return false, "missing " + command
	}
	return true, "ready (" + AbbreviatePath(path) + ")"
}

func saveCredentialProviderSetup(paths *config.Paths, provider initCredentialProviderRequest) (string, error) {
	providerPath, err := writeCredentialProviderConfig(paths, provider)
	if err != nil {
		return "", err
	}
	if err := ensureRootInclude(paths.ConfigFile, credentialIncludePattern); err != nil {
		return "", err
	}
	return providerPath, nil
}

func saveInventoryProviderSetup(paths *config.Paths, provider initInventoryProviderRequest) (string, error) {
	providerPath, err := writeInventoryProviderConfig(paths, provider)
	if err != nil {
		return "", err
	}
	if err := ensureRootInclude(paths.ConfigFile, inventoryIncludePattern); err != nil {
		return "", err
	}
	return providerPath, nil
}

func writeCredentialProviderConfig(paths *config.Paths, provider initCredentialProviderRequest) (string, error) {
	providerPath := credentialProviderSetupPath(paths, provider)
	if err := os.MkdirAll(filepath.Dir(providerPath), 0700); err != nil {
		return "", fmt.Errorf("create credential config dir: %w", err)
	}
	text, err := credentialProviderConfigText(provider)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(providerPath, []byte(text), 0600); err != nil {
		return "", fmt.Errorf("write credential provider config: %w", err)
	}
	return providerPath, nil
}

func writeInventoryProviderConfig(paths *config.Paths, provider initInventoryProviderRequest) (string, error) {
	providerPath := inventoryProviderSetupPath(paths, provider)
	if err := os.MkdirAll(filepath.Dir(providerPath), 0700); err != nil {
		return "", fmt.Errorf("create inventory config dir: %w", err)
	}
	text, err := inventoryProviderConfigText(provider)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(providerPath, []byte(text), 0600); err != nil {
		return "", fmt.Errorf("write inventory provider config: %w", err)
	}
	return providerPath, nil
}

func credentialProviderSetupPath(paths *config.Paths, provider initCredentialProviderRequest) string {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = strings.TrimSpace(provider.Type)
	}
	return filepath.Join(paths.ConfigDir, "credential", name+".yaml")
}

func inventoryProviderSetupPath(paths *config.Paths, provider initInventoryProviderRequest) string {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = strings.TrimSpace(provider.Type)
	}
	return filepath.Join(paths.ConfigDir, "inventory", name+".yaml")
}

func credentialProviderConfigText(provider initCredentialProviderRequest) (string, error) {
	providerTable := make(map[string]any)
	addProviderString(providerTable, "type", provider.Type)
	addProviderString(providerTable, "account", provider.Account)
	addProviderString(providerTable, "vault", provider.Vault)
	addProviderString(providerTable, "command", provider.Command)
	addProviderString(providerTable, "prefix", provider.Prefix)
	addProviderString(providerTable, "file", provider.File)
	addProviderString(providerTable, "age_key_file", provider.AgeKeyFile)
	table := map[string]any{
		"credential": map[string]any{
			"provider": map[string]any{
				provider.Name: providerTable,
			},
		},
	}
	text, err := marshalInitYAML(table)
	if err != nil {
		return "", fmt.Errorf("encode credential provider config: %w", err)
	}
	return addCredentialProviderComments(text, provider), nil
}

func inventoryProviderConfigText(provider initInventoryProviderRequest) (string, error) {
	providerTable := make(map[string]any)
	addProviderString(providerTable, "type", provider.Type)
	configTable := make(map[string]any)
	addProviderString(configTable, "base_url", provider.BaseURL)
	addProviderString(configTable, "url_env", provider.URLEnv)
	addProviderString(configTable, "token_env", provider.TokenEnv)
	addProviderString(configTable, "env_file", provider.EnvFile)
	addProviderString(configTable, "jump_host", provider.JumpHost)
	if provider.Sudo {
		configTable["sudo"] = true
	}
	if provider.StrictHostKeyChecking {
		configTable["strict_host_key_checking"] = true
	}
	if len(configTable) > 0 {
		providerTable["config"] = configTable
	}
	if provider.Type == config.ProviderLocal {
		providerTable["groups"] = map[string]any{"default": map[string]any{}}
	}
	table := map[string]any{
		"inventory": map[string]any{
			"providers": map[string]any{
				provider.Name: providerTable,
			},
		},
	}
	text, err := marshalInitYAML(table)
	if err != nil {
		return "", fmt.Errorf("encode inventory provider config: %w", err)
	}
	return addInventoryProviderComments(text, provider), nil
}

func addInventoryProviderComments(text string, provider initInventoryProviderRequest) string {
	var comments []string
	switch provider.Type {
	case config.ProviderLocal:
		comments = []string{
			"# groups:",
			"#   corp:",
			"#     auth:",
			"#       mode: password",
			"#       credential_provider: op-work",
			"#       password_ref: op://Work/network-admin/password",
			"#       username: netops",
			"#     match:",
			"#       domain_suffix:",
			"#         - .example.net",
			"#     ssh:",
			"#       options:",
			"#         IdentityFile:",
			"#           - ~/.ssh/work-ssh-key.pub",
			"# hosts:",
			"#   edge01.example.net:",
			"#     group: corp",
			"#     aliases:",
			"#       - edge01",
			"#     auth:",
			"#       mode: key",
			"#       username: netops",
		}
	case config.ProviderNetBox:
		comments = []string{
			"# config:",
			"#   base_url: https://netbox.example.com",
			"#   url_env: NETBOX_URL",
			"#   env_file: ~/.env",
			"# groups:",
			"#   corp:",
			"#     auth:",
			"#       mode: password",
			"#       credential_provider: op-work",
			"#       password_ref: op://Work/network-admin/password",
			"#       username: netops",
			"#     match:",
			"#       domain_suffix:",
			"#         - .example.net",
			"#       manufacturer:",
			"#         - Juniper",
			"#         - Arista",
			"#       status:",
			"#         - active",
			"#       tenant:",
			"#         - ExampleCorp",
			"#     ssh:",
			"#       options:",
			"#         WarnWeakCrypto: no-pq-kex",
			"#         IdentityFile:",
			"#           - ~/.ssh/work-ssh-key.pub",
			"# hosts:",
			"#   legacy-switch.example.net:",
			"#     aliases:",
			"#       - legacy-switch",
			"#     group: corp",
			"#     ssh:",
			"#       compatibility:",
			"#         kex: diffie-hellman-group14-sha1",
			"#         mac: hmac-sha1",
		}
	case config.ProviderContainerlab:
		comments = []string{
			"# config:",
			"#   sudo: true",
			"#   strict_host_key_checking: true",
			"# groups:",
			"#   ceos:",
			"#     auth:",
			"#       mode: password",
			"#       credential_provider: op-work",
			"#       username_ref: op://Work/containerlab-ceos/username",
			"#       password_ref: op://Work/containerlab-ceos/password",
			"#     match:",
			"#       kind:",
			"#         - ceos",
			"#         - arista_ceos",
			"#       state:",
			"#         - running",
			"#   vjunos:",
			"#     auth:",
			"#       mode: password",
			"#       credential_provider: op-work",
			"#       username_ref: op://Work/containerlab-vjunos/username",
			"#       password_ref: op://Work/containerlab-vjunos/password",
			"#     match:",
			"#       kind:",
			"#         - vjunos",
			"#         - juniper_vjunosrouter",
			"#       state:",
			"#         - running",
			"#   linux:",
			"#     auth:",
			"#       mode: password",
			"#       credential_provider: op-work",
			"#       username_ref: op://Work/containerlab-linux/username",
			"#       password_ref: op://Work/containerlab-linux/password",
			"#     match:",
			"#       kind:",
			"#         - linux",
			"#       state:",
			"#         - running",
		}
	}
	if len(comments) == 0 {
		return text
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, comment := range comments {
		lines = append(lines, "      "+comment)
	}
	return strings.Join(lines, "\n") + "\n"
}

func addProviderString(table map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		table[key] = value
	}
}

func addCredentialProviderComments(text string, provider initCredentialProviderRequest) string {
	var comments []string
	switch provider.Type {
	case config.CredentialProviderSOPSAge:
		comments = []string{
			"# command: sops",
			"# prefix: optional/ref/prefix",
			"#",
			"# Example inventory auth ref:",
			"# credential_provider: sops",
			"# password_ref: groups.corp.password",
		}
	case config.CredentialProvider1Password:
		comments = []string{
			"# account: account-user-id",
			"# vault: vault-id",
			"# keepalive: true",
			"# keepalive_interval: 5m",
			"# keepalive_timeout: 10s",
			"#",
			"# Example inventory auth refs:",
			"# credential_provider: " + provider.Name,
			"# username_ref: op://Work/network-admin/username",
			"# password_ref: op://Work/network-admin/password",
		}
	case config.CredentialProviderBitwarden:
		comments = []string{
			"# command: bw",
			"# warm_session: true",
			"#",
			"# Example inventory auth ref:",
			"# credential_provider: " + provider.Name,
			"# password_ref: item-id-or-name",
		}
	}
	if len(comments) == 0 {
		return text
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, comment := range comments {
		lines = append(lines, "      "+comment)
	}
	return strings.Join(lines, "\n") + "\n"
}

func ensureRootInclude(rootPath, pattern string) error {
	root := make(map[string]any)
	var existing []byte
	if data, err := os.ReadFile(rootPath); err == nil {
		existing = data
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse root config: %w", err)
		}
		if root == nil {
			root = make(map[string]any)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read root config: %w", err)
	}
	if includeContains(root["include"], pattern) {
		return nil
	}
	text := addIncludePatternToRootText(string(existing), root["include"], pattern)
	if err := os.MkdirAll(filepath.Dir(rootPath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if existing != nil {
		if err := os.WriteFile(rootPath+".bak", existing, 0600); err != nil {
			return fmt.Errorf("write config backup: %w", err)
		}
	}
	if err := os.WriteFile(rootPath, []byte(text), 0600); err != nil {
		return fmt.Errorf("write root config: %w", err)
	}
	return nil
}

func marshalInitYAML(table map[string]any) (string, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(table); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func includeContains(value any, pattern string) bool {
	for _, include := range includeStringValues(value) {
		if include == pattern {
			return true
		}
	}
	return false
}

func includeWithPattern(value any, pattern string) []string {
	out := []string{pattern}
	seen := map[string]bool{pattern: true}
	for _, include := range includeStringValues(value) {
		if seen[include] {
			continue
		}
		seen[include] = true
		out = append(out, include)
	}
	return out
}

func addIncludePatternToRootText(text string, existingInclude any, pattern string) string {
	includes := includeWithPattern(existingInclude, pattern)
	block := includeBlock(includes)
	trimmedText := strings.TrimLeft(text, "\n")
	if strings.TrimSpace(trimmedText) == "" {
		return block
	}
	lines := strings.SplitAfter(trimmedText, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "include:") {
			continue
		}
		end := i + 1
		for end < len(lines) {
			next := lines[end]
			if strings.TrimSpace(next) == "" || strings.HasPrefix(next, " ") || strings.HasPrefix(next, "\t") {
				end++
				continue
			}
			break
		}
		out := strings.Join(lines[:i], "") + block + strings.Join(lines[end:], "")
		return out
	}
	return block + trimmedText
}

func includeBlock(includes []string) string {
	var b strings.Builder
	b.WriteString("include:\n")
	for _, include := range includes {
		b.WriteString("  - ")
		b.WriteString(include)
		b.WriteString("\n")
	}
	return b.String()
}

func includeStringValues(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return typed
	case string:
		if strings.TrimSpace(typed) != "" {
			return []string{typed}
		}
	}
	return nil
}

func prepareCredentialProviders(providers []initCredentialProviderRequest, prompter initPrompter, dryRun bool) error {
	for i := range providers {
		if err := prepareCredentialProviderSetup(&providers[i], prompter, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func explicitInitPlanRequest(prompter initPrompter, opts InitOptions) (initPlanRequest, error) {
	req := initPlanRequest{}
	for _, providerType := range opts.CredentialProviderTypes {
		provider, err := explicitCredentialProviderRequest(prompter, providerType)
		if err != nil {
			return req, err
		}
		req.CredentialProviders = append(req.CredentialProviders, provider)
	}
	for _, providerType := range opts.InventoryProviderTypes {
		provider, err := promptInventoryProvider(prompter, providerType)
		if err != nil {
			return req, err
		}
		req.InventoryProviders = append(req.InventoryProviders, provider)
	}
	return req, nil
}

func prepareCredentialProviderSetup(provider *initCredentialProviderRequest, prompter initPrompter, dryRun bool) error {
	if provider == nil || provider.Type != config.CredentialProviderSOPSAge {
		return nil
	}
	if prompter == nil {
		prompter = uiInitPrompter{}
	}
	identityPath := strings.TrimSpace(provider.AgeKeyFile)
	if identityPath == "" {
		identityPath = defaultSOPSAgeIdentityPath()
	}
	expandedIdentity := expandSelfPath(identityPath)
	if missingFile(expandedIdentity) {
		ok, err := prompter.Confirm("Create SOPS age key at "+AbbreviatePath(identityPath)+"?", true)
		if err != nil {
			return err
		}
		if ok && !dryRun {
			if err := os.MkdirAll(filepath.Dir(expandedIdentity), 0700); err != nil {
				return fmt.Errorf("create age key dir: %w", err)
			}
			if out, err := runSelfInitCommand("age-keygen", nil, "-o", expandedIdentity); err != nil {
				return fmt.Errorf("age-keygen -o %s: %w: %s", AbbreviatePath(identityPath), err, strings.TrimSpace(string(out)))
			}
		}
	}
	identityReady := !missingFile(expandedIdentity)

	file := strings.TrimSpace(provider.File)
	if file == "" {
		file = "~/.local/share/nssh/credentials.sops.yaml"
		provider.File = file
	}
	expandedFile := expandSelfPath(file)
	if missingFile(expandedFile) && identityReady {
		ok, err := prompter.Confirm("Create starter SOPS file at "+AbbreviatePath(file)+"?", true)
		if err != nil {
			return err
		}
		if ok && !dryRun {
			recipient, err := readAgeRecipient(expandedIdentity)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(expandedFile), 0700); err != nil {
				return fmt.Errorf("create SOPS file dir: %w", err)
			}
			starter := []byte("groups:\n  default:\n    username: \"\"\n    password: \"\"\n")
			args := []string{"--encrypt", "--age", recipient, "--input-type", "yaml", "--output-type", "yaml", "--output", expandedFile, "/dev/stdin"}
			if out, err := runSelfInitCommand("sops", starter, args...); err != nil {
				return fmt.Errorf("create starter SOPS file %s: %w: %s", AbbreviatePath(file), err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

func missingFile(path string) bool {
	info, err := os.Stat(path)
	return err != nil || info.IsDir()
}

func defaultSOPSAgeIdentityPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "sops", "age", "keys.txt")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(homeDir(), "Library", "Application Support", "sops", "age", "keys.txt")
	}
	return filepath.Join(homeDir(), ".config", "sops", "age", "keys.txt")
}

func defaultSOPSAgeIdentityPromptPath() string {
	if env := strings.TrimSpace(os.Getenv("SOPS_AGE_KEY_FILE")); env != "" {
		return env
	}
	candidates := []string{}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "sops", "age", "keys.txt"))
	}
	candidates = append(candidates, filepath.Join(homeDir(), ".config", "sops", "age", "keys.txt"))
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, filepath.Join(homeDir(), "Library", "Application Support", "sops", "age", "keys.txt"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return abbreviateHome(candidate)
		}
	}
	return defaultSOPSAgeIdentityPath()
}

func abbreviateHome(path string) string {
	home := homeDir()
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~/" + strings.TrimPrefix(path, home+string(os.PathSeparator))
	}
	return path
}

func readAgeRecipient(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read age identity: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# public key: ") {
			recipient := strings.TrimSpace(strings.TrimPrefix(line, "# public key: "))
			if recipient != "" {
				return recipient, nil
			}
		}
	}
	return "", fmt.Errorf("age identity %s does not contain a public key comment", AbbreviatePath(path))
}

func runSelfInitCommandDefault(name string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func explicitCredentialProviderRequest(prompter initPrompter, providerType string) (initCredentialProviderRequest, error) {
	if providerType == config.CredentialProviderSOPSAge {
		return promptSOPSAgeProvider(prompter)
	}
	return promptCredentialProvider(prompter, providerType)
}

func credentialProviderSetupLabel(provider initCredentialProviderRequest) string {
	label := credentialProviderLabel(provider.Type)
	if strings.EqualFold(provider.Name, label) {
		return provider.Name
	}
	return fmt.Sprintf("%s (%s)", provider.Name, label)
}

func inventoryProviderSetupLabel(provider initInventoryProviderRequest) string {
	label := inventoryProviderLabel(provider.Type)
	if strings.EqualFold(provider.Name, label) {
		return provider.Name
	}
	return fmt.Sprintf("%s (%s)", provider.Name, label)
}

func initCredentialProviderRequestSummary(providers []initCredentialProviderRequest) []string {
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		out = append(out, credentialProviderLabel(provider.Type))
	}
	sort.Strings(out)
	return out
}

func initInventoryProviderRequestSummary(providers []initInventoryProviderRequest) []string {
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		out = append(out, inventoryProviderLabel(provider.Type))
	}
	sort.Strings(out)
	return out
}

func credentialProviderLabel(providerType string) string {
	switch providerType {
	case config.CredentialProviderSOPSAge:
		return "SOPS+age"
	case config.CredentialProvider1Password:
		return "1Password"
	case config.CredentialProviderBitwarden:
		return "Bitwarden"
	default:
		return providerType
	}
}

func inventoryProviderLabel(providerType string) string {
	switch providerType {
	case config.ProviderLocal:
		return "Local"
	case config.ProviderNetBox:
		return "NetBox"
	case config.ProviderContainerlab:
		return "Containerlab"
	default:
		return providerType
	}
}

func applyInitPlan(paths *config.Paths, plan *initPlan) error {
	if paths == nil {
		return fmt.Errorf("paths are required")
	}
	if plan == nil || plan.Config == nil {
		return fmt.Errorf("init plan config is required")
	}
	includes := []string{}
	if len(plan.CredentialProviders) > 0 {
		if err := writeCredentialProviderRequestConfigs(paths, plan.CredentialProviders); err != nil {
			return err
		}
		includes = append(includes, credentialIncludePattern)
	}
	if len(plan.InventoryProviders) > 0 {
		if err := writeInventoryProviderRequestConfigs(paths, plan.InventoryProviders); err != nil {
			return err
		}
		includes = append(includes, inventoryIncludePattern)
	}
	return saveInitConfigTemplate(paths, includes)
}

func writeCredentialProviderRequestConfigs(paths *config.Paths, providers []initCredentialProviderRequest) error {
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	for _, provider := range providers {
		if _, err := writeCredentialProviderConfig(paths, provider); err != nil {
			return err
		}
	}
	return nil
}

func writeInventoryProviderRequestConfigs(paths *config.Paths, providers []initInventoryProviderRequest) error {
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	for _, provider := range providers {
		if _, err := writeInventoryProviderConfig(paths, provider); err != nil {
			return err
		}
	}
	return nil
}

func inventoryProviderRequestFromConfig(name string, provider config.InventoryProviderConfig) initInventoryProviderRequest {
	return initInventoryProviderRequest{
		Name:                  name,
		Type:                  provider.Type,
		BaseURL:               provider.Config.BaseURL,
		URLEnv:                provider.Config.URLEnv,
		TokenEnv:              provider.Config.TokenEnv,
		EnvFile:               provider.Config.EnvFile,
		JumpHost:              provider.Config.JumpHost,
		Sudo:                  provider.Config.Sudo,
		StrictHostKeyChecking: provider.Config.StrictHostKeyChecking,
	}
}

func credentialProviderRequestFromConfig(name string, provider config.CredentialProviderConfig) initCredentialProviderRequest {
	command := firstNonEmpty(provider.Command, provider.Config.Command)
	prefix := firstNonEmpty(provider.Prefix, provider.Config.Prefix)
	account := firstNonEmpty(provider.Account, provider.Config.Account)
	vault := firstNonEmpty(provider.Vault, provider.Config.Vault)
	file := firstNonEmpty(provider.File, provider.Config.File)
	ageKeyFile := firstNonEmpty(provider.AgeKeyFile, provider.Config.AgeKeyFile)
	return initCredentialProviderRequest{
		Name:       name,
		Type:       provider.Type,
		Command:    command,
		Prefix:     prefix,
		Account:    account,
		Vault:      vault,
		File:       file,
		AgeKeyFile: ageKeyFile,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func saveInitConfigTemplate(paths *config.Paths, includes []string) error {
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if _, err := os.Stat(paths.ConfigFile); err == nil {
		backup := paths.ConfigFile + ".bak"
		data, readErr := os.ReadFile(paths.ConfigFile)
		if readErr != nil {
			return fmt.Errorf("read existing config: %w", readErr)
		}
		if writeErr := os.WriteFile(backup, data, 0600); writeErr != nil {
			return fmt.Errorf("write config backup: %w", writeErr)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat config: %w", err)
	}
	text := rootTemplateWithIncludes(config.ExampleConfig, includes)
	if err := os.WriteFile(paths.ConfigFile, []byte(text), 0600); err != nil {
		return fmt.Errorf("write root config: %w", err)
	}
	return os.Chmod(filepath.Dir(paths.ConfigFile), 0700)
}

func rootTemplateWithIncludes(template string, includes []string) string {
	text := strings.TrimRight(template, "\n") + "\n"
	if len(includes) > 0 {
		text = strings.ReplaceAll(text, "# include:", "include:")
	}
	for _, include := range includes {
		text = uncommentTemplateInclude(text, include)
	}
	return text
}

func uncommentTemplateInclude(text, include string) string {
	return strings.ReplaceAll(text, "#   - "+include, "  - "+include)
}

package self

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
)

const initInventoryLocal = "local"

type initPlanRequest struct {
	Yes                      bool
	Existing                 *config.Config
	InventorySources         []initInventorySourceRequest
	CredentialProviders      []initCredentialProviderRequest
	GroupCredentialProviders map[string]string
	HostCredentialProviders  map[string]string
}

type initInventorySourceRequest struct {
	Type                  string
	Name                  string
	Groups                []string
	BaseURL               string
	URLEnv                string
	TokenEnv              string
	Token                 string
	EnvFile               string
	JumpHost              string
	Sudo                  bool
	StrictHostKeyChecking bool
}

type initCredentialProviderRequest struct {
	Name      string
	Type      string
	Command   string
	Prefix    string
	Account   string
	Vault     string
	Session   string
	AuthToken string
	BWSession string
}

type initPlan struct {
	Config *config.Config
}

type initPrompter interface {
	Select(title string, options []ui.SelectOption) (string, error)
	Input(title, defaultValue string) (string, error)
	Confirm(title string, defaultValue bool) (bool, error)
}

type uiInitPrompter struct{}

func (uiInitPrompter) Select(title string, options []ui.SelectOption) (string, error) {
	return ui.Select(title, options)
}

func (uiInitPrompter) Input(title, defaultValue string) (string, error) {
	return ui.InputWithDefault(title, defaultValue)
}

func (uiInitPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	return ui.Confirm(title, defaultValue)
}

func (p *initPlan) Summary() string {
	if p == nil || p.Config == nil {
		return ""
	}

	lines := []string{
		"Inventory sources: " + strings.Join(initInventorySummary(p.Config), ", "),
		"Credential providers: " + strings.Join(initCredentialProviderSummary(p.Config), ", "),
		"Credential assignment: " + strings.Join(initCredentialAssignmentSummary(p.Config), ", "),
	}
	return strings.Join(lines, "\n")
}

func promptInitPlanRequest(prompter initPrompter, existing *config.Config) (initPlanRequest, error) {
	if prompter == nil {
		prompter = uiInitPrompter{}
	}
	req := initPlanRequest{
		Existing:                 existing,
		InventorySources:         []initInventorySourceRequest{{Type: initInventoryLocal, Groups: []string{"default"}}},
		GroupCredentialProviders: make(map[string]string),
	}

	if yes, err := prompter.Confirm("Inventory sources: NetBox", false); err != nil {
		return req, err
	} else if yes {
		source, err := promptNetBoxSource(prompter)
		if err != nil {
			return req, err
		}
		req.InventorySources = append(req.InventorySources, source)
	}
	if yes, err := prompter.Confirm("Inventory sources: Containerlab", false); err != nil {
		return req, err
	} else if yes {
		source, err := promptContainerlabSource(prompter)
		if err != nil {
			return req, err
		}
		req.InventorySources = append(req.InventorySources, source)
	}

	first, err := prompter.Select("Credential providers", credentialProviderOptions())
	if err != nil {
		return req, err
	}
	if first == "" {
		first = config.CredentialProviderPass
	}
	provider, err := promptCredentialProvider(prompter, first)
	if err != nil {
		return req, err
	}
	req.CredentialProviders = append(req.CredentialProviders, provider)
	for {
		next, err := prompter.Select("Add another credential provider", append([]ui.SelectOption{{Label: "Done", Value: "done"}}, credentialProviderOptions()...))
		if err != nil {
			return req, err
		}
		if next == "" || next == "done" {
			break
		}
		provider, err := promptCredentialProvider(prompter, next)
		if err != nil {
			return req, err
		}
		req.CredentialProviders = append(req.CredentialProviders, provider)
	}

	providerOptions := make([]ui.SelectOption, 0, len(req.CredentialProviders))
	for i := range req.CredentialProviders {
		provider := &req.CredentialProviders[i]
		providerOptions = append(providerOptions, ui.SelectOption{Label: provider.Name, Value: provider.Name})
	}
	for _, group := range groupsFromInventorySources(req.InventorySources) {
		selected, err := prompter.Select("Credential assignment: "+group, providerOptions)
		if err != nil {
			return req, err
		}
		if selected == "" {
			selected = req.CredentialProviders[0].Name
		}
		req.GroupCredentialProviders[group] = selected
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

	if len(req.InventorySources) == 0 {
		req.InventorySources = []initInventorySourceRequest{{Type: initInventoryLocal, Groups: []string{"default"}}}
	}
	if len(req.CredentialProviders) == 0 {
		req.CredentialProviders = []initCredentialProviderRequest{{Name: "pass-local", Type: config.CredentialProviderPass}}
	}

	cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	ensureInventoryGroups(cfg, req.InventorySources)
	if err := applyInventorySources(cfg, req.InventorySources); err != nil {
		return nil, err
	}

	cfg.Credential.Provider = make(map[string]config.CredentialProviderConfig)
	for i := range req.CredentialProviders {
		if err := applyCredentialProvider(cfg, req.CredentialProviders[i]); err != nil {
			return nil, err
		}
	}
	if len(req.GroupCredentialProviders) == 0 {
		req.GroupCredentialProviders = map[string]string{"default": req.CredentialProviders[0].Name}
	}
	for group, provider := range req.GroupCredentialProviders {
		ref := credentialRefForGroup(provider, group)
		groupCfg := cfg.Inventory.Group[group]
		groupCfg.Auth = config.InventoryAuthConfig{Provider: provider, Ref: ref}
		cfg.Inventory.Group[group] = groupCfg
	}
	if len(req.HostCredentialProviders) > 0 && cfg.Inventory.Host == nil {
		cfg.Inventory.Host = make(map[string]config.InventoryHostConfig)
	}
	for host, provider := range req.HostCredentialProviders {
		cfg.Inventory.Host[host] = config.InventoryHostConfig{
			Auth: config.InventoryAuthConfig{
				Provider: provider,
				Ref:      credentialRefForHost(provider, host),
			},
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &initPlan{Config: cfg}, nil
}

func promptNetBoxSource(prompter initPrompter) (initInventorySourceRequest, error) {
	name, err := prompter.Input("NetBox provider name", "netbox-prod")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	baseURL, err := prompter.Input("NetBox base_url", "")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	tokenEnv, err := prompter.Input("NetBox token_env", "NETBOX_TOKEN")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	group, err := prompter.Input("NetBox route group", "default")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	return initInventorySourceRequest{
		Type:     config.ProviderNetBox,
		Name:     name,
		Groups:   []string{group},
		BaseURL:  baseURL,
		TokenEnv: tokenEnv,
	}, nil
}

func promptContainerlabSource(prompter initPrompter) (initInventorySourceRequest, error) {
	name, err := prompter.Input("Containerlab provider name", "containerlab-lab")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	jumpHost, err := prompter.Input("Containerlab jump_host", "")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	group, err := prompter.Input("Containerlab route group", "lab")
	if err != nil {
		return initInventorySourceRequest{}, err
	}
	return initInventorySourceRequest{
		Type:     config.ProviderContainerlab,
		Name:     name,
		Groups:   []string{group},
		JumpHost: jumpHost,
	}, nil
}

func promptCredentialProvider(prompter initPrompter, providerType string) (initCredentialProviderRequest, error) {
	switch providerType {
	case config.CredentialProviderPass:
		name, err := prompter.Input("Pass provider name", "pass-local")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		return initCredentialProviderRequest{Name: name, Type: config.CredentialProviderPass, Command: "pass", Prefix: "nssh"}, nil
	case config.CredentialProvider1Password:
		name, err := prompter.Input("1Password provider name", "op-network")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		account, err := prompter.Input("1Password account", "")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		vault, err := prompter.Input("1Password vault", "Network")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		return initCredentialProviderRequest{Name: name, Type: config.CredentialProvider1Password, Account: account, Vault: vault, Session: config.ProviderSessionAgentOwned}, nil
	case config.CredentialProviderBitwarden:
		name, err := prompter.Input("Bitwarden provider name", "bw-local")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		return initCredentialProviderRequest{Name: name, Type: config.CredentialProviderBitwarden, Session: config.ProviderSessionExternal}, nil
	default:
		return initCredentialProviderRequest{}, fmt.Errorf("unsupported credential provider %q", providerType)
	}
}

func credentialProviderOptions() []ui.SelectOption {
	return []ui.SelectOption{
		{Label: "Pass", Value: config.CredentialProviderPass},
		{Label: "1Password", Value: config.CredentialProvider1Password},
		{Label: "Bitwarden", Value: config.CredentialProviderBitwarden},
	}
}

func groupsFromInventorySources(sources []initInventorySourceRequest) []string {
	seen := make(map[string]bool)
	for i := range sources {
		source := &sources[i]
		for _, group := range source.Groups {
			if group != "" {
				seen[group] = true
			}
		}
	}
	if len(seen) == 0 {
		seen["default"] = true
	}
	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}

func applyInventorySources(cfg *config.Config, sources []initInventorySourceRequest) error {
	for i := range sources {
		source := &sources[i]
		switch source.Type {
		case "", initInventoryLocal:
			continue
		case config.ProviderNetBox:
			if source.Name == "" {
				return fmt.Errorf("netbox provider name is required")
			}
			cfg.Inventory.Provider[source.Name] = config.InventoryProviderConfig{
				Type: config.ProviderNetBox,
				Config: config.InventoryProviderDetailConfig{
					BaseURL:  source.BaseURL,
					URLEnv:   source.URLEnv,
					TokenEnv: source.TokenEnv,
					EnvFile:  source.EnvFile,
				},
				Route: routesForGroups(source.Groups),
			}
		case config.ProviderContainerlab:
			if strings.TrimSpace(source.JumpHost) == "" {
				return fmt.Errorf("containerlab.config.jump_host is required")
			}
			if source.Name == "" {
				return fmt.Errorf("containerlab provider name is required")
			}
			cfg.Inventory.Provider[source.Name] = config.InventoryProviderConfig{
				Type: config.ProviderContainerlab,
				Config: config.InventoryProviderDetailConfig{
					JumpHost:              source.JumpHost,
					Sudo:                  source.Sudo,
					StrictHostKeyChecking: source.StrictHostKeyChecking,
				},
				Route: routesForGroups(source.Groups),
			}
		default:
			return fmt.Errorf("unsupported inventory source %q", source.Type)
		}
	}
	return nil
}

func ensureInventoryGroups(cfg *config.Config, sources []initInventorySourceRequest) {
	if cfg.Inventory.Group == nil {
		cfg.Inventory.Group = make(map[string]config.GroupConfig)
	}
	for i := range sources {
		source := &sources[i]
		for _, group := range source.Groups {
			if group == "" {
				continue
			}
			cfg.Inventory.Group[group] = config.GroupConfig{}
		}
	}
}

func applyCredentialProvider(cfg *config.Config, provider initCredentialProviderRequest) error {
	if provider.Name == "" {
		return fmt.Errorf("credential provider name is required")
	}
	detail := config.CredentialProviderDetailConfig{
		Command: provider.Command,
		Prefix:  provider.Prefix,
		Account: provider.Account,
		Vault:   provider.Vault,
		Session: provider.Session,
	}
	cfg.Credential.Provider[provider.Name] = config.CredentialProviderConfig{
		Type:   provider.Type,
		Config: detail,
	}
	return nil
}

func routesForGroups(groups []string) []config.InventoryRouteConfig {
	routes := make([]config.InventoryRouteConfig, 0, len(groups))
	for _, group := range groups {
		if group == "" {
			continue
		}
		routes = append(routes, config.InventoryRouteConfig{Group: group})
	}
	return routes
}

func credentialRefForGroup(provider, group string) string {
	switch {
	case strings.HasPrefix(provider, "op-"):
		return "nssh group " + group
	case strings.HasPrefix(provider, "bw-"):
		return "nssh group " + group
	default:
		return "nssh/groups/" + group
	}
}

func credentialRefForHost(provider, host string) string {
	switch {
	case strings.HasPrefix(provider, "op-"):
		return "nssh host " + host
	case strings.HasPrefix(provider, "bw-"):
		return "nssh host " + host
	default:
		return "nssh/hosts/" + host
	}
}

func initInventorySummary(cfg *config.Config) []string {
	out := []string{"Local SSH config"}
	names := make([]string, 0, len(cfg.Inventory.Provider))
	for name := range cfg.Inventory.Provider {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, name+" ("+cfg.Inventory.Provider[name].Type+")")
	}
	return out
}

func initCredentialProviderSummary(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Credential.Provider))
	for name := range cfg.Credential.Provider {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, credentialProviderLabel(cfg.Credential.Provider[name].Type))
	}
	return out
}

func initCredentialAssignmentSummary(cfg *config.Config) []string {
	groups := make([]string, 0, len(cfg.Inventory.Group))
	for group, groupCfg := range cfg.Inventory.Group {
		if groupCfg.Auth.IsSet() {
			groups = append(groups, group)
		}
	}
	sort.Strings(groups)
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		provider := cfg.Inventory.Group[group].Auth.Provider
		out = append(out, group+" -> "+provider)
	}
	return out
}

func credentialProviderLabel(providerType string) string {
	switch providerType {
	case config.CredentialProviderPass:
		return "Pass"
	case config.CredentialProvider1Password:
		return "1Password"
	case config.CredentialProviderBitwarden:
		return "Bitwarden"
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
	if err := config.Save(paths.ConfigFile, plan.Config); err != nil {
		return err
	}
	return os.Chmod(filepath.Dir(paths.ConfigFile), 0700)
}

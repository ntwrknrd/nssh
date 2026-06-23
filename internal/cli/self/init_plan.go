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

const credentialIncludePattern = "credential/*.yaml"

type initPlanRequest struct {
	Existing            *config.Config
	CredentialProviders []initCredentialProviderRequest
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
	Config *config.Config
}

type initPrompter interface {
	Select(title string, options []ui.SelectOption) (string, error)
	Input(title, defaultValue string) (string, error)
	Confirm(title string, defaultValue bool) (bool, error)
}

type uiInitPrompter struct{}

var runSelfInitCommand = runSelfInitCommandDefault

func (uiInitPrompter) Select(title string, options []ui.SelectOption) (string, error) {
	return ui.Select(title, options)
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

	lines := []string{
		"Credential providers: " + strings.Join(initCredentialProviderSummary(p.Config), ", "),
	}
	return strings.Join(lines, "\n")
}

func promptInitPlanRequest(prompter initPrompter, existing *config.Config) (initPlanRequest, error) {
	if prompter == nil {
		prompter = uiInitPrompter{}
	}
	req := initPlanRequest{Existing: existing}

	first, err := prompter.Select("Credential providers", credentialProviderOptions())
	if err != nil {
		return req, err
	}
	if first == "" {
		first = config.CredentialProviderSOPSAge
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

	if len(req.CredentialProviders) == 0 {
		req.CredentialProviders = []initCredentialProviderRequest{{Name: "sops", Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"}}
	}

	cfg.Credential.Provider = make(map[string]config.CredentialProviderConfig)
	for i := range req.CredentialProviders {
		if err := applyCredentialProvider(cfg, req.CredentialProviders[i]); err != nil {
			return nil, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &initPlan{Config: cfg}, nil
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
		account, err := prompter.Input("1Password account", "")
		if err != nil {
			return initCredentialProviderRequest{}, err
		}
		vault, err := prompter.Input("1Password vault", "Network")
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

func applyCredentialProviderSetup(paths *config.Paths, cfg *config.Config, providerType string, prompter initPrompter, dryRun bool) error {
	if paths == nil {
		return fmt.Errorf("paths are required")
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if provider, providerPath, ok := existingCredentialProviderSetup(paths, cfg, providerType); ok {
		printCredentialProviderSetup(provider, providerPath, dryRun)
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

func existingCredentialProviderSetup(paths *config.Paths, cfg *config.Config, providerType string) (initCredentialProviderRequest, string, bool) {
	if providerType != config.CredentialProviderSOPSAge || cfg == nil {
		return initCredentialProviderRequest{}, "", false
	}
	provider, ok := cfg.Credential.Provider["sops"]
	if !ok || provider.Type != config.CredentialProviderSOPSAge {
		return initCredentialProviderRequest{}, "", false
	}
	req := credentialProviderRequestFromConfig("sops", provider)
	providerPath := credentialProviderSetupPath(paths, req)
	if info, err := os.Stat(providerPath); err != nil || info.IsDir() {
		return initCredentialProviderRequest{}, "", false
	}
	return req, providerPath, true
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
	if err := ensureCredentialProviderInclude(paths.ConfigFile, provider.Name); err != nil {
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

func credentialProviderSetupPath(paths *config.Paths, provider initCredentialProviderRequest) string {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = strings.TrimSpace(provider.Type)
	}
	return filepath.Join(paths.ConfigDir, "credential", name+".yaml")
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
	return text, nil
}

func addProviderString(table map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		table[key] = value
	}
}

func ensureCredentialProviderInclude(rootPath, providerName string) error {
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
	root["include"] = includeWithCredentialPattern(root["include"])
	removeRootCredentialProvider(root, providerName)
	text, err := marshalInitYAML(root)
	if err != nil {
		return fmt.Errorf("encode root config: %w", err)
	}
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

func includeWithCredentialPattern(value any) []string {
	out := []string{credentialIncludePattern}
	seen := map[string]bool{credentialIncludePattern: true}
	for _, include := range includeStringValues(value) {
		if seen[include] {
			continue
		}
		seen[include] = true
		out = append(out, include)
	}
	return out
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

func removeRootCredentialProvider(root map[string]any, providerName string) {
	credential, ok := root["credential"].(map[string]any)
	if !ok {
		return
	}
	providers, ok := credential["provider"].(map[string]any)
	if !ok {
		return
	}
	delete(providers, providerName)
	if len(providers) == 0 {
		delete(credential, "provider")
	}
	if len(credential) == 0 {
		delete(root, "credential")
	}
}

func prepareCredentialProviders(providers []initCredentialProviderRequest, prompter initPrompter, dryRun bool) error {
	for i := range providers {
		if err := prepareCredentialProviderSetup(&providers[i], prompter, dryRun); err != nil {
			return err
		}
	}
	return nil
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

func applyInitPlan(paths *config.Paths, plan *initPlan) error {
	if paths == nil {
		return fmt.Errorf("paths are required")
	}
	if plan == nil || plan.Config == nil {
		return fmt.Errorf("init plan config is required")
	}
	if err := writeCredentialProviderConfigs(paths, plan.Config.Credential.Provider); err != nil {
		return err
	}
	rootCfg := *plan.Config
	rootCfg.Include = includeWithCredentialPattern(rootCfg.Include)
	rootCfg.Credential.Provider = nil
	return saveInitConfig(paths, &rootCfg)
}

func writeCredentialProviderConfigs(paths *config.Paths, providers map[string]config.CredentialProviderConfig) error {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := writeCredentialProviderConfig(paths, credentialProviderRequestFromConfig(name, providers[name])); err != nil {
			return err
		}
	}
	return nil
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

func saveInitConfig(paths *config.Paths, cfg *config.Config) error {
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
	if err := config.Save(paths.ConfigFile, cfg); err != nil {
		return err
	}
	return os.Chmod(filepath.Dir(paths.ConfigFile), 0700)
}

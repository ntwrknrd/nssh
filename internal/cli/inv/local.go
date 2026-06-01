package inv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"golang.org/x/term"
)

type hostPatch struct {
	Host         string
	Group        string
	HostName     string
	User         string
	Port         int
	PortSet      bool
	AuthMode     string
	Auth         config.InventoryAuthConfig
	AuthDisabled bool

	CompatFixes []compat.CompatType
}

type hostMetadata struct {
	Owner string
	Group string
}

type groupPromptFunc func([]string) (string, error)

type localHostAddPrompter interface {
	InputWithDefault(title, defaultValue string) (string, error)
	Select(title string, options []ui.SelectOption) (string, error)
}

type uiLocalHostAddPrompter struct{}

func (uiLocalHostAddPrompter) InputWithDefault(title, defaultValue string) (string, error) {
	return ui.InputWithDefault(title, defaultValue)
}

func (uiLocalHostAddPrompter) Select(title string, options []ui.SelectOption) (string, error) {
	return ui.Select(title, options)
}

type importResult struct {
	Added   int
	Skipped int
	Failed  int
	Errors  []string
}

type localHostCompatResult struct {
	Success       bool
	FixesApplied  []compat.CompatType
	TestResult    *connector.TestResult
	StoppedReason string
}

const (
	promptBackValue        = "__back__"
	promptBackInput        = ":back"
	promptCreateGroupValue = "__create_group__"

	localBackupTimestampLayout = "20060102_150405"
	localBackupHourlyKeep      = 10
	localBackupDailyKeep       = 5
	localBackupDayHistory      = 7
)

var errPromptBack = errors.New("prompt back")
var runLocalHostCompatCheck = testLocalHostCompatibility
var promptNewInventoryGroupName = func() (string, error) {
	return ui.InputWithDefault("New local group", "")
}

var localHostConnectionTest = func(ctx context.Context, host *sshconfig.HostEntry, cfg connector.TestConfig) (*connector.TestResult, error) {
	return connector.TestConnection(ctx, host.Host, host.User(), cfg)
}

type credentialRegistry interface {
	Provider(name string) credential.Provider
}

var newCredentialRegistry = func(cfg *config.Config) (credentialRegistry, error) {
	return credential.NewRegistry(cfg)
}

func upsertLocalHost(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, patch hostPatch) error {
	if patch.Host == "" {
		return fmt.Errorf("host is required")
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return err
	}
	targetFile := localFilePath(paths, inventory.LocalProviderIncludeFile())

	existing, existingCfg, err := findInventoryHostWithLocation(parser, cfg, paths, patch.Host)
	if err != nil {
		return err
	}
	if existing != nil && metadataForHost(existing, cfg, paths, nil).Owner != "local" {
		return fmt.Errorf("host %q is provider-owned; change provider group selector config instead", patch.Host)
	}

	groupName := patch.Group
	if groupName == "" && existing != nil {
		groupName = inventory.LocalHostGroup(existing, "")
	}
	if groupName == "" {
		return fmt.Errorf("group is required")
	}
	if _, ok, err := localProviderGroup(cfg, groupName); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("group %q not found", groupName)
	}

	var host *sshconfig.HostEntry
	if existing != nil {
		host = cloneHostEntry(existing)
		applyHostPatch(host, patch)
	} else {
		port := patch.Port
		if !patch.PortSet {
			port = 22
		}
		host = sshconfig.CreateHostEntry(patch.Host, patch.HostName, "", port, patch.AuthMode == config.AuthModePassword, targetFile)
		if len(patch.CompatFixes) > 0 {
			if err := sshconfig.ApplyCompatFixes(host, patch.CompatFixes); err != nil {
				return err
			}
		}
	}
	host.SourceFile = targetFile
	inventory.SetLocalHostGroup(host, groupName)

	if existingCfg != nil && existingCfg.Path != targetFile {
		existingCfg.Hosts = sshconfig.RemoveHost(existingCfg.Hosts, patch.Host)
		sshconfig.SortHosts(existingCfg.Hosts)
		if err := writeParsedConfig(parser, existingCfg, paths); err != nil {
			return err
		}
	}

	targetCfg, err := parser.ParseFile(targetFile)
	if err != nil {
		return err
	}
	targetCfg.Hosts = sshconfig.RemoveHost(targetCfg.Hosts, patch.Host)
	idx := sshconfig.FindInsertionIndex(targetCfg.Hosts, host.Host)
	targetCfg.Hosts = append(targetCfg.Hosts[:idx], append([]*sshconfig.HostEntry{host}, targetCfg.Hosts[idx:]...)...)
	sshconfig.SortHosts(targetCfg.Hosts)
	return writeParsedConfig(parser, targetCfg, paths)
}

func resolveLocalHostGroup(cfg *config.Config, patch hostPatch, existing *sshconfig.HostEntry, prompt groupPromptFunc) (string, error) {
	if strings.TrimSpace(patch.Group) != "" {
		group := strings.TrimSpace(patch.Group)
		if _, _, err := localProviderGroup(cfg, group); err != nil {
			return "", err
		}
		return group, nil
	}
	if existing != nil {
		if group := inventory.LocalHostGroup(existing, ""); group != "" {
			return group, nil
		}
	}
	if group := inferLocalHostGroupFromMatch(cfg, patch); group != "" {
		return group, nil
	}
	if prompt == nil {
		return "", fmt.Errorf("group is required")
	}
	groups := sortedInventoryGroupNames(cfg)
	selected, err := prompt(groups)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(selected) == "" {
		return "", fmt.Errorf("selection canceled")
	}
	return strings.TrimSpace(selected), nil
}

func inferLocalHostGroupFromMatch(cfg *config.Config, patch hostPatch) string {
	if cfg == nil {
		return ""
	}
	selectors := localGroupSelectors(cfg)
	if len(selectors) == 0 {
		return ""
	}
	matches := inventory.MatchGroupSelectors(localHostInventoryObject(patch), selectors)
	if len(matches) != 1 {
		return ""
	}
	return matches[0].Group
}

func localGroupSelectors(cfg *config.Config) []config.InventoryGroupSelector {
	selectors := cfg.Inventory.ProviderSelectors(config.ProviderLocal)
	seen := make(map[string]bool, len(selectors))
	for _, selector := range selectors {
		seen[selector.Group] = true
	}
	for groupName, groupCfg := range cfg.Inventory.Group {
		groupID := config.FormatInventoryGroupID(config.ProviderLocal, groupName)
		if seen[groupID] {
			continue
		}
		selectors = append(selectors, config.InventoryGroupSelector{
			Group:    groupID,
			Provider: config.ProviderLocal,
			Match:    groupCfg.Match,
		})
	}
	sort.Slice(selectors, func(i, j int) bool {
		return selectors[i].Group < selectors[j].Group
	})
	return selectors
}

func localHostInventoryObject(patch hostPatch) *inventory.Object {
	target := strings.TrimSpace(patch.HostName)
	if target == "" {
		target = strings.TrimSpace(patch.Host)
	}
	attrs := make(map[string][]string)
	if suffix := domainSuffixFromFQDN(target); suffix != "" {
		attrs["domain_suffix"] = []string{suffix}
	}
	return &inventory.Object{
		Provider:   config.ProviderLocal,
		ObjectType: config.ProviderLocal,
		Name:       strings.TrimSpace(patch.Host),
		FQDN:       target,
		HostName:   target,
		Attributes: attrs,
	}
}

func domainSuffixFromFQDN(fqdn string) string {
	fqdn = strings.ToLower(strings.TrimSpace(fqdn))
	dot := strings.Index(fqdn, ".")
	if dot <= 0 || dot == len(fqdn)-1 {
		return ""
	}
	return fqdn[dot:]
}

func sortedInventoryGroupNames(cfg *config.Config) []string {
	if cfg == nil || (len(cfg.Inventory.Provider[config.ProviderLocal].Group) == 0 && len(cfg.Inventory.Group) == 0) {
		return nil
	}
	localProvider := cfg.Inventory.Provider[config.ProviderLocal]
	groups := make([]string, 0, len(localProvider.Group))
	for group := range localProvider.Group {
		groups = append(groups, config.FormatInventoryGroupID(config.ProviderLocal, group))
	}
	for group := range cfg.Inventory.Group {
		id := config.FormatInventoryGroupID(config.ProviderLocal, group)
		if !slices.Contains(groups, id) {
			groups = append(groups, id)
		}
	}
	sort.Strings(groups)
	return groups
}

func defaultHostNameForGroup(cfg *config.Config, host, group string) string {
	return host
}

func promptInputWithBack(prompter localHostAddPrompter, title, defaultValue string, allowBack bool) (string, error) {
	value, err := prompter.InputWithDefault(title, defaultValue)
	if err != nil {
		return "", err
	}
	if allowBack && strings.TrimSpace(value) == promptBackInput {
		return "", errPromptBack
	}
	return value, nil
}

func promptSelectWithBack(prompter localHostAddPrompter, title string, options []ui.SelectOption, allowBack bool) (string, error) {
	if allowBack {
		options = append(append([]ui.SelectOption(nil), options...), ui.SelectOption{Label: "Back", Value: promptBackValue})
	}
	selected, err := prompter.Select(title, options)
	if err != nil {
		return "", err
	}
	if allowBack && selected == promptBackValue {
		return "", errPromptBack
	}
	return selected, nil
}

func promptLocalHostAddDetails(cfg *config.Config, patch hostPatch, prompter localHostAddPrompter) (hostPatch, error) {
	if prompter == nil {
		prompter = uiLocalHostAddPrompter{}
	}
	patch.Host = strings.TrimSpace(patch.Host)
	if patch.Host == "" {
		return patch, fmt.Errorf("host is required")
	}

	type promptStep int
	const (
		stepHost promptStep = iota
		stepHostName
		stepPort
		stepAuth
		stepCredentialSource
		stepUser
	)
	step := stepHost
	for {
		switch step {
		case stepHost:
			hostDefault := strings.TrimSpace(patch.Host)
			if derived := sshconfig.DeriveHostID(hostDefault); derived != "" {
				hostDefault = derived
			}
			host, err := promptInputWithBack(prompter, "Host", hostDefault, true)
			if err != nil {
				return patch, err
			}
			host = strings.TrimSpace(host)
			if host == "" {
				return patch, fmt.Errorf("host is required")
			}
			patch.Host = host
			step = stepHostName
		case stepHostName:
			hostNameDefault := strings.TrimSpace(patch.HostName)
			if hostNameDefault == "" {
				hostNameDefault = defaultHostNameForGroup(cfg, patch.Host, patch.Group)
			}
			hostName, err := promptInputWithBack(prompter, "HostName", hostNameDefault, true)
			if errors.Is(err, errPromptBack) {
				step = stepHost
				continue
			}
			if err != nil {
				return patch, err
			}
			patch.HostName = strings.TrimSpace(hostName)
			step = stepPort
		case stepPort:
			portDefault := "22"
			if patch.PortSet && patch.Port > 0 {
				portDefault = strconv.Itoa(patch.Port)
			}
			portValue, err := promptInputWithBack(prompter, "Port", portDefault, true)
			if errors.Is(err, errPromptBack) {
				step = stepHostName
				continue
			}
			if err != nil {
				return patch, err
			}
			port, err := strconv.Atoi(strings.TrimSpace(portValue))
			if err != nil || port < 1 {
				return patch, fmt.Errorf("invalid port %q", portValue)
			}
			patch.Port = port
			patch.PortSet = true
			step = stepAuth
		case stepAuth:
			authOptions := []ui.SelectOption{
				{Label: "Public key", Value: config.AuthModeKey},
				{Label: "Password", Value: config.AuthModePassword},
			}
			if patch.AuthMode == config.AuthModePassword {
				authOptions = []ui.SelectOption{
					{Label: "Password", Value: config.AuthModePassword},
					{Label: "Public key", Value: config.AuthModeKey},
				}
			}
			authMode, err := promptSelectWithBack(prompter, "Authentication", authOptions, true)
			if errors.Is(err, errPromptBack) {
				step = stepPort
				continue
			}
			if err != nil {
				return patch, err
			}
			authMode = strings.TrimSpace(authMode)
			if authMode == "" {
				authMode = config.AuthModeKey
			}
			if authMode != config.AuthModeKey && authMode != config.AuthModePassword {
				return patch, fmt.Errorf("unknown auth mode %q", authMode)
			}
			patch.AuthMode = authMode
			patch.Auth = config.InventoryAuthConfig{}
			if authMode == config.AuthModeKey {
				step = stepUser
				continue
			}
			step = stepCredentialSource
		case stepCredentialSource:
			auth, source, err := promptCredentialSource(cfg, patch.Group, patch.Host, prompter)
			if errors.Is(err, errPromptBack) {
				step = stepAuth
				continue
			}
			if err != nil {
				return patch, err
			}
			patch.Auth = auth
			switch source {
			case "host", "group":
				return patch, nil
			case "none":
				step = stepUser
			default:
				return patch, fmt.Errorf("unknown credential source %q", source)
			}
		case stepUser:
			if err := promptLocalHostUser(cfg, &patch, prompter); errors.Is(err, errPromptBack) {
				if patch.AuthMode == config.AuthModePassword {
					step = stepCredentialSource
				} else {
					step = stepAuth
				}
				continue
			} else if err != nil {
				return patch, err
			}
			patch.Auth = config.InventoryAuthConfig{Username: patch.User, AuthMode: patch.AuthMode}
			return patch, nil
		}
	}
}

func shouldPromptLocalHostAddDetails(existing *sshconfig.HostEntry, group, hostname, user string, portSet bool, authPatch hostAuthPatch) bool {
	return existing == nil &&
		strings.TrimSpace(group) == "" &&
		strings.TrimSpace(hostname) == "" &&
		strings.TrimSpace(user) == "" &&
		!portSet &&
		!authPatch.HasChange() &&
		term.IsTerminal(int(os.Stdin.Fd()))
}

func defaultUserForGroup(cfg *config.Config, group string) string {
	if cfg == nil {
		return ""
	}
	auth := cfg.ResolveInventoryAuth(config.InventoryAuthContext{Group: group})
	if strings.TrimSpace(auth.Username) != "" {
		return strings.TrimSpace(auth.Username)
	}
	return ""
}

func localHostProbeUser(cfg *config.Config, patch hostPatch, record *credential.Record) string {
	if strings.TrimSpace(patch.User) != "" {
		return strings.TrimSpace(patch.User)
	}
	if cfg != nil {
		auth := cfg.ResolveInventoryAuth(config.InventoryAuthContext{Host: patch.Host, Group: patch.Group})
		if strings.TrimSpace(auth.Username) != "" {
			return strings.TrimSpace(auth.Username)
		}
	}
	if record != nil && strings.TrimSpace(record.Username) != "" {
		return strings.TrimSpace(record.Username)
	}
	return ""
}

func promptLocalHostUser(cfg *config.Config, patch *hostPatch, prompter localHostAddPrompter) error {
	userDefault := strings.TrimSpace(patch.User)
	if userDefault == "" {
		userDefault = defaultUserForGroup(cfg, patch.Group)
	}
	user, err := promptInputWithBack(prompter, "User", userDefault, true)
	if err != nil {
		return err
	}
	patch.User = strings.TrimSpace(user)
	return nil
}

func localHostEntryFromPatch(paths *config.Paths, patch hostPatch) *sshconfig.HostEntry {
	port := patch.Port
	if port <= 0 {
		port = 22
	}
	return sshconfig.CreateHostEntry(
		patch.Host,
		patch.HostName,
		"",
		port,
		patch.AuthMode == config.AuthModePassword,
		localFilePath(paths, inventory.LocalProviderIncludeFile()),
	)
}

func promptCredentialSource(cfg *config.Config, group, host string, prompter localHostAddPrompter) (config.InventoryAuthConfig, string, error) {
	for {
		options := make([]ui.SelectOption, 0, 3)
		if cfg != nil {
			if groupCfg, ok, err := localProviderGroup(cfg, group); err == nil && ok && groupCfg.Auth.IsSet() {
				auth := groupCfg.Auth
				auth.Normalize()
				options = append(options, ui.SelectOption{
					Label: fmt.Sprintf("Use group stored credential (%s: %s)", auth.CredentialProvider, displayStoredPasswordRef(auth.PasswordRef)),
					Value: "group",
				})
			}
		}
		options = append(options,
			ui.SelectOption{Label: "Set host stored credential", Value: "host"},
			ui.SelectOption{Label: "No stored credential", Value: "none"},
		)
		selected, err := promptSelectWithBack(prompter, "Credential source", options, true)
		if err != nil {
			return config.InventoryAuthConfig{}, "", err
		}
		switch selected {
		case "group", "none", "":
			if selected == "" {
				selected = "none"
			}
			return config.InventoryAuthConfig{}, selected, nil
		case "host":
			auth, err := promptHostCredentialAuth(cfg, host, prompter)
			if errors.Is(err, errPromptBack) {
				continue
			}
			if err != nil {
				return config.InventoryAuthConfig{}, "", err
			}
			return auth, selected, nil
		default:
			return config.InventoryAuthConfig{}, "", fmt.Errorf("unknown credential source %q", selected)
		}
	}
}

func displayStoredPasswordRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "op://") {
		ref = strings.TrimSuffix(ref, "/password")
	}
	return ref
}

func promptHostCredentialAuth(cfg *config.Config, host string, prompter localHostAddPrompter) (config.InventoryAuthConfig, error) {
	providers := credentialProviderOptions(cfg)
	if len(providers) == 0 {
		return config.InventoryAuthConfig{}, fmt.Errorf("no credential providers configured")
	}
	for {
		provider, err := promptSelectWithBack(prompter, "Credential provider", providers, true)
		if err != nil {
			return config.InventoryAuthConfig{}, err
		}
		provider = strings.TrimSpace(provider)
		if provider == "" {
			return config.InventoryAuthConfig{}, fmt.Errorf("credential provider is required")
		}

		ref, err := promptPasswordRef(cfg, provider, host, prompter)
		if errors.Is(err, errPromptBack) {
			continue
		}
		if err != nil {
			return config.InventoryAuthConfig{}, err
		}
		auth := config.InventoryAuthConfig{
			CredentialProvider: provider,
			PasswordRef:        strings.TrimSpace(ref),
		}
		if auth.PasswordRef == "" {
			return config.InventoryAuthConfig{}, fmt.Errorf("password_ref is required")
		}
		if err := auth.Validate("inventory.host.auth"); err != nil {
			return config.InventoryAuthConfig{}, err
		}
		return auth, nil
	}
}

func credentialProviderOptions(cfg *config.Config) []ui.SelectOption {
	if cfg == nil || len(cfg.Credential.Provider) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.Credential.Provider))
	for name := range cfg.Credential.Provider {
		names = append(names, name)
	}
	sort.Strings(names)
	options := make([]ui.SelectOption, 0, len(names))
	for _, name := range names {
		provider := cfg.Credential.Provider[name]
		label := name
		if provider.Type != "" {
			label = fmt.Sprintf("%s (%s)", name, provider.Type)
		}
		options = append(options, ui.SelectOption{Label: label, Value: name})
	}
	return options
}

func promptPasswordRef(cfg *config.Config, provider, host string, prompter localHostAddPrompter) (string, error) {
	items, err := listCredentialItems(cfg, provider)
	if err == nil && len(items) > 0 {
		options := make([]ui.SelectOption, 0, len(items)+1)
		for _, item := range items {
			options = append(options, ui.SelectOption{Label: item.Label, Value: item.Ref})
		}
		options = append(options, ui.SelectOption{Label: "Manual password ref", Value: "__manual__"})
		selected, err := promptSelectWithBack(prompter, "Credential item", options, true)
		if err != nil {
			return "", err
		}
		if selected != "__manual__" && strings.TrimSpace(selected) != "" {
			return selected, nil
		}
	}
	return promptInputWithBack(prompter, "Password ref", defaultPasswordRef(cfg, provider, host), true)
}

func defaultPasswordRef(cfg *config.Config, provider, host string) string {
	if cfg == nil {
		return host
	}
	providerCfg := cfg.Credential.Provider[provider]
	switch providerCfg.Type {
	case config.CredentialProviderPass:
		prefix := strings.Trim(strings.TrimSpace(providerCfg.Config.Prefix), "/")
		if prefix == "" {
			prefix = "nssh"
		}
		return prefix + "/hosts/" + host
	default:
		return host
	}
}

func resolveLocalHostCredentialSecret(cfg *config.Config, patch hostPatch) (*secret.Secret, error) {
	record, err := resolveLocalHostCredentialRecord(cfg, patch)
	if err != nil || record == nil {
		return nil, err
	}
	return record.Secret, nil
}

func resolveLocalHostCredentialRecord(cfg *config.Config, patch hostPatch) (*credential.Record, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if patch.AuthDisabled {
		return nil, nil
	}
	if patch.Auth.IsSet() {
		if cfg.Inventory.Host == nil {
			cfg.Inventory.Host = make(map[string]config.InventoryHostConfig)
		}
		cfg.Inventory.Host[patch.Host] = config.InventoryHostConfig{Auth: patch.Auth}
	}
	registry, err := newCredentialRegistry(cfg)
	if err != nil {
		return nil, err
	}
	if patch.Auth.IsSet() {
		auth := patch.Auth
		auth.Normalize()
		provider := registry.Provider(auth.CredentialProvider)
		if provider == nil || auth.PasswordRef == "" {
			return nil, nil
		}
		return provider.GetRef(auth.CredentialRef())
	}
	if hostCfg, ok := cfg.Inventory.Host[patch.Host]; ok && hostCfg.AuthDisabled {
		return nil, nil
	} else if ok && hostCfg.Auth.IsSet() {
		auth := hostCfg.Auth
		auth.Normalize()
		provider := registry.Provider(auth.CredentialProvider)
		if provider == nil || auth.PasswordRef == "" {
			return nil, nil
		}
		return provider.GetRef(auth.CredentialRef())
	}
	if groupCfg, ok, err := localProviderGroup(cfg, patch.Group); err == nil && ok && groupCfg.Auth.IsSet() {
		auth := groupCfg.Auth
		auth.Normalize()
		provider := registry.Provider(auth.CredentialProvider)
		if provider == nil || auth.PasswordRef == "" {
			return nil, nil
		}
		return provider.GetRef(auth.CredentialRef())
	}
	return nil, nil
}

func applyInteractiveHostAuthSelection(cfg *config.Config, patch hostPatch) bool {
	if cfg == nil {
		return false
	}
	if patch.Auth.IsSet() || patch.AuthDisabled {
		if cfg.Inventory.Host == nil {
			cfg.Inventory.Host = make(map[string]config.InventoryHostConfig)
		}
		cfg.Inventory.Host[patch.Host] = config.InventoryHostConfig{Auth: patch.Auth, AuthDisabled: patch.AuthDisabled}
		return true
	}
	if cfg.Inventory.Host == nil {
		return false
	}
	if _, ok := cfg.Inventory.Host[patch.Host]; !ok {
		return false
	}
	delete(cfg.Inventory.Host, patch.Host)
	return true
}

func credentialSecret(provider credential.Provider, scope, name string) (*secret.Secret, error) {
	record, err := credentialRecord(provider, scope, name)
	if err != nil || record == nil {
		return nil, err
	}
	return record.Secret, nil
}

func credentialRecord(provider credential.Provider, scope, name string) (*credential.Record, error) {
	if provider == nil {
		return nil, nil
	}
	var (
		record *credential.Record
		err    error
	)
	if scope == "host" {
		record, err = provider.GetHost(name)
	} else {
		record, err = provider.GetGroup(name)
	}
	if err != nil || record == nil {
		return nil, err
	}
	return record, nil
}

func promptInventoryGroup(groups []string) (string, error) {
	options := make([]ui.SelectOption, 0, len(groups))
	for _, group := range groups {
		options = append(options, ui.SelectOption{Label: group, Value: group})
	}
	return promptInventoryGroupOptions(options)
}

func promptInventoryGroupOptions(options []ui.SelectOption) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("group is required")
	}
	for {
		options := inventoryGroupPromptOptions(options)
		selected, err := ui.Select("Group", options)
		if err != nil {
			return "", err
		}
		if selected == promptBackValue {
			return "", errPromptBack
		}
		if selected != promptCreateGroupValue {
			return selected, err
		}
		group, err := promptNewInventoryGroupName()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(group) == promptBackInput {
			continue
		}
		return normalizePromptedLocalGroup(group)
	}
}

func inventoryGroupPromptOptions(options []ui.SelectOption) []ui.SelectOption {
	options = append(append([]ui.SelectOption(nil), options...),
		ui.SelectOption{Label: "Create new group", Value: promptCreateGroupValue},
	)
	return options
}

func normalizePromptedLocalGroup(group string) (string, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return "", fmt.Errorf("group is required")
	}
	if !strings.Contains(group, "/") {
		group = config.FormatInventoryGroupID(config.ProviderLocal, group)
	}
	if err := validateLocalGroupID(group); err != nil {
		return "", err
	}
	return group, nil
}

func removeLocalHost(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, hostName string) (bool, error) {
	if hostName == "" {
		return false, fmt.Errorf("host is required")
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return false, err
	}
	host, parsed, err := findInventoryHostWithLocation(parser, cfg, paths, hostName)
	if err != nil {
		return false, err
	}
	if host == nil || parsed == nil {
		return false, nil
	}
	if metadataForHost(host, cfg, paths, nil).Owner != "local" {
		return false, fmt.Errorf("host %q is provider-owned; change provider group selector config instead", hostName)
	}
	parsed.Hosts = sshconfig.RemoveHost(parsed.Hosts, hostName)
	sshconfig.SortHosts(parsed.Hosts)
	return true, writeParsedConfig(parser, parsed, paths)
}

func importLocalCSV(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, csvPath, group string) (*importResult, error) {
	if csvPath == "" {
		return nil, fmt.Errorf("CSV file is required")
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return nil, err
	}
	file, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("open CSV: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for i, col := range header {
		columns[strings.ToLower(strings.TrimSpace(col))] = i
	}
	if _, ok := columns["host"]; !ok {
		return nil, fmt.Errorf("CSV file missing required host column")
	}

	result := &importResult{}
	line := 1
	for {
		line++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("line %d: %w", line, err)
		}
		patch := hostPatch{
			Host:     csvValue(record, columns, "host"),
			HostName: csvValue(record, columns, "hostname"),
			User:     csvValue(record, columns, "user"),
		}
		if patch.Host == "" {
			result.Skipped++
			continue
		}
		patch.Group = group
		if patch.Group == "" {
			patch.Group = csvValue(record, columns, "group")
		}
		if portValue := csvValue(record, columns, "port"); portValue != "" {
			port, err := strconv.Atoi(portValue)
			if err != nil || port < 1 {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid port %q", line, portValue))
				continue
			}
			patch.Port = port
			patch.PortSet = true
		}
		if err := upsertLocalHost(parser, cfg, paths, patch); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("line %d host %s: %v", line, patch.Host, err))
			continue
		}
		result.Added++
	}
	return result, nil
}

func csvValue(record []string, columns map[string]int, name string) string {
	idx, ok := columns[name]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

func localFilePath(paths *config.Paths, localFile string) string {
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if filepath.IsAbs(localFile) {
		return localFile
	}
	if strings.ContainsRune(localFile, filepath.Separator) {
		return filepath.Join(paths.SSHConfigDir, localFile)
	}
	return filepath.Join(paths.SSHConfigDir, "nssh.d", localFile)
}

func localProviderOwnerLabel(paths *config.Paths) string {
	return "Inventory Filepath: " + localFilePath(paths, inventory.LocalProviderIncludeFile())
}

func localWrittenHostConfig(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, host string) (string, error) {
	entry, _, err := findInventoryHostWithLocation(parser, cfg, paths, host)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", fmt.Errorf("host %q not found", host)
	}
	return strings.Join(entry.Lines, ""), nil
}

func printLocalWrittenHostConfig(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, host string) {
	stanza, err := localWrittenHostConfig(parser, cfg, paths, host)
	if err != nil {
		ui.Warning("Failed to print written config: %v", err)
		return
	}
	ui.Info("Written config:")
	for _, line := range strings.Split(strings.TrimRight(stanza, "\n"), "\n") {
		fmt.Printf("    %s\n", line)
	}
}

func testLocalHostCompatibility(
	ctx context.Context,
	cfg *config.Config,
	host *sshconfig.HostEntry,
	maxIterations int,
	password *secret.Secret,
) (*localHostCompatResult, error) {
	if maxIterations <= 0 {
		maxIterations = 5
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	testHost := cloneHostEntry(host)
	tmp, err := os.CreateTemp("", "nssh-host-add-*.conf")
	if err != nil {
		return nil, fmt.Errorf("create temp ssh config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp ssh config: %w", err)
	}
	writeTemp := func() error {
		return os.WriteFile(tmpPath, []byte(strings.Join(testHost.Lines, "")), 0600)
	}
	if err := writeTemp(); err != nil {
		return nil, fmt.Errorf("write temp ssh config: %w", err)
	}

	result := &localHostCompatResult{FixesApplied: make([]compat.CompatType, 0)}
	appliedSet := make(map[compat.CompatType]bool)
	for iteration := 1; iteration <= maxIterations; iteration++ {
		ui.Info("Testing connection to %s (%d/%d)...", testHost.Host, iteration, maxIterations)
		testResult, err := localHostConnectionTest(ctx, testHost, connector.TestConfig{
			Timeout:             10 * time.Second,
			Password:            password,
			ConfigFile:          tmpPath,
			Port:                testHost.Port(),
			UseSystemKnownHosts: cfg.SSH.Security.CompatPersistProbes,
		})
		if err != nil {
			result.TestResult = &connector.TestResult{Success: false, ExitCode: 1, Stderr: err.Error()}
			result.StoppedReason = "non_compatibility_error"
			return result, nil
		}
		result.TestResult = testResult
		if testResult.Success || compat.IsAuthFailureAfterKex(testResult.Stderr) {
			result.Success = true
			if testResult.Success {
				result.StoppedReason = "connection_succeeded"
			} else {
				result.StoppedReason = "auth_failed_after_kex_success"
			}
			return result, nil
		}

		compatTypes := compat.ParseCompatibilityError(testResult.Stderr)
		if len(compatTypes) == 0 {
			result.StoppedReason = "no_compatibility_issues"
			return result, nil
		}

		var newFixes []compat.CompatType
		for _, ct := range compatTypes {
			if !appliedSet[ct] {
				newFixes = append(newFixes, ct)
			}
		}
		if len(newFixes) == 0 {
			result.StoppedReason = "no_progress"
			return result, nil
		}
		for _, ct := range newFixes {
			ui.Warning("Applying: %s", compat.CompatConfigs[ct].Name)
		}
		if err := sshconfig.ApplyCompatFixes(testHost, newFixes); err != nil {
			result.StoppedReason = "fix_application_error"
			return result, nil
		}
		if err := writeTemp(); err != nil {
			return nil, fmt.Errorf("update temp ssh config: %w", err)
		}
		for _, ct := range newFixes {
			appliedSet[ct] = true
			result.FixesApplied = append(result.FixesApplied, ct)
		}
	}
	result.StoppedReason = "max_iterations_reached"
	return result, nil
}

func inventoryHosts(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths) ([]*sshconfig.HostEntry, error) {
	if parser == nil {
		parser = sshconfig.NewParser()
	}
	files, err := inventoryFiles(cfg, paths)
	if err != nil {
		return nil, err
	}
	hosts := make([]*sshconfig.HostEntry, 0)
	for _, file := range files {
		parsed, err := parser.ParseFile(file)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, parsed.Hosts...)
	}
	return hosts, nil
}

func findInventoryHostWithLocation(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, pattern string) (*sshconfig.HostEntry, *sshconfig.ParsedConfig, error) {
	if parser == nil {
		parser = sshconfig.NewParser()
	}
	files, err := inventoryFiles(cfg, paths)
	if err != nil {
		return nil, nil, err
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(file)
		if err != nil {
			return nil, nil, err
		}
		if host := sshconfig.FindHostByPattern(parsed.Hosts, pattern); host != nil {
			return host, parsed, nil
		}
	}
	return nil, nil, nil
}

func inventoryFiles(cfg *config.Config, paths *config.Paths) ([]string, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	files := make([]string, 0, 1+len(cfg.Inventory.Provider))
	add := func(path string) {
		clean := filepath.Clean(path)
		if seen[clean] {
			return
		}
		seen[clean] = true
		files = append(files, clean)
	}
	add(localFilePath(paths, inventory.LocalProviderIncludeFile()))
	for name := range cfg.Inventory.Provider {
		add(localFilePath(paths, inventory.ProviderIncludeFile(name)))
	}
	sort.Strings(files)
	return files, nil
}

func metadataForHost(host *sshconfig.HostEntry, cfg *config.Config, paths *config.Paths, index map[string]*inventory.HostInfo) hostMetadata {
	if host == nil {
		return hostMetadata{Owner: "local"}
	}
	if index != nil {
		if info := index[host.Host]; info != nil {
			return hostMetadata{Owner: info.Provider, Group: info.Group}
		}
		for _, pattern := range host.Patterns {
			if info := index[pattern]; info != nil {
				return hostMetadata{Owner: info.Provider, Group: info.Group}
			}
		}
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	for name := range cfg.Inventory.Provider {
		if name == inventory.LocalProviderName {
			continue
		}
		if samePath(host.SourceFile, localFilePath(paths, inventory.ProviderIncludeFile(name))) {
			return hostMetadata{Owner: name, Group: "-"}
		}
	}
	if samePath(host.SourceFile, localFilePath(paths, inventory.LocalProviderIncludeFile())) {
		return hostMetadata{Owner: inventory.LocalProviderName, Group: inventory.LocalHostGroup(host, "-")}
	}
	return hostMetadata{Owner: "local", Group: "-"}
}

func localProviderGroup(cfg *config.Config, groupID string) (config.GroupConfig, bool, error) {
	provider, group, err := config.ParseInventoryGroupID(groupID)
	if err != nil {
		return config.GroupConfig{}, false, err
	}
	if provider != config.ProviderLocal {
		return config.GroupConfig{}, false, fmt.Errorf("local inventory group must use local/<group>")
	}
	groupCfg, ok := cfg.Inventory.ProviderGroup(provider, group)
	return groupCfg, ok, nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return filepath.Clean(absA) == filepath.Clean(absB)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func applyHostPatch(host *sshconfig.HostEntry, patch hostPatch) {
	deleteDirective(host, "User")
	delete(host.Properties, "user")
	if patch.HostName != "" {
		upsertDirective(host, "HostName", patch.HostName)
		host.HostName = patch.HostName
		host.Properties["hostname"] = patch.HostName
	}
	if patch.PortSet {
		upsertDirective(host, "Port", fmt.Sprintf("%d", patch.Port))
		host.Properties["port"] = fmt.Sprintf("%d", patch.Port)
	}
}

func deleteDirective(host *sshconfig.HostEntry, key string) {
	if host == nil {
		return
	}
	re := regexp.MustCompile(`(?i)^\s*` + regexp.QuoteMeta(key) + `\s+`)
	lines := host.Lines[:0]
	for _, line := range host.Lines {
		if re.MatchString(line) {
			continue
		}
		lines = append(lines, line)
	}
	host.Lines = lines
}

func upsertDirective(host *sshconfig.HostEntry, key, value string) {
	line := fmt.Sprintf("  %s %s\n", key, value)
	re := regexp.MustCompile(`(?i)^\s*` + regexp.QuoteMeta(key) + `\s+`)
	for i, existing := range host.Lines {
		if re.MatchString(existing) {
			host.Lines[i] = line
			return
		}
	}
	insertAt := 1
	if len(host.Lines) > 1 {
		insertAt = len(host.Lines)
		if strings.TrimSpace(host.Lines[len(host.Lines)-1]) == "" {
			insertAt = len(host.Lines) - 1
		}
	}
	host.Lines = append(host.Lines[:insertAt], append([]string{line}, host.Lines[insertAt:]...)...)
}

func cloneHostEntry(host *sshconfig.HostEntry) *sshconfig.HostEntry {
	clone := *host
	clone.Patterns = append([]string(nil), host.Patterns...)
	clone.Lines = append([]string(nil), host.Lines...)
	clone.Properties = make(map[string]string, len(host.Properties))
	for key, value := range host.Properties {
		clone.Properties[key] = value
	}
	return &clone
}

func writeParsedConfig(parser *sshconfig.Parser, parsed *sshconfig.ParsedConfig, paths *config.Paths) error {
	if err := backupFile(parsed.Path, paths.BackupDir); err != nil {
		return err
	}
	return parser.WriteFile(parsed)
}

func backupFile(srcPath, backupDir string) error {
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open backup source: %w", err)
	}
	defer func() { _ = src.Close() }()

	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", filepath.Base(srcPath), time.Now().Format("20060102_150405")))
	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("create backup: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("write backup: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close backup: %w", err)
	}
	return pruneLocalBackups(backupDir, filepath.Base(srcPath), time.Now())
}

type localBackupInfo struct {
	name string
	path string
	when time.Time
}

func pruneLocalBackups(backupDir, sourceBase string, now time.Time) error {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read backup dir: %w", err)
	}

	backups := make([]localBackupInfo, 0, len(entries))
	for _, entry := range entries {
		if backup, ok := localBackupFromEntry(backupDir, sourceBase, entry, now.Location()); ok {
			backups = append(backups, backup)
		}
	}
	if len(backups) <= 1 {
		return nil
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].when.Equal(backups[j].when) {
			return backups[i].name > backups[j].name
		}
		return backups[i].when.After(backups[j].when)
	})

	keep := make(map[string]bool)
	keepCount := func(backup localBackupInfo) {
		keep[backup.name] = true
	}

	hourCutoff := now.Add(-time.Hour)
	dayCutoff := now.Add(-24 * time.Hour)
	dailyStart := startOfLocalDay(now).AddDate(0, 0, -localBackupDayHistory)

	hourly := 0
	for _, backup := range backups {
		if backup.when.After(now) || backup.when.Before(hourCutoff) || hourly >= localBackupHourlyKeep {
			continue
		}
		keepCount(backup)
		hourly++
	}

	daily := 0
	for _, backup := range backups {
		if keep[backup.name] || backup.when.After(now) || !backup.when.Before(hourCutoff) || backup.when.Before(dayCutoff) || daily >= localBackupDailyKeep {
			continue
		}
		keepCount(backup)
		daily++
	}

	seenDays := make(map[string]bool)
	for _, backup := range backups {
		if keep[backup.name] || backup.when.After(now) || !backup.when.Before(dayCutoff) || backup.when.Before(dailyStart) {
			continue
		}
		day := backup.when.In(now.Location()).Format("2006-01-02")
		if seenDays[day] {
			continue
		}
		keepCount(backup)
		seenDays[day] = true
	}

	if len(keep) == 0 {
		keepCount(backups[0])
	}

	for _, backup := range backups {
		if keep[backup.name] {
			continue
		}
		if err := os.Remove(backup.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prune backup %s: %w", backup.name, err)
		}
	}
	return nil
}

func localBackupFromEntry(backupDir, sourceBase string, entry os.DirEntry, loc *time.Location) (localBackupInfo, bool) {
	if entry.IsDir() {
		return localBackupInfo{}, false
	}
	name := entry.Name()
	prefix := sourceBase + "."
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".bak") {
		return localBackupInfo{}, false
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".bak")
	when, err := time.ParseInLocation(localBackupTimestampLayout, stamp, loc)
	if err != nil {
		return localBackupInfo{}, false
	}
	return localBackupInfo{
		name: name,
		path: filepath.Join(backupDir, name),
		when: when,
	}, true
}

func startOfLocalDay(t time.Time) time.Time {
	local := t.In(t.Location())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

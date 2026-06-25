package self

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type sshImportBlock struct {
	Patterns   []string
	Directives []sshImportDirective
}

type sshImportDirective struct {
	Key   string
	Value string
	Line  int
}

type sshImportInclude struct {
	Pattern string
	Line    int
}

type sshImportPrompter interface {
	Confirm(title string, defaultValue bool) (bool, error)
	InputWithDefault(title, defaultValue string) (string, error)
}

type sshImportDescriptionPrompter interface {
	ConfirmWithDescription(title, description string, defaultValue bool) (bool, error)
}

type sshImportReviewPrompter interface {
	Review(title, body string, defaultValue bool) (bool, error)
}

type uiSSHImportPrompter struct{}

func (uiSSHImportPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	return ui.Confirm(title, defaultValue)
}

func (uiSSHImportPrompter) ConfirmWithDescription(title, description string, defaultValue bool) (bool, error) {
	return ui.ConfirmWithDescription(title, description, defaultValue)
}

func (uiSSHImportPrompter) Review(title, body string, defaultValue bool) (bool, error) {
	return ui.ReviewText(title, body, defaultValue)
}

func (uiSSHImportPrompter) InputWithDefault(title, defaultValue string) (string, error) {
	return ui.InputWithDefault(title, defaultValue)
}

// NewImportCmd creates the import command group.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import external configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(NewImportSSHConfigCmd())
	return cmd
}

// NewImportSSHConfigCmd creates the OpenSSH config import command.
func NewImportSSHConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh-config",
		Short: "Import OpenSSH config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImportSSHConfig(config.DefaultPaths(), uiSSHImportPrompter{}, cmd.OutOrStdout())
		},
	}
	return cmd
}

func runImportSSHConfig(paths *config.Paths, prompter sshImportPrompter, out io.Writer) error {
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if prompter == nil {
		prompter = uiSSHImportPrompter{}
	}
	if out == nil {
		out = io.Discard
	}

	inventoryTarget := filepath.Join(paths.ConfigDir, "inventory", "local.yaml")
	importer := newSSHConfigImporter(prompter, paths.ConfigFile, inventoryTarget)
	if err := importer.importRoot(paths.SSHConfigFile); err != nil {
		return err
	}
	for _, warning := range importer.warnings {
		fmt.Fprintf(out, "warning: %s\n", warning)
	}
	for _, path := range importer.written {
		fmt.Fprintf(out, "wrote %s\n", path)
	}
	return nil
}

type sshConfigImporter struct {
	prompter        sshImportPrompter
	cfg             *config.Config
	local           config.InventoryProviderConfig
	seen            map[string]bool
	warnings        []string
	defaultsTarget  string
	inventoryTarget string
	written         []string
}

func newSSHConfigImporter(prompter sshImportPrompter, defaultsTarget, inventoryTarget string) *sshConfigImporter {
	providerCfg := config.InventoryProviderConfig{
		Type:   config.ProviderLocal,
		Groups: make(map[string]config.GroupConfig),
		Hosts:  make(map[string]config.InventoryHostConfig),
	}
	return &sshConfigImporter{
		prompter: prompter,
		cfg: &config.Config{
			Inventory: config.InventoryConfig{
				Providers: map[string]config.InventoryProviderConfig{
					config.ProviderLocal: providerCfg,
				},
			},
		},
		local:           providerCfg,
		seen:            make(map[string]bool),
		defaultsTarget:  defaultsTarget,
		inventoryTarget: inventoryTarget,
	}
}

func (i *sshConfigImporter) config() *config.Config {
	i.cfg.Inventory.Providers[config.ProviderLocal] = i.local
	return i.cfg
}

func (i *sshConfigImporter) importRoot(path string) error {
	return i.importFile(path, true, true)
}

func (i *sshConfigImporter) importFile(path string, root bool, defaultValue bool) error {
	path = expandHome(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if i.seen[path] {
		return nil
	}
	i.seen[path] = true

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open ssh config %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	blocks, includes, warnings, err := parseSSHImportFile(f)
	if err != nil {
		return err
	}
	i.warnings = append(i.warnings, prefixSSHImportWarnings(path, warnings)...)

	defaultBlocks, hostBlocks := splitSSHImportBlocks(blocks)
	if len(defaultBlocks) > 0 {
		if err := i.reviewAndWriteDefaults(path, defaultBlocks, defaultValue); err != nil {
			return err
		}
	}
	if len(hostBlocks) > 0 {
		if err := i.reviewAndWriteHosts(path, hostBlocks, defaultValue); err != nil {
			return err
		}
	}
	for _, include := range includes {
		matches, err := expandSSHImportInclude(path, include.Pattern)
		if err != nil {
			i.warnings = append(i.warnings, fmt.Sprintf("%s:%d include %q: %v", path, include.Line, include.Pattern, err))
			continue
		}
		for _, match := range matches {
			confirmed := root
			if !root {
				confirmed = false
			}
			if err := i.importFile(match, false, confirmed); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *sshConfigImporter) reviewAndWriteDefaults(source string, blocks []sshImportBlock, defaultValue bool) error {
	oldText, err := readTextIfExists(i.defaultsTarget)
	if err != nil {
		return err
	}
	rootCfg, err := loadRootConfigOnly(i.defaultsTarget)
	if err != nil {
		return err
	}
	var auth config.InventoryAuthConfig
	var warnings []string
	for _, block := range blocks {
		applySSHImportDirectives(&rootCfg.SSH.Defaults, &auth, block.Directives, &warnings)
	}
	i.warnings = append(i.warnings, prefixSSHImportWarnings(source, warnings)...)
	newText, err := marshalRootSSHDefaults(oldText, rootCfg.SSH.Defaults)
	if err != nil {
		return err
	}
	return i.reviewAndWritePending("Import SSH defaults from "+source+"?", []sshImportPendingWrite{
		{path: i.defaultsTarget, oldText: oldText, newText: newText},
	}, defaultValue)
}

func (i *sshConfigImporter) reviewAndWriteHosts(source string, blocks []sshImportBlock, defaultValue bool) error {
	hosts := i.hostConfigsFromBlocks(blocks)
	if len(hosts) == 0 {
		return nil
	}
	oldText, err := readTextIfExists(i.inventoryTarget)
	if err != nil {
		return err
	}
	newText, err := i.marshalLocalInventoryHosts(oldText, hosts)
	if err != nil {
		return err
	}
	return i.reviewAndWritePending("Import SSH hosts from "+source+"?", []sshImportPendingWrite{
		{path: i.inventoryTarget, oldText: oldText, newText: newText},
	}, defaultValue)
}

type sshImportPendingWrite struct {
	path    string
	oldText string
	newText string
}

func loadRootConfigOnly(path string) (*config.Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &config.Config{}, nil
		}
		return nil, fmt.Errorf("read root config %s: %w", path, err)
	}
	cfg := &config.Config{}
	if err := yaml.Unmarshal(content, cfg); err != nil {
		return nil, fmt.Errorf("parse root config %s: %w", path, err)
	}
	return cfg, nil
}

func writeConfigText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func marshalRootSSHDefaults(oldText string, defaults config.SSHHostConfig) (string, error) {
	defaultsText, err := marshalIndentedSSHDefaults(defaults)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(oldText) == "" {
		return "ssh:\n" + defaultsText, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(oldText), &root); err != nil {
		return "", err
	}
	rootMap, err := rootMappingNode(&root)
	if err != nil {
		return "", err
	}
	sshKey, ssh := mappingEntry(rootMap, "ssh")
	if ssh == nil || ssh.Kind != yaml.MappingNode {
		return appendRootSSHDefaults(oldText, defaultsText), nil
	}
	lines := strings.SplitAfter(oldText, "\n")
	if defaultsKey, _ := mappingEntry(ssh, "defaults"); defaultsKey != nil {
		endLine := nextMappingKeyLine(ssh, defaultsKey.Line)
		if endLine == 0 {
			endLine = nextMappingKeyLine(rootMap, sshKey.Line)
		}
		if endLine == 0 {
			endLine = len(lines) + 1
		}
		return replaceLines(lines, defaultsKey.Line, endLine, defaultsText), nil
	}
	insertLine := nextMappingKeyLine(rootMap, sshKey.Line)
	if insertLine == 0 {
		insertLine = len(lines) + 1
	}
	return insertLines(lines, insertLine, defaultsText), nil
}

func rootMappingNode(root *yaml.Node) (*yaml.Node, error) {
	if root.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("root config is not a YAML document")
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("root config must be a YAML mapping")
	}
	if root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("root config must be a YAML mapping")
	}
	return root.Content[0], nil
}

func mappingEntry(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for idx := 0; idx+1 < len(mapping.Content); idx += 2 {
		if mapping.Content[idx].Value == key {
			return mapping.Content[idx], mapping.Content[idx+1]
		}
	}
	return nil, nil
}

func nextMappingKeyLine(mapping *yaml.Node, afterLine int) int {
	for idx := 0; idx+1 < len(mapping.Content); idx += 2 {
		line := mapping.Content[idx].Line
		if line > afterLine {
			return line
		}
	}
	return 0
}

func marshalIndentedSSHDefaults(defaults config.SSHHostConfig) (string, error) {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any{"defaults": sshHostConfigImportMap(defaults)}); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(b.String(), "\n") {
		if line == "" {
			continue
		}
		out.WriteString("  ")
		out.WriteString(line)
	}
	return out.String(), nil
}

func appendRootSSHDefaults(oldText, defaultsText string) string {
	lines := strings.SplitAfter(oldText, "\n")
	return insertLines(lines, len(lines)+1, "ssh:\n"+defaultsText)
}

func replaceLines(lines []string, startLine, endLine int, replacement string) string {
	start := max(startLine-1, 0)
	end := min(max(endLine-1, start), len(lines))
	updated := make([]string, 0, len(lines)-(end-start)+1)
	updated = append(updated, lines[:start]...)
	updated = append(updated, replacement)
	updated = append(updated, lines[end:]...)
	return strings.Join(updated, "")
}

func insertLines(lines []string, line int, insertion string) string {
	idx := min(max(line-1, 0), len(lines))
	if idx == len(lines) && len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
		lines[len(lines)-1] += "\n"
	}
	updated := make([]string, 0, len(lines)+1)
	updated = append(updated, lines[:idx]...)
	updated = append(updated, insertion)
	updated = append(updated, lines[idx:]...)
	return strings.Join(updated, "")
}

func spliceLocalInventoryHosts(oldText string, hosts map[string]config.InventoryHostConfig) (string, bool, error) {
	hostText, err := marshalIndentedLocalHosts(hosts)
	if err != nil {
		return "", false, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(oldText), &root); err != nil {
		return "", false, err
	}
	rootMap, err := rootMappingNode(&root)
	if err != nil {
		return "", false, err
	}
	_, inventory := mappingEntry(rootMap, "inventory")
	if inventory == nil || inventory.Kind != yaml.MappingNode {
		return "", false, nil
	}
	_, providers := mappingEntry(inventory, "providers")
	if providers == nil || providers.Kind != yaml.MappingNode {
		return "", false, nil
	}
	localKey, local := mappingEntry(providers, config.ProviderLocal)
	if local == nil || local.Kind != yaml.MappingNode {
		return "", false, nil
	}
	lines := strings.SplitAfter(oldText, "\n")
	if hostsKey, hostsNode := mappingEntry(local, "hosts"); hostsKey != nil && hostsNode.Kind == yaml.MappingNode {
		insertLine := nextMappingKeyLine(local, hostsKey.Line)
		if insertLine == 0 {
			insertLine = nextMappingKeyLine(providers, localKey.Line)
		}
		if insertLine == 0 {
			insertLine = nextMappingKeyLine(inventory, localKey.Line)
		}
		if insertLine == 0 {
			insertLine = nextMappingKeyLine(rootMap, localKey.Line)
		}
		if insertLine == 0 {
			insertLine = len(lines) + 1
		}
		return insertLines(lines, insertLine, hostText), true, nil
	}
	insertLine := nextMappingKeyLine(providers, localKey.Line)
	if insertLine == 0 {
		insertLine = nextMappingKeyLine(inventory, localKey.Line)
	}
	if insertLine == 0 {
		insertLine = nextMappingKeyLine(rootMap, localKey.Line)
	}
	if insertLine == 0 {
		insertLine = len(lines) + 1
	}
	return insertLines(lines, insertLine, "      hosts:\n"+hostText), true, nil
}

func marshalIndentedLocalHosts(hosts map[string]config.InventoryHostConfig) (string, error) {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(hosts); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(b.String(), "\n") {
		if line == "" {
			continue
		}
		out.WriteString("        ")
		out.WriteString(line)
	}
	return out.String(), nil
}

func sshHostConfigImportMap(cfg config.SSHHostConfig) map[string]any {
	table := make(map[string]any)
	compatibility := make(map[string]any)
	addImportString(compatibility, "kex", cfg.Compatibility.Kex)
	addImportString(compatibility, "mac", cfg.Compatibility.MAC)
	addImportString(compatibility, "host_key", cfg.Compatibility.HostKey)
	addImportString(compatibility, "public_key", cfg.Compatibility.PublicKey)
	if len(compatibility) > 0 {
		table["compatibility"] = compatibility
	}
	if options := cfg.Options; len(options) > 0 {
		table["options"] = options
	}
	return table
}

func addImportString(table map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		table[key] = value
	}
}

func readTextIfExists(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(content), nil
}

func (i *sshConfigImporter) mergedLocalProvider(path string) (config.InventoryProviderConfig, error) {
	return i.mergedLocalProviderWithHosts(path, i.local.Groups, i.local.Hosts)
}

func (i *sshConfigImporter) marshalLocalInventoryHosts(oldText string, hosts map[string]config.InventoryHostConfig) (string, error) {
	added := make(map[string]config.InventoryHostConfig)
	existing, ok, err := loadLocalProviderOnly(i.inventoryTarget)
	if err != nil && strings.TrimSpace(oldText) != "" {
		return "", err
	}
	if ok {
		for host, hostCfg := range hosts {
			if _, exists := existing.Hosts[host]; exists {
				i.warnings = append(i.warnings, fmt.Sprintf("skipping existing local host %q", host))
				continue
			}
			added[host] = hostCfg
		}
	} else {
		added = hosts
	}
	if len(added) == 0 {
		return oldText, nil
	}
	if strings.TrimSpace(oldText) == "" {
		provider := config.InventoryProviderConfig{
			Type:  config.ProviderLocal,
			Hosts: added,
		}
		return marshalLocalInventoryProvider(provider)
	}
	spliced, ok, err := spliceLocalInventoryHosts(oldText, added)
	if err != nil {
		return "", err
	}
	if ok {
		return spliced, nil
	}
	merged, err := i.mergedLocalProviderWithHosts(i.inventoryTarget, nil, added)
	if err != nil {
		return "", err
	}
	return marshalLocalInventoryProvider(merged)
}

func (i *sshConfigImporter) mergedLocalProviderWithHosts(path string, groups map[string]config.GroupConfig, hosts map[string]config.InventoryHostConfig) (config.InventoryProviderConfig, error) {
	merged := config.InventoryProviderConfig{
		Type:   config.ProviderLocal,
		Groups: make(map[string]config.GroupConfig),
		Hosts:  make(map[string]config.InventoryHostConfig),
	}
	if _, err := os.Stat(path); err == nil {
		existing, ok, err := loadLocalProviderOnly(path)
		if err != nil {
			return merged, err
		}
		if ok {
			merged = existing
			if merged.Type == "" {
				merged.Type = config.ProviderLocal
			}
			if merged.Groups == nil {
				merged.Groups = make(map[string]config.GroupConfig)
			}
			if merged.Hosts == nil {
				merged.Hosts = make(map[string]config.InventoryHostConfig)
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return merged, fmt.Errorf("stat %s: %w", path, err)
	}
	for group, groupCfg := range groups {
		if _, exists := merged.Groups[group]; !exists {
			merged.Groups[group] = groupCfg
		}
	}
	for host, hostCfg := range hosts {
		if _, exists := merged.Hosts[host]; exists {
			i.warnings = append(i.warnings, fmt.Sprintf("skipping existing local host %q", host))
			continue
		}
		merged.Hosts[host] = hostCfg
	}
	return merged, nil
}

func loadLocalProviderOnly(path string) (config.InventoryProviderConfig, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return config.InventoryProviderConfig{}, false, fmt.Errorf("read local inventory %s: %w", path, err)
	}
	cfg := &config.Config{}
	if err := yaml.Unmarshal(content, cfg); err != nil {
		return config.InventoryProviderConfig{}, false, fmt.Errorf("parse local inventory %s: %w", path, err)
	}
	provider, ok := cfg.Inventory.Providers[config.ProviderLocal]
	if !ok {
		provider, ok = cfg.Inventory.Provider[config.ProviderLocal]
	}
	return provider, ok, nil
}

func marshalLocalInventoryProvider(provider config.InventoryProviderConfig) (string, error) {
	provider.Group = nil
	providerMap := map[string]any{"type": provider.Type}
	if len(provider.Groups) > 0 {
		providerMap["groups"] = provider.Groups
	}
	if len(provider.Hosts) > 0 {
		providerMap["hosts"] = provider.Hosts
	}
	root := map[string]any{
		"inventory": map[string]any{
			"providers": map[string]any{
				config.ProviderLocal: providerMap,
			},
		},
	}
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func (i *sshConfigImporter) reviewAndWritePending(title string, pending []sshImportPendingWrite, defaultValue bool) error {
	var diffText strings.Builder
	var changed []sshImportPendingWrite
	for _, write := range pending {
		if write.oldText == write.newText {
			continue
		}
		diffText.WriteString(udiff.Unified(write.path, write.path, write.oldText, write.newText))
		if !strings.HasSuffix(diffText.String(), "\n") {
			diffText.WriteByte('\n')
		}
		changed = append(changed, write)
	}
	if len(changed) == 0 {
		return nil
	}
	approved, err := i.review(title, strings.TrimRight(diffText.String(), "\n")+"\n", defaultValue)
	if err != nil {
		return err
	}
	if !approved {
		return nil
	}
	for _, write := range changed {
		if err := writeConfigText(write.path, write.newText); err != nil {
			return err
		}
		i.written = append(i.written, write.path)
	}
	return nil
}

func (i *sshConfigImporter) review(title, body string, defaultValue bool) (bool, error) {
	if prompter, ok := i.prompter.(sshImportReviewPrompter); ok {
		return prompter.Review(title, body, defaultValue)
	}
	if prompter, ok := i.prompter.(sshImportDescriptionPrompter); ok {
		return prompter.ConfirmWithDescription(title, body, defaultValue)
	}
	return i.prompter.Confirm(title, defaultValue)
}

func (i *sshConfigImporter) applyHostBlocks(blocks []sshImportBlock) {
	if i.local.Hosts == nil {
		i.local.Hosts = make(map[string]config.InventoryHostConfig)
	}
	for hostName, host := range i.hostConfigsFromBlocks(blocks) {
		i.local.Hosts[hostName] = host
	}
}

func (i *sshConfigImporter) hostConfigsFromBlocks(blocks []sshImportBlock) map[string]config.InventoryHostConfig {
	hosts := make(map[string]config.InventoryHostConfig)
	existing := i.existingInventoryHostIndex()
	for _, block := range blocks {
		hostName := block.Patterns[0]
		if shouldSkipHostPattern(hostName) {
			i.warnings = append(i.warnings, fmt.Sprintf("skipping unsupported Host pattern %q", hostName))
			continue
		}
		host := config.InventoryHostConfig{}
		aliases := block.Patterns[1:]
		if target, ok := sshImportDirectiveValue(block.Directives, "hostname"); ok {
			if shouldSkipHostPattern(target) {
				i.warnings = append(i.warnings, fmt.Sprintf("skipping unsupported HostName %q for %s", target, hostName))
			} else if strings.TrimSpace(target) != "" {
				aliases = block.Patterns
				hostName = target
			}
		}
		for _, alias := range aliases {
			if shouldSkipHostPattern(alias) {
				i.warnings = append(i.warnings, fmt.Sprintf("skipping unsupported Host alias %q for %s", alias, hostName))
				continue
			}
			if alias == hostName {
				continue
			}
			host.Aliases = append(host.Aliases, alias)
		}
		applySSHImportHostDirectives(&host, block.Directives)
		applySSHImportDirectives(&host.SSH, &host.Auth, block.Directives, &i.warnings)
		if host.Auth.Mode == "" && host.Auth.Username != "" {
			host.Auth.Mode = config.AuthModePassword
		}
		omitPasswordAuthTransportOptions(&host)
		host.Group = i.inferLocalGroup(hostName)
		if match := existing.matchImportedHost(hostName, host); match != nil {
			i.warnings = append(i.warnings, fmt.Sprintf("skipping imported host %q: %s %q already exists in inventory host %q", hostName, match.field, match.value, match.host))
			continue
		}
		hosts[hostName] = host
	}
	return hosts
}

func sshImportDirectiveValue(directives []sshImportDirective, key string) (string, bool) {
	for _, directive := range directives {
		if strings.EqualFold(directive.Key, key) {
			return strings.TrimSpace(directive.Value), true
		}
	}
	return "", false
}

type sshImportExistingHostIndex struct {
	byToken map[string]sshImportExistingHostMatch
}

type sshImportExistingHostMatch struct {
	host  string
	field string
	value string
}

func (i *sshConfigImporter) existingInventoryHostIndex() sshImportExistingHostIndex {
	index := sshImportExistingHostIndex{byToken: make(map[string]sshImportExistingHostMatch)}
	if cfg, err := config.Load(i.defaultsTarget); err == nil {
		index.addInventoryConfig(cfg.Inventory)
	}
	if provider, ok, err := loadLocalProviderOnly(i.inventoryTarget); err == nil && ok {
		index.addProvider(provider)
	}
	index.addProviderStates()
	return index
}

func (idx sshImportExistingHostIndex) addInventoryConfig(inv config.InventoryConfig) {
	for host, hostCfg := range inv.Hosts {
		idx.addHost(host, hostCfg)
	}
	for host, hostCfg := range inv.Host {
		idx.addHost(host, hostCfg)
	}
	for _, provider := range inv.Providers {
		idx.addProvider(provider)
	}
	for _, provider := range inv.Provider {
		idx.addProvider(provider)
	}
}

func (idx sshImportExistingHostIndex) addProvider(provider config.InventoryProviderConfig) {
	for host, hostCfg := range provider.Hosts {
		idx.addHost(host, hostCfg)
	}
}

func (idx sshImportExistingHostIndex) addProviderStates() {
	providers, err := inventory.ListProviderStates()
	if err != nil {
		return
	}
	for _, providerName := range providers {
		state, err := inventory.LoadProviderState(providerName)
		if err != nil || state == nil {
			continue
		}
		for _, host := range state.Objects {
			idx.addProviderStateHost(providerName, host)
		}
	}
}

func (idx sshImportExistingHostIndex) addHost(host string, hostCfg config.InventoryHostConfig) {
	idx.addToken(host, sshImportExistingHostMatch{host: host, field: "host", value: host})
	for _, alias := range hostCfg.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		idx.addToken(alias, sshImportExistingHostMatch{host: host, field: "alias", value: alias})
	}
}

func (idx sshImportExistingHostIndex) addProviderStateHost(providerName string, host *inventory.ProviderHost) {
	if host == nil {
		return
	}
	hostLabel := strings.TrimSpace(providerName + "/" + host.Host)
	idx.addToken(host.Host, sshImportExistingHostMatch{host: hostLabel, field: "host", value: host.Host})
	if hostname := strings.TrimSpace(host.HostName); hostname != "" {
		idx.addToken(hostname, sshImportExistingHostMatch{host: hostLabel, field: "hostname", value: hostname})
	}
	for _, pattern := range host.Patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || pattern == host.Host {
			continue
		}
		idx.addToken(pattern, sshImportExistingHostMatch{host: hostLabel, field: "alias", value: pattern})
	}
}

func (idx sshImportExistingHostIndex) addToken(token string, match sshImportExistingHostMatch) {
	if idx.byToken == nil {
		idx.byToken = make(map[string]sshImportExistingHostMatch)
	}
	token = normalizeSSHImportHostToken(token)
	if token == "" {
		return
	}
	if _, exists := idx.byToken[token]; !exists {
		idx.byToken[token] = match
	}
}

func (idx sshImportExistingHostIndex) matchImportedHost(hostName string, host config.InventoryHostConfig) *sshImportExistingHostMatch {
	for _, candidate := range append([]string{hostName}, host.Aliases...) {
		if match, ok := idx.byToken[normalizeSSHImportHostToken(candidate)]; ok {
			return &match
		}
	}
	return nil
}

func normalizeSSHImportHostToken(token string) string {
	return strings.ToLower(strings.TrimSpace(token))
}

func (i *sshConfigImporter) inferLocalGroup(hostName string) string {
	provider, ok, err := loadLocalProviderOnly(i.inventoryTarget)
	if err != nil || !ok {
		return ""
	}
	if len(provider.Groups) == 0 && len(provider.Group) > 0 {
		provider.Groups = provider.Group
	}
	var matched string
	for groupName, groupCfg := range provider.Groups {
		if localGroupMatchesHost(groupCfg, hostName) {
			if matched != "" {
				return ""
			}
			matched = groupName
		}
	}
	return matched
}

func localGroupMatchesHost(group config.GroupConfig, hostName string) bool {
	target := strings.TrimSpace(hostName)
	if target == "" {
		return false
	}
	for _, suffix := range group.DomainSuffix {
		if hostMatchesDomainSuffix(target, suffix) {
			return true
		}
	}
	for _, suffix := range group.Match["domain_suffix"] {
		if hostMatchesDomainSuffix(target, suffix) {
			return true
		}
	}
	return false
}

func hostMatchesDomainSuffix(hostname, suffix string) bool {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	suffix = strings.ToLower(strings.TrimSpace(suffix))
	if hostname == "" || suffix == "" {
		return false
	}
	if !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	return strings.HasSuffix(hostname, suffix)
}

func importSSHConfigText(r io.Reader, group string) (string, []string, error) {
	blocks, warnings, err := parseSSHImportBlocks(r)
	if err != nil {
		return "", warnings, err
	}
	importer := newSSHConfigImporter(nil, "", "")
	defaultBlocks, hostBlocks := splitSSHImportBlocks(blocks)
	for _, block := range defaultBlocks {
		applySSHImportDirectives(&importer.cfg.SSH.Defaults, &importer.cfg.Inventory.Auth, block.Directives, &warnings)
	}
	importer.applyHostBlocks(hostBlocks)
	importer.warnings = append(importer.warnings, warnings...)
	text, err := config.MarshalSparse(importer.config())
	if err != nil {
		return "", warnings, err
	}
	return text, importer.warnings, nil
}

func parseSSHImportBlocks(r io.Reader) ([]sshImportBlock, []string, error) {
	blocks, _, warnings, err := parseSSHImportFile(r)
	return blocks, warnings, err
}

func parseSSHImportFile(r io.Reader) ([]sshImportBlock, []sshImportInclude, []string, error) {
	var blocks []sshImportBlock
	var includes []sshImportInclude
	var warnings []string
	var current *sshImportBlock
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := splitSSHDirective(line)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d ignored: %s", lineNo, line))
			continue
		}
		if strings.EqualFold(key, "Host") {
			block := sshImportBlock{Patterns: strings.Fields(cleanSSHValue(value))}
			blocks = append(blocks, block)
			current = &blocks[len(blocks)-1]
			continue
		}
		if strings.EqualFold(key, "Include") {
			includes = append(includes, sshImportInclude{Pattern: value, Line: lineNo})
			current = nil
			continue
		}
		if strings.EqualFold(key, "Match") {
			warnings = append(warnings, fmt.Sprintf("line %d %s directive is not imported", lineNo, key))
			current = nil
			continue
		}
		if current == nil {
			if shouldWarnUnsupportedDirective(key) {
				warnings = append(warnings, fmt.Sprintf("line %d %s directive is not imported", lineNo, key))
			}
			continue
		}
		current.Directives = append(current.Directives, sshImportDirective{Key: key, Value: cleanSSHValue(value), Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, warnings, err
	}
	return blocks, includes, warnings, nil
}

func splitSSHDirective(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	key := fields[0]
	value := strings.TrimSpace(line[len(key):])
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

func splitSSHImportBlocks(blocks []sshImportBlock) ([]sshImportBlock, []sshImportBlock) {
	var defaults []sshImportBlock
	var hosts []sshImportBlock
	for _, block := range blocks {
		if len(block.Patterns) == 0 {
			continue
		}
		if isGlobalPattern(block.Patterns) {
			defaults = append(defaults, block)
			continue
		}
		hosts = append(hosts, block)
	}
	return defaults, hosts
}

func prefixSSHImportWarnings(path string, warnings []string) []string {
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, path+": "+warning)
	}
	return out
}

func expandSSHImportInclude(fromFile, pattern string) ([]string, error) {
	var matches []string
	for _, part := range strings.Fields(pattern) {
		part = expandHome(cleanSSHValue(part))
		if !filepath.IsAbs(part) {
			part = filepath.Join(filepath.Dir(fromFile), part)
		}
		globMatches, err := filepath.Glob(part)
		if err != nil {
			return nil, err
		}
		if len(globMatches) == 0 {
			continue
		}
		for _, match := range globMatches {
			if abs, err := filepath.Abs(match); err == nil {
				match = abs
			}
			matches = append(matches, match)
		}
	}
	return matches, nil
}

func cleanSSHValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return unquoted
		}
		return strings.Trim(value, `"'`)
	}
	return value
}

func applySSHImportHostDirectives(host *config.InventoryHostConfig, directives []sshImportDirective) {
	for _, directive := range directives {
		switch strings.ToLower(directive.Key) {
		case "port":
			if n, err := strconv.Atoi(directive.Value); err == nil {
				host.Port = n
			}
		}
	}
}

func applySSHImportDirectives(sshCfg *config.SSHHostConfig, auth *config.InventoryAuthConfig, directives []sshImportDirective, warnings *[]string) {
	for _, directive := range directives {
		key := directive.Key
		value := directive.Value
		switch strings.ToLower(key) {
		case "hostname":
			// HostName is promoted to the inventory host key when possible.
			continue
		case "user":
			auth.Username = value
		case "port":
			// Port is handled by the caller because it is inventory metadata.
			continue
		case "proxyjump":
			addSSHImportOption(sshCfg, "ProxyJump", value)
		case "proxycommand":
			addSSHImportOption(sshCfg, "ProxyCommand", value)
		case "identityagent":
			addSSHImportOption(sshCfg, "IdentityAgent", value)
		case "identityfile":
			addSSHImportOptionItem(sshCfg, "IdentityFile", value)
		case "certificatefile":
			addSSHImportOptionItem(sshCfg, "CertificateFile", value)
		case "identitiesonly":
			if b, ok := parseSSHBool(value); ok {
				addSSHImportOptionBool(sshCfg, "IdentitiesOnly", b)
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "forwardagent":
			if b, ok := parseSSHBool(value); ok {
				addSSHImportOptionBool(sshCfg, "ForwardAgent", b)
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "batchmode":
			addSSHImportBoolOptionOrString(sshCfg, "BatchMode", value)
		case "challengeresponseauthentication":
			addSSHImportBoolOptionOrString(sshCfg, "ChallengeResponseAuthentication", value)
		case "compression":
			addSSHImportBoolOptionOrString(sshCfg, "Compression", value)
		case "gssapiauthentication":
			addSSHImportBoolOptionOrString(sshCfg, "GSSAPIAuthentication", value)
		case "kbdinteractiveauthentication":
			addSSHImportBoolOptionOrString(sshCfg, "KbdInteractiveAuthentication", value)
		case "passwordauthentication":
			addSSHImportBoolOptionOrString(sshCfg, "PasswordAuthentication", value)
		case "pubkeyauthentication":
			addSSHImportBoolOptionOrString(sshCfg, "PubkeyAuthentication", value)
		case "tcpkeepalive":
			addSSHImportBoolOptionOrString(sshCfg, "TCPKeepAlive", value)
		case "localforward":
			addSSHImportOptionItem(sshCfg, "LocalForward", value)
		case "remoteforward":
			addSSHImportOptionItem(sshCfg, "RemoteForward", value)
		case "setenv":
			for _, item := range strings.Fields(value) {
				k, v, ok := strings.Cut(item, "=")
				if ok && k != "" {
					addSSHImportOptionMapValue(sshCfg, "SetEnv", k, v)
				}
			}
		case "remotecommand":
			addSSHImportOption(sshCfg, "RemoteCommand", value)
		case "serveraliveinterval":
			if d, ok := parseSSHDuration(value); ok {
				addSSHImportOption(sshCfg, "ServerAliveInterval", formatSSHImportDuration(value, d))
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "serveralivecountmax":
			if n, err := strconv.Atoi(value); err == nil {
				addSSHImportOption(sshCfg, "ServerAliveCountMax", strconv.Itoa(n))
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "connecttimeout":
			if d, ok := parseSSHDuration(value); ok {
				addSSHImportOption(sshCfg, "ConnectTimeout", formatSSHImportDuration(value, d))
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "controlmaster":
			addSSHImportOption(sshCfg, "ControlMaster", value)
		case "controlpersist":
			if d, ok := parseSSHDuration(value); ok {
				addSSHImportOption(sshCfg, "ControlPersist", formatSSHImportDuration(value, d))
			} else {
				addSSHImportOption(sshCfg, key, value)
			}
		case "controlpath":
			addSSHImportOption(sshCfg, "ControlPath", value)
		case "ciphers":
			addSSHImportOptionItems(sshCfg, "Ciphers", splitCSV(value))
		case "macs":
			addSSHImportOptionItems(sshCfg, "MACs", splitCSV(value))
		case "kexalgorithms":
			addSSHImportOptionItems(sshCfg, "KexAlgorithms", splitCSV(value))
		case "hostkeyalgorithms":
			addSSHImportOptionItems(sshCfg, "HostKeyAlgorithms", splitCSV(value))
		case "pubkeyacceptedalgorithms":
			addSSHImportOptionItems(sshCfg, "PubkeyAcceptedAlgorithms", splitCSV(value))
		case "canonicalizehostname":
			*warnings = append(*warnings, fmt.Sprintf("line %d %s directive is not imported", directive.Line, key))
		default:
			addSSHImportOption(sshCfg, key, value)
		}
	}
}

func omitPasswordAuthTransportOptions(host *config.InventoryHostConfig) {
	if host.Auth.Mode != config.AuthModePassword || len(host.SSH.Options) == 0 {
		return
	}
	deleteSSHImportOption(host.SSH.Options, "PreferredAuthentications")
	deleteSSHImportOption(host.SSH.Options, "PubkeyAuthentication")
	if len(host.SSH.Options) == 0 {
		host.SSH.Options = nil
	}
}

func deleteSSHImportOption(options config.SSHOptions, key string) {
	for existing := range options {
		if strings.EqualFold(existing, key) {
			delete(options, existing)
			return
		}
	}
}

func parseSSHBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "on":
		return true, true
	case "no", "false", "off":
		return false, true
	default:
		return false, false
	}
}

func parseSSHDuration(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, true
	}
	d, err := time.ParseDuration(value)
	return d, err == nil
}

func formatSSHImportDuration(raw string, d time.Duration) string {
	if _, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return strings.TrimSpace(raw)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func addSSHImportOption(sshCfg *config.SSHHostConfig, key, value string) {
	if sshCfg.Options == nil {
		sshCfg.Options = make(config.SSHOptions)
	}
	sshCfg.Options[key] = config.NewSSHOptionString(value)
}

func addSSHImportOptionBool(sshCfg *config.SSHHostConfig, key string, value bool) {
	if sshCfg.Options == nil {
		sshCfg.Options = make(config.SSHOptions)
	}
	sshCfg.Options[key] = config.NewSSHOptionBool(value)
}

func addSSHImportBoolOptionOrString(sshCfg *config.SSHHostConfig, key, value string) {
	if b, ok := parseSSHBool(value); ok {
		addSSHImportOptionBool(sshCfg, key, b)
		return
	}
	addSSHImportOption(sshCfg, key, value)
}

func addSSHImportOptionItem(sshCfg *config.SSHHostConfig, key, value string) {
	if sshCfg.Options == nil {
		sshCfg.Options = make(config.SSHOptions)
	}
	current := sshCfg.Options[key]
	current.Items = append(current.Items, value)
	current.Scalar = ""
	current.Bool = nil
	current.Map = nil
	sshCfg.Options[key] = current
}

func addSSHImportOptionItems(sshCfg *config.SSHHostConfig, key string, values []string) {
	if len(values) == 0 {
		return
	}
	if sshCfg.Options == nil {
		sshCfg.Options = make(config.SSHOptions)
	}
	sshCfg.Options[key] = config.NewSSHOptionItems(values...)
}

func addSSHImportOptionMapValue(sshCfg *config.SSHHostConfig, key, mapKey, value string) {
	if sshCfg.Options == nil {
		sshCfg.Options = make(config.SSHOptions)
	}
	current := sshCfg.Options[key]
	if current.Map == nil {
		current.Map = make(map[string]string)
	}
	current.Map[mapKey] = value
	current.Scalar = ""
	current.Bool = nil
	current.Items = nil
	sshCfg.Options[key] = current
}

func isGlobalPattern(patterns []string) bool {
	return len(patterns) == 1 && patterns[0] == "*"
}

func shouldSkipHostPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?!")
}

func shouldWarnUnsupportedDirective(key string) bool {
	switch strings.ToLower(key) {
	case "canonicalizehostname":
		return true
	default:
		return false
	}
}

func expandHome(path string) string {
	if path == "~" {
		return homeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}

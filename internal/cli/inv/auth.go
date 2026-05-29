package inv

import (
	"fmt"

	runtimeagent "github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

type hostAuthPatch struct {
	Auth  config.InventoryAuthConfig
	Clear bool
}

type inventoryAuthView struct {
	Source      string
	Provider    string
	Ref         string
	Username    string
	UsernameRef string
}

type inventoryDisplayRow struct {
	Label string
	Value string
}

type inventoryDisplaySection struct {
	Title string
	Rows  []inventoryDisplayRow
}

func (p hostAuthPatch) HasChange() bool {
	return p.Clear || p.Auth.IsSet()
}

func (p hostAuthPatch) Validate(cfg *config.Config) error {
	if p.Clear && p.Auth.IsSet() {
		return fmt.Errorf("--credential-clear conflicts with credential mapping flags")
	}
	if !p.HasChange() {
		return nil
	}
	if err := p.Auth.Validate("inventory.host.auth"); err != nil {
		return err
	}
	if p.Clear {
		return nil
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	provider := p.Auth.Provider
	if provider == "" {
		provider = cfg.Credential.DefaultProvider
	}
	if provider == "" {
		return fmt.Errorf("--credential-provider is required when credential.default_provider is unset")
	}
	if _, ok := cfg.Credential.Provider[provider]; !ok {
		return fmt.Errorf("credential provider %q is not configured", provider)
	}
	return nil
}

func applyHostAuthPatch(parser *sshconfig.Parser, cfg *config.Config, paths *config.Paths, host string, patch hostAuthPatch) error {
	if !patch.HasChange() {
		return nil
	}
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	existing, _, err := findInventoryHostWithLocation(parser, cfg, paths, host)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("host %q not found", host)
	}
	if err := patch.Validate(cfg); err != nil {
		return err
	}
	if patch.Clear {
		if cfg.Inventory.Host != nil {
			delete(cfg.Inventory.Host, host)
		}
		return nil
	}
	if cfg.Inventory.Host == nil {
		cfg.Inventory.Host = make(map[string]config.InventoryHostConfig)
	}
	cfg.Inventory.Host[host] = config.InventoryHostConfig{Auth: patch.Auth}
	return cfg.Validate()
}

func stopAgentAfterInventoryAuthMutation() {
	client, err := runtimeagent.Connect()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	_ = client.Lock()
}

func effectiveInventoryAuth(cfg *config.Config, host, group string) inventoryAuthView {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if hostCfg, ok := cfg.Inventory.Host[host]; ok && hostCfg.Auth.IsSet() {
		return inventoryAuthViewFromAuth("host override", hostCfg.Auth, cfg.Credential.DefaultProvider)
	}
	if groupCfg, ok := cfg.Inventory.Group[group]; ok && groupCfg.Auth.IsSet() {
		return inventoryAuthViewFromAuth("group "+group, groupCfg.Auth, cfg.Credential.DefaultProvider)
	}
	return inventoryAuthView{Source: "-", Provider: "-", Ref: "-", Username: "-", UsernameRef: "-"}
}

func inventoryAuthViewFromAuth(source string, auth config.InventoryAuthConfig, defaultProvider string) inventoryAuthView {
	provider := auth.Provider
	if provider == "" {
		provider = defaultProvider
	}
	return inventoryAuthView{
		Source:      source,
		Provider:    valueOrDash(provider),
		Ref:         valueOrDash(auth.Ref),
		Username:    valueOrDash(auth.Username),
		UsernameRef: valueOrDash(auth.UsernameRef),
	}
}

func inventoryAuthDisplayRows(auth inventoryAuthView) []inventoryDisplayRow {
	return []inventoryDisplayRow{
		{Label: "Auth Source", Value: auth.Source},
		{Label: "Credential Provider", Value: auth.Provider},
		{Label: "Credential Password Ref", Value: auth.Ref},
		{Label: "Credential Username Override", Value: auth.Username},
		{Label: "Credential Username Ref", Value: auth.UsernameRef},
	}
}

func inventoryDisplaySections(sshRows, nsshRows []inventoryDisplayRow) []inventoryDisplaySection {
	return []inventoryDisplaySection{
		{Title: "SSH CONFIG", Rows: sshRows},
		{Title: "NSSH CONFIG", Rows: nsshRows},
	}
}

func inventoryHostSSHDisplayRows(host, hostName, user, port string) []inventoryDisplayRow {
	return []inventoryDisplayRow{
		{Label: "Host", Value: host},
		{Label: "HostName", Value: hostName},
		{Label: "User", Value: user},
		{Label: "Port", Value: port},
	}
}

func inventoryGroupSSHDisplayRows(domainSuffix, defaultUser string) []inventoryDisplayRow {
	return []inventoryDisplayRow{
		{Label: "Domain Suffix", Value: domainSuffix},
		{Label: "Default User", Value: defaultUser},
	}
}

func printInventoryDisplaySections(sections []inventoryDisplaySection) {
	for i, section := range sections {
		ui.SubSection(section.Title, i == 0)
		for _, row := range section.Rows {
			ui.PrintKeyValue(row.Label, row.Value)
		}
	}
}

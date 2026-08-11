package inv

import (
	"fmt"

	runtimeagent "github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
)

type inventoryAuthPatch struct {
	Auth  config.InventoryAuthConfig
	Clear bool
}

type inventoryAuthView struct {
	Source             string
	CredentialProvider string
	PasswordRef        string
	Username           string
	UsernameRef        string
	UsernameSource     string
	PasswordSource     string
	AuthMode           string
	AuthModeSource     string
}

type inventoryDisplayRow struct {
	Label string
	Value string
}

type inventoryDisplaySection struct {
	Title string
	Rows  []inventoryDisplayRow
}

func (p inventoryAuthPatch) HasChange() bool {
	return p.Clear || p.Auth.IsSet()
}

func (p inventoryAuthPatch) Validate(cfg *config.Config) error {
	if p.Clear && p.Auth.IsSet() {
		return fmt.Errorf("--cred none conflicts with credential mapping flags")
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
	auth := p.Auth
	auth.Normalize()
	provider := auth.CredentialProvider
	if provider == "" {
		return fmt.Errorf("--cred provider is required")
	}
	if _, ok := cfg.Credential.Provider[provider]; !ok {
		return fmt.Errorf("credential provider %q is not configured", provider)
	}
	return nil
}

func applyInventoryAuthPatch(cfg *config.Config, paths *config.Paths, host string, patch inventoryAuthPatch) error {
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
	existing, err := findInventoryHost(cfg, paths, host)
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
	hostCfg := cfg.Inventory.Host[host]
	hostCfg.Auth = mergeInventoryAuthPatch(hostCfg.Auth, patch.Auth)
	cfg.Inventory.Host[host] = hostCfg
	return cfg.Validate()
}

func mergeInventoryAuthPatch(existing, next config.InventoryAuthConfig) config.InventoryAuthConfig {
	if next.Username == "" && next.UsernameRef == "" {
		next.Username = existing.Username
		next.UsernameRef = existing.UsernameRef
	}
	return next
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
	provider := ""
	if parsedProvider, _, err := config.ParseInventoryGroupID(group); err == nil {
		provider = parsedProvider
	}
	resolved := cfg.ResolveInventoryAuth(config.InventoryAuthContext{Host: host, Provider: provider, Group: group})
	if resolved.Disabled {
		auth := emptyInventoryAuthView()
		auth.Source = "disabled"
		auth.PasswordSource = "disabled"
		return auth
	}
	if resolved.Source != "" {
		return inventoryAuthView{
			Source:             resolved.Source,
			CredentialProvider: valueOrDash(resolved.CredentialProvider),
			PasswordRef:        valueOrDash(resolved.PasswordRef),
			Username:           valueOrDash(resolved.Username),
			UsernameRef:        valueOrDash(resolved.UsernameRef),
			UsernameSource:     valueOrDash(resolved.UsernameSource),
			PasswordSource:     valueOrDash(resolved.PasswordSource),
			AuthMode:           valueOrDash(resolved.AuthMode),
			AuthModeSource:     valueOrDash(resolved.AuthModeSource),
		}
	}
	return emptyInventoryAuthView()
}

func inventoryAuthViewFromAuth(source string, auth config.InventoryAuthConfig) inventoryAuthView {
	auth.Normalize()
	usernameSource := "-"
	if auth.Username != "" || auth.UsernameRef != "" {
		usernameSource = source
	}
	passwordSource := "-"
	if auth.CredentialProvider != "" || auth.PasswordRef != "" {
		passwordSource = source
	}
	authModeSource := "-"
	if auth.Mode != "" {
		authModeSource = source
	}
	return inventoryAuthView{
		Source:             source,
		CredentialProvider: valueOrDash(auth.CredentialProvider),
		PasswordRef:        valueOrDash(auth.PasswordRef),
		Username:           valueOrDash(auth.Username),
		UsernameRef:        valueOrDash(auth.UsernameRef),
		UsernameSource:     usernameSource,
		PasswordSource:     passwordSource,
		AuthMode:           valueOrDash(auth.Mode),
		AuthModeSource:     authModeSource,
	}
}

func inventoryAuthDisplayRows(auth inventoryAuthView) []inventoryDisplayRow {
	return []inventoryDisplayRow{
		{Label: "Auth Source", Value: auth.Source},
		{Label: "Auth Mode", Value: auth.AuthMode},
		{Label: "Auth Mode Source", Value: auth.AuthModeSource},
		{Label: "Credential Provider", Value: auth.CredentialProvider},
		{Label: "Username Source", Value: auth.UsernameSource},
		{Label: "Username", Value: auth.Username},
		{Label: "Username Ref", Value: auth.UsernameRef},
		{Label: "Password Source", Value: auth.PasswordSource},
		{Label: "Password Ref", Value: auth.PasswordRef},
	}
}

func emptyInventoryAuthView() inventoryAuthView {
	return inventoryAuthView{
		Source:             "-",
		CredentialProvider: "-",
		PasswordRef:        "-",
		Username:           "-",
		UsernameRef:        "-",
		UsernameSource:     "-",
		PasswordSource:     "-",
		AuthMode:           "-",
		AuthModeSource:     "-",
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

func printInventoryDisplaySections(sections []inventoryDisplaySection) {
	for i, section := range sections {
		ui.SubSection(section.Title, i == 0)
		for _, row := range section.Rows {
			ui.PrintKeyValue(row.Label, row.Value)
		}
	}
}

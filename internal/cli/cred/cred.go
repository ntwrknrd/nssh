// Package cred provides SSH target credential commands.
package cred

import (
	"fmt"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

type credentialScope struct {
	Host  string
	Group string
}

// NewCmd creates the credential command group.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cred",
		Short: "Manage credentials",
		Long:  "Manage SSH target credentials for hosts and groups.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newSetCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newStatusCmd())
	return cmd
}

func activeStatusProvider() (credential.Provider, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, err
	}
	return credential.NewProvider(cfg)
}

func activeWritableProvider() (credential.Provider, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, err
	}
	if err := cfg.Credential.Validate(); err != nil {
		return nil, err
	}
	if cfg.Credential.Type != config.CredentialProviderAge {
		return credential.NewProvider(cfg)
	}
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return nil, err
	}
	if err := clisession.TryUnlockIfTTY(mgr); err != nil {
		return nil, err
	}
	return credential.NewAgeProvider(mgr), nil
}

func credentialScopeArg(args []string, group string) (credentialScope, error) {
	switch {
	case len(args) > 1:
		return credentialScope{}, fmt.Errorf("too many arguments")
	case len(args) == 1 && group != "":
		return credentialScope{}, fmt.Errorf("host and --group are mutually exclusive")
	case len(args) == 1:
		return credentialScope{Host: args[0]}, nil
	case group != "":
		return credentialScope{Group: group}, nil
	default:
		return credentialScope{}, fmt.Errorf("host or --group is required")
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show credential provider status",
		Long:  "Show the active credential provider backend.",
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := activeStatusProvider()
			if err != nil {
				return err
			}
			status := provider.Status()
			ui.CommandStart("CREDENTIAL PROVIDER")
			ui.PrintKeyValue("Provider", status.Type)
			ui.PrintKeyValue("Available", fmt.Sprintf("%v", status.Available))
			ui.PrintKeyValue("Detail", status.Detail)
			ui.CommandEnd(ui.StatusSuccess)
			return nil
		},
	}
}

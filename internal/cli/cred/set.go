package cred

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newSetCmd() *cobra.Command {
	var group string
	var username string
	cmd := &cobra.Command{
		Use:   "set HOST",
		Short: "Create or update credentials",
		Long:  "Create or update a host override or group fallback credential.",
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh cred set HOST\nnssh cred set -g GROUP",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := credentialScopeArg(args, group)
			if err != nil {
				return err
			}
			if username == "" {
				return fmt.Errorf("--username is required for non-interactive credential set")
			}
			password, err := ui.PasswordWithConfirm("Password")
			if err != nil {
				return err
			}
			provider, err := activeWritableProvider()
			if err != nil {
				return err
			}
			record := &credential.Record{Username: username, Secret: secret.NewFromString(password)}
			if scope.Host != "" {
				if err := provider.SetHost(scope.Host, record); err != nil {
					return err
				}
				if err := updateCredentialLinkAfterSet(scope); err != nil {
					return err
				}
				ui.Success("Credential saved for host %s", scope.Host)
				return nil
			}
			if err := provider.SetGroup(scope.Group, record); err != nil {
				return err
			}
			if err := updateCredentialLinkAfterSet(scope); err != nil {
				return err
			}
			ui.Success("Credential saved for group %s", scope.Group)
			return nil
		},
	}
	cmd.Flags().StringVarP(&group, "group", "g", "", "group credential scope")
	cmd.Flags().StringVar(&username, "username", "", "SSH username")
	return cmd
}

func updateCredentialLinkAfterSet(scope credentialScope) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	if cfg.Credential.Type == config.CredentialProvider1Password {
		if err := setCredentialLink(cfg, scope, config.CredentialRefConfig{Ref: deterministicOnePasswordItemRef(scope)}); err != nil {
			return err
		}
		return config.Save(config.DefaultPaths().ConfigFile, cfg)
	}
	_, err = clearCredentialLinkForScope(scope)
	return err
}

func deterministicOnePasswordItemRef(scope credentialScope) string {
	if scope.Host != "" {
		return "nssh host " + scope.Host
	}
	return "nssh group " + scope.Group
}

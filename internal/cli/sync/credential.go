package sync

import (
	"fmt"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

func newCredentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential",
		Short: "Manage sync source credentials",
		Long:  "Get or edit encrypted credentials for sync sources.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newCredentialGetCmd())
	cmd.AddCommand(newCredentialEditCmd())
	return cmd
}

func newCredentialGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <source>",
		Short: "Show sync source credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCredentialGet(args[0])
		},
	}
}

func runCredentialGet(source string) error {
	ui.CommandStart("SYNC CREDENTIALS: " + source)

	cfg, err := config.LoadDefault()
	if err == nil && findSource(cfg.Sync.Sources, source) == nil {
		ui.Warning("Source %q is not in the current config (credentials may be stale)", source)
	}

	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		ui.Error("Vault not available: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: exit.ExitVaultError, Message: err.Error()}
	}

	if err := clisession.TryUnlockIfTTY(mgr); err != nil {
		ui.Error("Unlock failed: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: exit.ExitVaultError, Message: err.Error()}
	}

	sv, err := mgr.GetSyncSource(source)
	if err != nil {
		ui.Error("Failed to read vault: %s", err)
		ui.CommandEnd(ui.StatusError)
		return err
	}

	if sv == nil {
		ui.Noop("No credentials stored for source %q", source)
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	if sv.DefaultCredential != nil {
		panel := ui.NewPanel("Default Credential")
		panel.Row("Username", sv.DefaultCredential.Username)
		panel.Row("Password", maskPassword(sv.DefaultCredential.Password))
		panel.Print()
	}

	if len(sv.ClassCredentials) > 0 {
		table := ui.NewTable("Class", "Username", "Password")
		for class, cred := range sv.ClassCredentials {
			table.AddRow(class, cred.Username, maskPassword(cred.Password))
		}
		fmt.Println()
		ui.SubSection("Class Credentials")
		table.Render()
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func newCredentialEditCmd() *cobra.Command {
	var className string
	var isDefault bool

	cmd := &cobra.Command{
		Use:   "edit <source>",
		Short: "Edit sync source credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isDefault && className == "" {
				return fmt.Errorf("specify --default or --class <name>")
			}
			if isDefault && className != "" {
				return fmt.Errorf("--default and --class are mutually exclusive")
			}
			return runCredentialEdit(args[0], className, isDefault)
		},
	}

	cmd.Flags().StringVar(&className, "class", "", "Credential class name")
	cmd.Flags().BoolVar(&isDefault, "default", false, "Edit default credential")
	return cmd
}

func runCredentialEdit(source, class string, isDefault bool) error {
	ui.CommandStart("EDIT SYNC CREDENTIAL: " + source)

	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		ui.Error("Vault not available: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: exit.ExitVaultError, Message: err.Error()}
	}

	if err := clisession.EnsureUnlocked(mgr); err != nil {
		ui.Error("Vault unlock required: %s", err)
		ui.CommandEnd(ui.StatusError)
		return &exit.ExitError{Code: exit.ExitVaultError, Message: err.Error()}
	}

	label := "default"
	if class != "" {
		label = "class " + class
	}

	username, err := ui.InputWithDefault(fmt.Sprintf("Username for %s [%s]", source, label), "")
	if err != nil {
		return err
	}
	if username == "" {
		ui.Abort("No username provided")
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	password, err := ui.PasswordSecure("Password")
	if err != nil {
		return err
	}
	defer password.Destroy()

	plainPass := string(password.Bytes())

	cred := &vault.Credential{
		Username: username,
		Password: plainPass,
	}

	if isDefault {
		if err := mgr.SetSyncSourceDefaultCredential(source, cred); err != nil {
			ui.Error("Failed to save: %s", err)
			ui.CommandEnd(ui.StatusError)
			return err
		}
		ui.Success("Default credential saved for %s", source)
	} else {
		if err := mgr.SetSyncSourceClassCredential(source, class, cred); err != nil {
			ui.Error("Failed to save: %s", err)
			ui.CommandEnd(ui.StatusError)
			return err
		}
		ui.Success("Class credential %q saved for %s", class, source)
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func maskPassword(pw string) string {
	if pw == "" {
		return "(not set)"
	}
	return "****"
}

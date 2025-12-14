package ctx

import (
	"fmt"
	"os"
	"path/filepath"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewAddCmd creates the ctx add command.
func NewAddCmd() *cobra.Command {
	var (
		sshConfig string
		domain    string
		username  string
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Create new context",
		Long: `Create new SSH context.

Prompts for context name, SSH config file, domain, and credentials interactively.
Use flags for non-interactive/scripted usage.

The --domain option enables automatic context selection when connecting
to hosts matching the domain suffix (e.g., --domain example.com will
auto-select this context for server.example.com).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return runAdd(name, addFlags{
				sshConfig:    sshConfig,
				sshConfigSet: cmd.Flags().Changed("ssh-config"),
				domain:       domain,
				domainSet:    cmd.Flags().Changed("domain"),
				username:     username,
				usernameSet:  cmd.Flags().Changed("username"),
				dryRun:       dryRun,
			})
		},
	}

	cmd.Flags().StringVarP(&sshConfig, "ssh-config", "s", "", "SSH config include file")
	cmd.Flags().StringVarP(&domain, "domain", "d", "", "domain suffix for auto-selection")
	cmd.Flags().StringVarP(&username, "username", "u", "", "fallback username (password prompted securely)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only")

	return cmd
}

type addFlags struct {
	sshConfig    string
	sshConfigSet bool
	domain       string
	domainSet    bool
	username     string
	usernameSet  bool
	dryRun       bool
}

func runAdd(name string, flags addFlags) error {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return err
	}

	// Unlock vault if needed and TTY is available
	_ = clisession.TryUnlockIfTTY(mgr)

	ui.CommandStart("ADD CONTEXT")

	// Context name
	finalName := name
	if finalName == "" {
		if flags.dryRun {
			finalName = "<dry-run-context>"
		} else {
			finalName, err = ui.Input("Context name", "")
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if finalName == "" {
				ui.Error("Context name is required")
				ui.CommandEnd(ui.StatusError)
				return nil
			}
		}
	}

	// Check if context already exists (skip for dry-run)
	if !flags.dryRun {
		existing, err := mgr.GetContext(finalName)
		if err != nil {
			ui.Error("Failed to check context: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		if existing != nil {
			ui.Error("Context '%s' already exists", finalName)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
	}

	// SSH config file
	gitIncludeFile := flags.sshConfig
	if !flags.sshConfigSet {
		if flags.dryRun {
			gitIncludeFile = "<dry-run-file>"
		} else {
			// Get files already used by other contexts
			contexts, _ := mgr.ListContexts()
			usedFiles := make([]string, 0, len(contexts))
			for _, c := range contexts {
				usedFiles = append(usedFiles, c.GitIncludeFile)
			}

			defaultFilename := fmt.Sprintf("%s_hosts", finalName)
			gitIncludeFile, err = selectSSHConfigFile(defaultFilename, usedFiles...)
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
		}
	}

	// Domain
	domain := flags.domain
	if !flags.domainSet && !flags.dryRun {
		choice, err := ui.Select("Domain", []ui.SelectOption{
			{Label: "Skip (no domain)", Value: "skip"},
			{Label: "Set domain...", Value: "set"},
		})
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}

		if choice == "set" {
			domain, err = ui.Input("Domain suffix (e.g., example.com)", "")
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
		}
	}

	// Credential
	username := flags.username
	var password string

	if flags.usernameSet {
		// Username provided via flag - validate and prompt for password
		if username == "" {
			ui.Error("Username cannot be empty")
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		if !flags.dryRun {
			password, err = ui.PasswordWithConfirm("Password")
			if err != nil {
				ui.Error("Password error: %s", err)
				ui.CommandEnd(ui.StatusError)
				return nil
			}
		}
	} else if !flags.dryRun {
		// No username flag - offer interactive credential setup
		choice, err := ui.Select("Credential", []ui.SelectOption{
			{Label: "Skip (no credential)", Value: "skip"},
			{Label: "Add credential...", Value: "add"},
		})
		if err != nil {
			ui.Abort("Canceled")
			ui.CommandEnd(ui.StatusAbort)
			return nil
		}

		if choice == "add" {
			username, err = ui.Input("Username", "")
			if err != nil {
				ui.Abort("Canceled")
				ui.CommandEnd(ui.StatusAbort)
				return nil
			}
			if username == "" {
				ui.Warning("Username cannot be empty, skipping credential")
			} else {
				password, err = ui.PasswordWithConfirm("Password")
				if err != nil {
					ui.Error("Password error: %s", err)
					ui.CommandEnd(ui.StatusError)
					return nil
				}
			}
		}
	}

	if flags.dryRun {
		ui.SubSection("Preview")
		ui.Info("Would create context '%s' for file '%s'", finalName, gitIncludeFile)
		if domain != "" {
			ui.Info("Domain auto-select: %s", domain)
		}
		if username != "" {
			ui.Info("Would add credential for '%s'", username)
		}
		ui.Warning("Dry-run: no changes written")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}

	// Create SSH config file if needed
	paths := config.DefaultPaths()
	confD := filepath.Join(paths.SSHConfigDir, "conf.d")
	configFilePath := filepath.Join(confD, gitIncludeFile)
	fileCreated := false

	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		if err := os.MkdirAll(confD, 0700); err != nil {
			ui.Error("Failed to create conf.d: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		f, err := os.Create(configFilePath)
		if err != nil {
			ui.Error("Failed to create config file: %s", err)
			ui.CommandEnd(ui.StatusError)
			return nil
		}
		_ = f.Close()
		fileCreated = true
	}

	// Build credential if provided
	var cred *vault.Credential
	if username != "" && password != "" {
		cred = &vault.Credential{Username: username, Password: password}
	}

	// Create context with credential (single write)
	// Store just the filename - full path is constructed at runtime via sshConfigPath()
	if err := mgr.CreateContext(finalName, gitIncludeFile, domain, cred); err != nil {
		ui.Error("Failed to create context: %s", err)
		ui.CommandEnd(ui.StatusError)
		return nil
	}

	// Show results
	if fileCreated {
		ui.Success("Created SSH config file: '%s'", configFilePath)
	}
	ui.Success("Context '%s' created", finalName)
	if domain != "" {
		ui.Info("Auto-selects for hosts matching: *.%s", domain)
	}
	if username != "" {
		ui.Success("Credential added for '%s'", username)
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

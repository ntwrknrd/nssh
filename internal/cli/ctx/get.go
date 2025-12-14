package ctx

import (
	"crypto/sha256"
	"fmt"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewGetCmd creates the ctx get command.
func NewGetCmd() *cobra.Command {
	var showSecret bool

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show context details",
		Long:  "Show details for a specific credential context.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(args[0], showSecret)
		},
	}

	cmd.Flags().BoolVarP(&showSecret, "show-secret", "s", false, "reveal password in plain text (prints to terminal)")

	return cmd
}

// maskPassword returns a deterministic asterisk mask based on password hash.
// Length is 6-13 asterisks based on hash, not actual password length.
func maskPassword(password string) string {
	if password == "" {
		return "(no password)"
	}
	hash := sha256.Sum256([]byte(password))
	// Use first byte to determine length (6-13)
	length := int(hash[0]%8) + 6
	result := make([]byte, length)
	for i := range result {
		result[i] = '*'
	}
	return string(result)
}

func runGet(name string, showSecret bool) error {
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return err
	}

	// Unlock vault if needed and TTY is available
	_ = clisession.TryUnlockIfTTY(mgr)

	ui.CommandStart("CONTEXT DETAILS")

	ctx, err := mgr.GetContext(name)
	if err != nil {
		ui.Error("Failed to get context: %s", err)
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("get context: %w", err)
	}

	if ctx == nil {
		ui.Error("Context not found: %s", name)
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("context %q not found", name)
	}

	// Display context details (same fields as ctx list)
	includePath := displayIncludePath(ctx.GitIncludeFile)
	hostCount := countHostsInFile(ctx.GitIncludeFile)
	hostCountStr := "-"
	if hostCount > 0 {
		hostCountStr = fmt.Sprintf("%d", hostCount)
	}
	domain := ctx.Domain
	if domain == "" {
		domain = "-"
	}

	ui.PrintKeyValue("Context", name)
	ui.PrintKeyValue("SSH Config File", includePath)
	ui.PrintKeyValue("Hosts", hostCountStr)
	ui.PrintKeyValue("Domain", domain)

	// Display credential with username and password
	if ctx.Credential != nil {
		var passwordDisplay string
		if showSecret {
			passwordDisplay = ctx.Credential.Password
		} else {
			passwordDisplay = maskPassword(ctx.Credential.Password)
		}
		ui.PrintKeyValue("Credential", fmt.Sprintf("%s / %s", ctx.Credential.Username, passwordDisplay))
	} else {
		ui.PrintKeyValue("Credential", "-")
	}

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

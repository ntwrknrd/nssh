package sync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/ntwrknrd/nssh/internal/cli/resolve"
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
	intsync "github.com/ntwrknrd/nssh/internal/sync"

	"github.com/ntwrknrd/nssh/internal/sync/providers"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewRunCmd creates the sync run command.
func NewRunCmd() *cobra.Command {
	var dryRun bool
	var prune bool

	cmd := &cobra.Command{
		Use:   "run [source]",
		Short: "Run sync for one or all sources",
		Long: `Run inventory sync for a named source or all configured sources.

Without a source name, processes all configured sources sequentially.
With --dry-run, shows the plan without making changes.
With --prune, removes hosts that are no longer discovered.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var source string
			if len(args) > 0 {
				source = args[0]
			}
			return runSync(source, dryRun, prune)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show plan without making changes")
	cmd.Flags().BoolVar(&prune, "prune", false, "Remove hosts no longer present in source")
	return cmd
}

func runSync(sourceName string, dryRun, prune bool) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Sync.Sources) == 0 {
		ui.Noop("No sync sources configured")
		return nil
	}

	// Select sources to process
	var sources []config.SyncSourceConfig
	if sourceName != "" {
		src := findSource(cfg.Sync.Sources, sourceName)
		if src == nil {
			return fmt.Errorf("source %q not found in config", sourceName)
		}
		sources = []config.SyncSourceConfig{*src}
	} else {
		sources = cfg.Sync.Sources
	}

	// Initialize vault manager for credential operations
	var mgr *vault.Manager
	if !dryRun {
		mgr, err = clisession.NewManager(vault.Auto())
		if err != nil {
			return fmt.Errorf("vault: %w", err)
		}
		if err := clisession.TryUnlockIfTTY(mgr); err != nil {
			return fmt.Errorf("vault unlock: %w", err)
		}
	}

	runner := newSyncRunner()
	var anyFailed bool

	for _, src := range sources {
		if err := runSourceSync(src, runner, mgr, dryRun, prune); err != nil {
			ui.Error("Source %s failed: %s", src.Name, err)
			anyFailed = true
			// Continue with remaining sources
		}
	}

	if anyFailed {
		return &exit.ExitError{Code: exit.ExitGeneralError, Message: "one or more sources failed"}
	}
	return nil
}

func runSourceSync(
	src config.SyncSourceConfig,
	runner intsync.RemoteRunner,
	mgr *vault.Manager,
	dryRun, prune bool,
) error {
	ui.CommandStart(fmt.Sprintf("SYNC: %s (%s)", src.Name, src.Provider))

	// Acquire per-source lock
	if !dryRun {
		unlock, err := intsync.AcquireSourceLock(src.Name)
		if err != nil {
			ui.CommandEnd(ui.StatusError)
			return fmt.Errorf("acquire lock: %w", err)
		}
		defer unlock()
	}

	// Load current state
	current, err := intsync.LoadSourceState(src.Name)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("load state: %w", err)
	}

	// Create provider
	provider, err := createProvider(src.Provider)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	// Discover
	ui.Info("Discovering from %s...", src.Name)
	objects, err := provider.Discover(context.TODO(), src, runner)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("discover: %w", err)
	}
	ui.Info("Discovered %d objects", len(objects))

	// Reconcile
	plan := intsync.Reconcile(objects, src.Routes, src.Name, current)

	// Show plan
	printPlan(plan)

	if dryRun {
		ui.Info("Dry run -- no changes written")
		ui.CommandEnd(ui.StatusInfo)
		return nil
	}

	// Prompt for missing class credentials
	if mgr != nil {
		if err := promptMissingCredentials(plan, src.Name, mgr); err != nil {
			ui.CommandEnd(ui.StatusError)
			return err
		}
	}

	// Build the final host set
	allHosts := make(map[string]*intsync.ManagedHost)
	if current != nil {
		maps.Copy(allHosts, current.Objects)
	}
	for _, h := range plan.Adds {
		allHosts[h.ObjectID] = h
	}
	for _, h := range plan.Updates {
		allHosts[h.ObjectID] = h
	}
	if prune {
		for _, h := range plan.Removals {
			delete(allHosts, h.ObjectID)
		}
	}

	// Collect hosts for SSH config writing
	hostList := slices.Collect(maps.Values(allHosts))

	// Write SSH config
	if len(hostList) > 0 {
		if err := intsync.WriteManagedSSHConfigs(hostList, src.Name, src.Provider); err != nil {
			ui.CommandEnd(ui.StatusError)
			return fmt.Errorf("write SSH config: %w", err)
		}
	}

	// Handle pruned include files
	if prune && len(hostList) == 0 {
		// All hosts removed, clean up include files
		if current != nil {
			files := intsync.CollectIncludeFiles(slices.Collect(maps.Values(current.Objects)))
			for _, f := range files {
				if err := intsync.RemoveManagedSSHConfig(f); err != nil {
					ui.Warning("Failed to remove %s: %s", f, err)
				}
			}
		}
	}

	// Save state
	state := &intsync.SourceState{
		Version:  intsync.StateVersion,
		Source:   src.Name,
		Provider: src.Provider,
		LastSync: time.Now().UTC(),
		Objects:  allHosts,
	}
	if err := intsync.SaveSourceState(state); err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("save state: %w", err)
	}

	// Summary
	fmt.Println()
	ui.Info("SSH config:")
	fmt.Printf("  [+] %d added   [~] %d updated   [-] %d removed   [=] %d unchanged\n",
		len(plan.Adds), len(plan.Updates), prunedCount(plan, prune), len(plan.Unchanged))

	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func createProvider(providerName string) (intsync.Provider, error) {
	switch providerName {
	case config.ProviderContainerlab:
		return providers.NewContainerlabProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", providerName)
	}
}

func printPlan(plan *intsync.SyncPlan) {
	if len(plan.Adds) > 0 {
		ui.SubSection("Additions")
		for _, h := range plan.Adds {
			fmt.Printf("    [+] %s -> %s\n", h.Host, h.HostName)
		}
	}
	if len(plan.Updates) > 0 {
		ui.SubSection("Updates")
		for _, h := range plan.Updates {
			fmt.Printf("    [~] %s -> %s\n", h.Host, h.HostName)
		}
	}
	if len(plan.Removals) > 0 {
		ui.SubSection("Removals (requires --prune)")
		for _, h := range plan.Removals {
			fmt.Printf("    [-] %s\n", h.Host)
		}
	}
	if len(plan.Unmatched) > 0 {
		ui.SubSection("Unmatched (skipped)")
		for i := range plan.Unmatched {
			fmt.Printf("    [ ] %s (%s)\n", plan.Unmatched[i].Name, plan.Unmatched[i].CredentialClass)
		}
	}
}

func promptMissingCredentials(plan *intsync.SyncPlan, source string, mgr *vault.Manager) error {
	// Collect unique credential classes from adds and updates
	classSet := make(map[string]bool)
	for _, h := range append(plan.Adds, plan.Updates...) {
		if h.CredentialClass != "" {
			classSet[h.CredentialClass] = true
		}
	}

	for class := range classSet {
		// Check if we already have this credential
		cred, err := mgr.GetSyncSourceCredential(source, class)
		if err != nil {
			return err
		}
		if cred != nil {
			continue
		}

		// Prompt for new credential
		fmt.Println()
		ui.Info("New class credential needed for source %q:", source)
		fmt.Printf("  %s\n", class)

		username, err := ui.InputWithDefault("    Username [admin]", "admin")
		if err != nil {
			return err
		}

		password, err := ui.PasswordSecure("    Password")
		if err != nil {
			return err
		}

		plainPass := string(password.Bytes())
		password.Destroy()

		if err := mgr.SetSyncSourceClassCredential(source, class, &vault.Credential{
			Username: username,
			Password: plainPass,
		}); err != nil {
			return fmt.Errorf("save class credential: %w", err)
		}
	}

	return nil
}

func prunedCount(plan *intsync.SyncPlan, prune bool) int {
	if prune {
		return len(plan.Removals)
	}
	return 0
}

func findSource(sources []config.SyncSourceConfig, name string) *config.SyncSourceConfig {
	for i := range sources {
		if sources[i].Name == name {
			return &sources[i]
		}
	}
	return nil
}

// syncRunner adapts remoteexec.SSHRunner to the sync.RemoteRunner interface.
type syncRunner struct {
	ssh *remoteexec.SSHRunner
}

func newSyncRunner() *syncRunner {
	resolver := func(host string) (*remoteexec.HostInfo, error) {
		resolved, err := resolve.ResolveHostForConnect(host, "")
		if err != nil {
			return nil, err
		}
		return &remoteexec.HostInfo{
			Hostname: resolved.Hostname,
			Username: resolved.Username,
		}, nil
	}
	return &syncRunner{ssh: remoteexec.NewSSHRunner(resolver)}
}

func (r *syncRunner) Run(ctx context.Context, host string, cmd intsync.RemoteCommand) (*intsync.RemoteResult, error) {
	result, err := r.ssh.Run(ctx, host, remoteexec.RemoteCommand{
		Argv:    cmd.Argv,
		Sudo:    cmd.Sudo,
		Timeout: cmd.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &intsync.RemoteResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}, nil
}

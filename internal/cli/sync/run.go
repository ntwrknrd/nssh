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
	cmd := &cobra.Command{
		Use:   "run [source]",
		Short: "Run sync for one or all sources",
		Long: `Run inventory sync for a named source or all configured sources.

Without a source name, processes all configured sources sequentially.

Sync is authoritative: hosts no longer discovered are removed automatically.

Jump hosts used for remote discovery (e.g. containerlab) must use key-based
SSH authentication. Password-only jump hosts are not supported.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var source string
			if len(args) > 0 {
				source = args[0]
			}
			return runSync(source)
		},
	}
	return cmd
}

func runSync(sourceName string) error {
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

	// Initialize vault manager for context validation and credential operations.
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if err := clisession.TryUnlockIfTTY(mgr); err != nil {
		return fmt.Errorf("vault unlock: %w", err)
	}
	if mgr.NeedsUnlock() {
		return fmt.Errorf("vault is locked and no TTY available for interactive unlock")
	}

	// Validate that every referenced route context exists in vault
	if err := validateRouteContexts(sources, mgr); err != nil {
		return err
	}

	runner := newSyncRunner(cfg)
	var anyFailed bool

	for _, src := range sources {
		if err := runSourceSync(src, runner, mgr); err != nil {
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
) error {
	ui.CommandStart(fmt.Sprintf("SYNC: %s (%s)", src.Name, src.Provider))

	// Acquire per-source lock
	unlock, err := intsync.AcquireSourceLock(src.Name)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer unlock()

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
	objects, err := provider.Discover(context.Background(), src, runner)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("discover: %w", err)
	}
	ui.Info("Discovered %d objects", len(objects))

	// Reconcile
	plan := intsync.Reconcile(objects, src.Routes, src.Name, current)

	// Show plan
	printPlan(plan)

	// Prompt for missing class credentials
	if err := promptMissingCredentials(plan, src.Name, mgr); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	// Build the final host set (removals always applied)
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
	for _, h := range plan.Removals {
		delete(allHosts, h.ObjectID)
	}

	// Collect hosts for SSH config writing
	hostList := slices.Collect(maps.Values(allHosts))

	// Save state, then write SSH configs. If SSH config write fails,
	// rollback state to keep the two in sync.
	hasChanges := len(plan.Adds) > 0 || len(plan.Updates) > 0 || len(plan.Removals) > 0
	state := &intsync.SourceState{
		Version:     intsync.StateVersion,
		Source:      src.Name,
		Provider:    src.Provider,
		LastSync:    time.Now().UTC(),
		IncludeFile: intsync.SourceIncludeFile(src.Name),
		Objects:     allHosts,
	}
	if err := intsync.SaveSourceState(state); err != nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("save state: %w", err)
	}

	// Write SSH config only when there are actual changes
	if hasChanges && len(hostList) > 0 {
		if err := intsync.WriteManagedSSHConfig(state.IncludeFile, hostList, src.Name, src.Provider); err != nil {
			// Rollback state to previous version
			if current != nil {
				if rbErr := intsync.SaveSourceState(current); rbErr != nil {
					ui.Warning("State rollback also failed: %s", rbErr)
				}
			} else {
				if rbErr := intsync.DeleteSourceState(src.Name); rbErr != nil {
					ui.Warning("State rollback also failed: %s", rbErr)
				}
			}
			ui.CommandEnd(ui.StatusError)
			return fmt.Errorf("write SSH config: %w", err)
		}
	} else if hasChanges && len(hostList) == 0 && current != nil {
		// All hosts removed -- delete the old source-owned include file.
		includeFile := current.IncludeFile
		if includeFile == "" {
			includeFile = intsync.SourceIncludeFile(src.Name)
		}
		if err := intsync.RemoveManagedSSHConfig(includeFile); err != nil {
			ui.Warning("Failed to remove %s: %s", includeFile, err)
		}
	}

	// Summary
	fmt.Println()
	ui.Info("SSH config:")
	fmt.Printf("  [+] %d added   [~] %d updated   [-] %d removed   [=] %d unchanged\n",
		len(plan.Adds), len(plan.Updates), len(plan.Removals), len(plan.Unchanged))

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
		ui.SubSection("Removals")
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

		username, err := ui.Input("    Username", "")
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

func findSource(sources []config.SyncSourceConfig, name string) *config.SyncSourceConfig {
	for i := range sources {
		if sources[i].Name == name {
			return &sources[i]
		}
	}
	return nil
}

func validateRouteContexts(sources []config.SyncSourceConfig, mgr *vault.Manager) error {
	seen := make(map[string]bool)
	var missing []string
	for _, src := range sources {
		for _, r := range src.Routes {
			if seen[r.Context] {
				continue
			}
			seen[r.Context] = true
			ctx, err := mgr.GetContext(r.Context)
			if err != nil {
				return fmt.Errorf("vault error checking context %q: %w", r.Context, err)
			}
			if ctx == nil {
				missing = append(missing, r.Context)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("route context(s) not found in vault: %v; create with 'nssh ctx add'", missing)
	}
	return nil
}

func newSyncRunner(cfg *config.Config) intsync.RemoteRunner {
	resolver := func(host string) (*remoteexec.HostInfo, error) {
		resolved, err := resolve.ResolveHostForConnect(host, "", cfg)
		if err != nil {
			return nil, err
		}
		return remoteExecHostInfo(host, resolved)
	}
	return remoteexec.NewSSHRunner(resolver)
}

func remoteExecHostInfo(host string, resolved *resolve.ResolvedHost) (*remoteexec.HostInfo, error) {
	if resolved.HostEntry != nil && resolved.HostEntry.UsesPassword() {
		return nil, fmt.Errorf("host %q is configured for password auth, but sync remote exec requires key-based access (BatchMode=yes)", host)
	}
	username := resolved.Username
	if resolved.HostEntry != nil {
		if transportUser := resolved.HostEntry.User(); transportUser != "" {
			username = transportUser
		}
	}
	return &remoteexec.HostInfo{
		Target:   host,
		Hostname: resolved.Hostname,
		Username: username,
	}, nil
}

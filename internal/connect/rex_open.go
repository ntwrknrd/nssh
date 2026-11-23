package connect

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/rex"
	"github.com/ntwrknrd/nssh/internal/ui"
)

var (
	rexClientFunc     = newRexOpener
	rexExecutableFunc = os.Executable
	selectHostsFunc   = ui.FuzzySelectStrings
)

// rexOpener is the part of the Rex client that opening hosts needs.
type rexOpener interface {
	CanSelect(context.Context) bool
	Open(context.Context, rex.Placement, []rex.Host) (rex.Opened, error)
}

func newRexOpener() rexOpener {
	if c := rex.NewClient(); c != nil {
		return c
	}
	return nil
}

// OpenInRex opens targets as interactive sessions in Rex panes and reports
// whether it did. It declines, so the caller connects or errors as it would
// outside Rex, when the integration is off, SSH options were given (a pane
// could not carry them), the process is itself a launched pane, or Rex
// cannot place the panes. A single host also needs terminal stdin and stdout
// and an app that accepts actions, since a tab the operator cannot see
// would look like a hang.
func OpenInRex(ctx context.Context, cfg *config.Config, targets, sshArgs []string) bool {
	single := len(targets) == 1
	if cfg == nil || !cfg.Rex.EnabledValue() || len(targets) == 0 || len(sshArgs) > 0 || rex.InPane() {
		return false
	}
	if single && (cfg.Rex.SingleHostValue() != config.RexSingleHostTab || !titleTerminal()) {
		return false
	}
	client := rexClientFunc()
	if client == nil {
		return false
	}
	if single && !client.CanSelect(ctx) {
		slog.Debug("rex app does not accept actions; connecting in the current pane")
		return false
	}
	exe, err := rexExecutableFunc()
	if err != nil {
		slog.Debug("rex open skipped", "err", err)
		return false
	}
	hosts := make([]rex.Host, len(targets))
	for i, target := range targets {
		hosts[i] = rex.Host{Name: target, Argv: rex.PaneArgv(exe, target)}
	}
	placement := rex.Placement{
		Session:     cfg.Rex.SessionValue(),
		PanesPerTab: cfg.Rex.PanesPerTabValue(),
		Split:       cfg.Rex.SplitValue(),
	}
	opened, err := client.Open(ctx, placement, hosts)
	if err != nil {
		slog.Debug("rex open failed", "err", err)
		return false
	}
	reportRexOpen(opened)
	return true
}

func reportRexOpen(opened rex.Opened) {
	var parts []string
	if len(opened.Tabs) > 0 {
		parts = append(parts, "opened "+strings.Join(opened.Tabs, ", "))
	}
	if len(opened.Reused) > 0 {
		parts = append(parts, "switched to "+strings.Join(opened.Reused, ", "))
	}
	msg := strings.Join(parts, "; ") + " in Rex session " + opened.Session
	if !opened.Selected {
		msg += " (turn on Remote Control in Rex to switch to it automatically)"
	}
	ui.Info("%s", msg)
}

// rexTarget is the literal target a launched pane connects to: the inventory
// name when the host resolved to one, with any explicit user kept.
func rexTarget(hostname, explicitUser string, resolved *ResolvedHost) string {
	if resolved == nil || resolved.Canonical == "" {
		return hostname
	}
	if explicitUser != "" {
		return explicitUser + "@" + resolved.Canonical
	}
	return resolved.Canonical
}

// rexPickHosts runs smart lookup with multi-select when several inventory
// hosts match query and Rex could open them as panes. Choosing several opens
// them and reports handled. Choosing one returns it as the query to connect
// to, so the caller's lookup resolves it exactly. Otherwise query is returned
// unchanged and lookup proceeds as it does outside Rex.
func rexPickHosts(ctx context.Context, query, explicitUser string, sshArgs []string, cfg *config.Config) (string, bool, error) {
	if cfg == nil || !cfg.Rex.EnabledValue() || len(sshArgs) > 0 || rex.InPane() || !titleTerminal() {
		return query, false, nil
	}
	hostname, user := splitConnectQuery(query, explicitUser)
	catalog, err := BuildHostCatalog(cfg)
	if err != nil {
		return query, false, err
	}
	if _, ok := catalog.Find(hostname); ok {
		return query, false, nil
	}
	suggestions := catalog.Suggestions(hostname)
	if len(suggestions) < 2 || rexClientFunc() == nil {
		return query, false, nil
	}
	selected, err := selectHostsFunc("Select hosts (Tab for several)", suggestions, hostname)
	if err != nil {
		return query, false, fmt.Errorf("fuzzy select: %w", err)
	}
	if len(selected) == 0 {
		return query, false, fmt.Errorf("selection canceled")
	}
	targets := make([]string, len(selected))
	for i, name := range selected {
		targets[i] = name
		if user != "" {
			targets[i] = user + "@" + name
		}
	}
	if len(targets) == 1 {
		return targets[0], false, nil
	}
	if !OpenInRex(ctx, cfg, targets, nil) {
		return query, true, fmt.Errorf("could not open %d hosts in Rex; run nssh -v for details", len(targets))
	}
	return query, true, nil
}

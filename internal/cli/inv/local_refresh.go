package inv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

type localRefreshFinding struct {
	Host   string
	Group  string
	Issue  string
	Detail string
	host   *sshconfig.HostEntry
	fix    localRefreshFix
}

type localRefreshFixKind int

const (
	localRefreshFixNone localRefreshFixKind = iota
	localRefreshFixRemoveHost
	localRefreshFixRemoveDuplicate
	localRefreshFixRenameHost
	localRefreshFixMergeHost
)

type localRefreshFix struct {
	kind        localRefreshFixKind
	host        *sshconfig.HostEntry
	target      *sshconfig.HostEntry
	newID       string
	cnameTarget string
	alias       string
}

type localRefreshDNSResult struct {
	status string
	detail string
}

func runLocalRefresh() error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		return err
	}
	hosts, err := inventoryHosts(nil, cfg, config.DefaultPaths())
	if err != nil {
		return err
	}
	table := ui.NewStreamTable("Host", "Group", "Issue", "Detail").
		WithColumnWidths(localRefreshTableWidths(hosts, cfg, config.DefaultPaths(), index)...)
	var findings []localRefreshFinding
	count := visitLocalRefreshFindings(hosts, cfg, config.DefaultPaths(), index, localRefreshResolveTarget, func(finding localRefreshFinding) {
		findings = append(findings, finding)
		table.AddRow(finding.Host, finding.Group, finding.Issue, finding.Detail)
	})
	if count == 0 {
		ui.Noop("No inventory issues found")
		return nil
	}
	table.Close()
	fixable := fixableLocalRefreshFindings(findings)
	if len(fixable) == 0 {
		ui.Warning("No local fixes are available for these findings")
		return nil
	}

	selected, err := selectLocalRefreshFixes(fixable)
	if err != nil {
		ui.Abort("Selection failed: %s", err)
		return nil
	}
	if len(selected) == 0 {
		ui.Abort("No fixes selected")
		return nil
	}

	ui.SubSection("Applying")
	applied, err := applyLocalRefreshFixes(nil, config.DefaultPaths(), selected)
	if err != nil {
		return err
	}
	ui.Success("Applied %d fix(es)", applied)
	return nil
}

func visitLocalRefreshFindings(
	hosts []*sshconfig.HostEntry,
	cfg *config.Config,
	paths *config.Paths,
	index map[string]*inventory.HostInfo,
	dnsCheck func(string) localRefreshDNSResult,
	emit func(localRefreshFinding),
) int {
	if dnsCheck == nil {
		dnsCheck = localRefreshResolveTarget
	}
	count := 0
	emitFinding := func(finding localRefreshFinding) {
		count++
		if emit != nil {
			emit(finding)
		}
	}
	seen := make(map[string]*sshconfig.HostEntry)
	localHosts := localHostPatternIndex(hosts, cfg, paths, index)
	for _, host := range hosts {
		meta := metadataForHost(host, cfg, paths, index)
		duplicate := false
		for _, pattern := range hostPatterns(host) {
			if prev := seen[pattern]; prev != nil {
				duplicate = true
				finding := localRefreshFinding{
					Host:   host.Host,
					Group:  meta.Group,
					Issue:  "duplicate",
					Detail: fmt.Sprintf("pattern %q also appears in %s", pattern, prev.SourceFile),
					host:   host,
				}
				if meta.Owner == "local" {
					finding.fix = localRefreshFix{kind: localRefreshFixRemoveDuplicate, host: host}
				}
				emitFinding(finding)
				continue
			}
			seen[pattern] = host
		}
		if duplicate {
			continue
		}
		if meta.Owner != "local" {
			continue
		}
		target := localRefreshDNSTarget(host)
		result := dnsCheck(target)
		switch result.status {
		case "nxdomain":
			emitFinding(localRefreshFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "stale-dns",
				Detail: fmt.Sprintf("%s: NXDOMAIN", target),
				host:   host,
				fix:    localRefreshFix{kind: localRefreshFixRemoveHost, host: host},
			})
		case "timeout":
			emitFinding(localRefreshFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "stale-dns",
				Detail: fmt.Sprintf("%s: lookup timeout", target),
				host:   host,
				fix:    localRefreshFix{kind: localRefreshFixRemoveHost, host: host},
			})
		case "cname":
			newID := sshconfig.DeriveHostID(result.detail)
			fix := localRefreshFix{kind: localRefreshFixRenameHost, host: host, newID: newID, cnameTarget: result.detail}
			if existing := localHosts[strings.ToLower(newID)]; existing != nil && existing != host {
				fix = localRefreshFix{kind: localRefreshFixMergeHost, host: host, target: existing, alias: host.Host}
			}
			emitFinding(localRefreshFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "cname-rename",
				Detail: fmt.Sprintf("%s -> %s", target, result.detail),
				host:   host,
				fix:    fix,
			})
		case "error":
			emitFinding(localRefreshFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "dns-error",
				Detail: fmt.Sprintf("%s: %s", target, result.detail),
				host:   host,
			})
		}
	}
	return count
}

func localRefreshTableWidths(hosts []*sshconfig.HostEntry, cfg *config.Config, paths *config.Paths, index map[string]*inventory.HostInfo) []int {
	widths := []int{len("Host"), len("Group"), len("stale-dns"), 72}
	for _, host := range hosts {
		if w := len(host.Host); w > widths[0] {
			widths[0] = w
		}
		meta := metadataForHost(host, cfg, paths, index)
		if w := len(meta.Group); w > widths[1] {
			widths[1] = w
		}
	}
	return widths
}

func fixableLocalRefreshFindings(findings []localRefreshFinding) []localRefreshFinding {
	fixable := make([]localRefreshFinding, 0, len(findings))
	for i := range findings {
		finding := findings[i]
		if finding.fix.kind != localRefreshFixNone {
			fixable = append(fixable, finding)
		}
	}
	return fixable
}

func selectLocalRefreshFixes(findings []localRefreshFinding) ([]localRefreshFinding, error) {
	options := make([]ui.FuzzySelectOption, len(findings))
	for i := range findings {
		finding := findings[i]
		options[i] = ui.FuzzySelectOption{
			Label: fmt.Sprintf("%s %s", finding.Issue, finding.Host),
			Value: i,
		}
	}
	indices, err := ui.FuzzySelectMulti("Select inventory fixes to apply", options)
	if err != nil {
		return nil, err
	}
	selected := make([]localRefreshFinding, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(findings) {
			selected = append(selected, findings[idx])
		}
	}
	return selected, nil
}

func applyLocalRefreshFixes(parser *sshconfig.Parser, paths *config.Paths, findings []localRefreshFinding) (int, error) {
	if paths == nil {
		paths = config.DefaultPaths()
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return 0, err
	}
	provider := cfg.Inventory.Providers[config.ProviderLocal]
	if provider.Hosts == nil {
		provider.Hosts = make(map[string]config.InventoryHostConfig)
	}

	applied := 0
	for i := range findings {
		finding := findings[i]
		switch finding.fix.kind {
		case localRefreshFixNone:
			continue
		case localRefreshFixRemoveHost:
			delete(provider.Hosts, finding.fix.host.Host)
			applied++
		case localRefreshFixRemoveDuplicate:
			delete(provider.Hosts, finding.fix.host.Host)
			applied++
		case localRefreshFixRenameHost:
			host, ok := provider.Hosts[finding.fix.host.Host]
			if !ok {
				return applied, fmt.Errorf("host %q not found in local inventory", finding.fix.host.Host)
			}
			newID := finding.fix.newID
			if strings.TrimSpace(newID) == "" {
				newID = finding.fix.host.Host
			}
			delete(provider.Hosts, finding.fix.host.Host)
			host.Aliases = uniqueHostPatterns(append(host.Aliases, finding.fix.host.Host))
			provider.Hosts[newID] = host
			applied++
		case localRefreshFixMergeHost:
			delete(provider.Hosts, finding.fix.host.Host)
			target, ok := provider.Hosts[finding.fix.target.Host]
			if !ok {
				return applied, fmt.Errorf("merge target %q not found in local inventory", finding.fix.target.Host)
			}
			target.Aliases = uniqueHostPatterns(append(target.Aliases, finding.fix.alias))
			provider.Hosts[finding.fix.target.Host] = target
			applied++
		}
	}

	if applied > 0 {
		cfg.Inventory.Providers[config.ProviderLocal] = provider
		cfg.Inventory.Provider = cfg.Inventory.Providers
		if err := saveLocalProviderInventory(cfg, paths); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func renameLocalRefreshHost(host *sshconfig.HostEntry, newID, cnameTarget string) {
	if strings.TrimSpace(newID) == "" {
		newID = host.Host
	}
	if !strings.EqualFold(newID, host.Host) {
		setLocalRefreshHostPatterns(host, append([]string{newID}, hostPatterns(host)...))
		host.Host = newID
	}
	upsertDirective(host, "HostName", cnameTarget)
	host.HostName = cnameTarget
	host.Properties["hostname"] = cnameTarget
}

func addLocalRefreshHostAlias(host *sshconfig.HostEntry, alias string) {
	if strings.TrimSpace(alias) == "" {
		return
	}
	setLocalRefreshHostPatterns(host, append(hostPatterns(host), alias))
}

func setLocalRefreshHostPatterns(host *sshconfig.HostEntry, patterns []string) {
	patterns = uniqueHostPatterns(patterns)
	for i, line := range host.Lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "host ") {
			host.Lines[i] = fmt.Sprintf("Host %s\n", strings.Join(patterns, " "))
			host.Patterns = patterns
			return
		}
	}
	host.Lines = append([]string{fmt.Sprintf("Host %s\n", strings.Join(patterns, " "))}, host.Lines...)
	host.Patterns = patterns
}

func removeHostEntryByLines(hosts []*sshconfig.HostEntry, lines []string) []*sshconfig.HostEntry {
	removed := false
	result := make([]*sshconfig.HostEntry, 0, len(hosts))
	for _, host := range hosts {
		if !removed && linesEqual(host.Lines, lines) {
			removed = true
			continue
		}
		result = append(result, host)
	}
	return result
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func localHostPatternIndex(hosts []*sshconfig.HostEntry, cfg *config.Config, paths *config.Paths, index map[string]*inventory.HostInfo) map[string]*sshconfig.HostEntry {
	result := make(map[string]*sshconfig.HostEntry)
	for _, host := range hosts {
		if metadataForHost(host, cfg, paths, index).Owner != "local" {
			continue
		}
		for _, pattern := range hostPatterns(host) {
			key := strings.ToLower(pattern)
			if result[key] == nil {
				result[key] = host
			}
		}
	}
	return result
}

func hostPatterns(host *sshconfig.HostEntry) []string {
	if host == nil {
		return nil
	}
	if len(host.Patterns) > 0 {
		return host.Patterns
	}
	if host.Host == "" {
		return nil
	}
	return []string{host.Host}
}

func uniqueHostPatterns(patterns []string) []string {
	seen := make(map[string]bool, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		key := strings.ToLower(pattern)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, pattern)
	}
	return result
}

func localRefreshDNSTarget(host *sshconfig.HostEntry) string {
	if host == nil {
		return ""
	}
	if host.HostName != "" && host.HostName != host.Host {
		return host.HostName
	}
	return host.Host
}

func localRefreshResolveTarget(target string) localRefreshDNSResult {
	if target == "" || strings.ContainsAny(target, "*?") {
		return localRefreshDNSResult{status: "skip", detail: "wildcard pattern"}
	}
	if net.ParseIP(target) != nil {
		return localRefreshDNSResult{status: "skip", detail: "IP address"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(ctx, target); err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return localRefreshDNSResult{status: "nxdomain"}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(err.Error(), "i/o timeout") {
			return localRefreshDNSResult{status: "timeout"}
		}
		return localRefreshDNSResult{status: "error", detail: err.Error()}
	}
	cnameCtx, cnameCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cnameCancel()
	cname, err := net.DefaultResolver.LookupCNAME(cnameCtx, target)
	if err == nil {
		cname = strings.TrimSuffix(cname, ".")
		if !strings.EqualFold(cname, target) {
			return localRefreshDNSResult{status: "cname", detail: cname}
		}
	}
	return localRefreshDNSResult{status: "ok"}
}

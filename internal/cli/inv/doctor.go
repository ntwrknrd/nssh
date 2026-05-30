package inv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check inventory health",
		Long:  "Check local-provider inventory for stale hosts, duplicates, and rename candidates.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

type doctorFinding struct {
	Host   string
	Group  string
	Issue  string
	Detail string
	host   *sshconfig.HostEntry
	fix    doctorFix
}

type doctorFixKind int

const (
	doctorFixNone doctorFixKind = iota
	doctorFixRemoveHost
	doctorFixRemoveDuplicate
	doctorFixRenameHost
	doctorFixMergeHost
)

type doctorFix struct {
	kind        doctorFixKind
	host        *sshconfig.HostEntry
	target      *sshconfig.HostEntry
	newID       string
	cnameTarget string
	alias       string
}

type doctorDNSResult struct {
	status string
	detail string
}

func runDoctor() error {
	ui.CommandStart("INVENTORY DOCTOR")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	hosts, err := inventoryHosts(sshconfig.NewParser(), cfg, config.DefaultPaths())
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	table := ui.NewStreamTable("Host", "Group", "Issue", "Detail").
		WithColumnWidths(doctorTableWidths(hosts, cfg, config.DefaultPaths(), index)...)
	var findings []doctorFinding
	count := visitDoctorFindings(hosts, cfg, config.DefaultPaths(), index, doctorResolveTarget, func(finding doctorFinding) {
		findings = append(findings, finding)
		table.AddRow(finding.Host, finding.Group, finding.Issue, finding.Detail)
	})
	if count == 0 {
		ui.Noop("No inventory issues found")
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}
	table.Close()
	fixable := fixableDoctorFindings(findings)
	if len(fixable) == 0 {
		ui.Warning("No local fixes are available for these findings")
		ui.CommandEnd(ui.StatusWarning)
		return nil
	}

	selected, err := selectDoctorFixes(fixable)
	if err != nil {
		ui.Abort("Selection failed: %s", err)
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}
	if len(selected) == 0 {
		ui.Abort("No fixes selected")
		ui.CommandEnd(ui.StatusAbort)
		return nil
	}

	ui.SubSection("Applying")
	applied, err := applyDoctorFixes(sshconfig.NewParser(), config.DefaultPaths(), selected)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	ui.Success("Applied %d fix(es)", applied)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func visitDoctorFindings(
	hosts []*sshconfig.HostEntry,
	cfg *config.Config,
	paths *config.Paths,
	index map[string]*inventory.HostInfo,
	dnsCheck func(string) doctorDNSResult,
	emit func(doctorFinding),
) int {
	if dnsCheck == nil {
		dnsCheck = doctorResolveTarget
	}
	count := 0
	emitFinding := func(finding doctorFinding) {
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
				finding := doctorFinding{
					Host:   host.Host,
					Group:  meta.Group,
					Issue:  "duplicate",
					Detail: fmt.Sprintf("pattern %q also appears in %s", pattern, prev.SourceFile),
					host:   host,
				}
				if meta.Owner == "local" {
					finding.fix = doctorFix{kind: doctorFixRemoveDuplicate, host: host}
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
		target := doctorDNSTarget(host)
		result := dnsCheck(target)
		switch result.status {
		case "nxdomain":
			emitFinding(doctorFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "stale-dns",
				Detail: fmt.Sprintf("%s: NXDOMAIN", target),
				host:   host,
				fix:    doctorFix{kind: doctorFixRemoveHost, host: host},
			})
		case "timeout":
			emitFinding(doctorFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "stale-dns",
				Detail: fmt.Sprintf("%s: lookup timeout", target),
				host:   host,
				fix:    doctorFix{kind: doctorFixRemoveHost, host: host},
			})
		case "cname":
			newID := sshconfig.DeriveHostID(result.detail)
			fix := doctorFix{kind: doctorFixRenameHost, host: host, newID: newID, cnameTarget: result.detail}
			if existing := localHosts[strings.ToLower(newID)]; existing != nil && existing != host {
				fix = doctorFix{kind: doctorFixMergeHost, host: host, target: existing, alias: host.Host}
			}
			emitFinding(doctorFinding{
				Host:   host.Host,
				Group:  meta.Group,
				Issue:  "cname-rename",
				Detail: fmt.Sprintf("%s -> %s", target, result.detail),
				host:   host,
				fix:    fix,
			})
		case "error":
			emitFinding(doctorFinding{
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

func doctorTableWidths(hosts []*sshconfig.HostEntry, cfg *config.Config, paths *config.Paths, index map[string]*inventory.HostInfo) []int {
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

func fixableDoctorFindings(findings []doctorFinding) []doctorFinding {
	fixable := make([]doctorFinding, 0, len(findings))
	for i := range findings {
		finding := findings[i]
		if finding.fix.kind != doctorFixNone {
			fixable = append(fixable, finding)
		}
	}
	return fixable
}

func selectDoctorFixes(findings []doctorFinding) ([]doctorFinding, error) {
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
	selected := make([]doctorFinding, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(findings) {
			selected = append(selected, findings[idx])
		}
	}
	return selected, nil
}

func applyDoctorFixes(parser *sshconfig.Parser, paths *config.Paths, findings []doctorFinding) (int, error) {
	if parser == nil {
		parser = sshconfig.NewParser()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	parsedByPath := make(map[string]*sshconfig.ParsedConfig)
	dirty := make(map[string]bool)
	getParsed := func(path string) (*sshconfig.ParsedConfig, error) {
		if path == "" {
			return nil, fmt.Errorf("host source file is unknown")
		}
		if parsed := parsedByPath[path]; parsed != nil {
			return parsed, nil
		}
		parsed, err := parser.ParseFile(path)
		if err != nil {
			return nil, err
		}
		parsedByPath[path] = parsed
		return parsed, nil
	}
	markDirty := func(parsed *sshconfig.ParsedConfig) {
		if parsed != nil {
			dirty[parsed.Path] = true
		}
	}

	applied := 0
	for i := range findings {
		finding := findings[i]
		switch finding.fix.kind {
		case doctorFixNone:
			continue
		case doctorFixRemoveHost:
			parsed, err := getParsed(finding.fix.host.SourceFile)
			if err != nil {
				return applied, err
			}
			parsed.Hosts = sshconfig.RemoveHost(parsed.Hosts, finding.fix.host.Host)
			markDirty(parsed)
			applied++
		case doctorFixRemoveDuplicate:
			parsed, err := getParsed(finding.fix.host.SourceFile)
			if err != nil {
				return applied, err
			}
			parsed.Hosts = removeHostEntryByLines(parsed.Hosts, finding.fix.host.Lines)
			markDirty(parsed)
			applied++
		case doctorFixRenameHost:
			parsed, err := getParsed(finding.fix.host.SourceFile)
			if err != nil {
				return applied, err
			}
			host := sshconfig.FindHostByPattern(parsed.Hosts, finding.fix.host.Host)
			if host == nil {
				return applied, fmt.Errorf("host %q not found in %s", finding.fix.host.Host, parsed.Path)
			}
			renameDoctorHost(host, finding.fix.newID, finding.fix.cnameTarget)
			markDirty(parsed)
			applied++
		case doctorFixMergeHost:
			oldParsed, err := getParsed(finding.fix.host.SourceFile)
			if err != nil {
				return applied, err
			}
			targetParsed, err := getParsed(finding.fix.target.SourceFile)
			if err != nil {
				return applied, err
			}
			oldParsed.Hosts = removeHostEntryByLines(oldParsed.Hosts, finding.fix.host.Lines)
			target := sshconfig.FindHostByPattern(targetParsed.Hosts, finding.fix.target.Host)
			if target == nil {
				return applied, fmt.Errorf("merge target %q not found in %s", finding.fix.target.Host, targetParsed.Path)
			}
			addDoctorHostAlias(target, finding.fix.alias)
			markDirty(oldParsed)
			markDirty(targetParsed)
			applied++
		}
	}

	pathsToWrite := make([]string, 0, len(dirty))
	for path := range dirty {
		pathsToWrite = append(pathsToWrite, path)
	}
	sort.Strings(pathsToWrite)
	for _, path := range pathsToWrite {
		parsed := parsedByPath[path]
		sshconfig.SortHosts(parsed.Hosts)
		if err := writeParsedConfig(parser, parsed, paths); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func renameDoctorHost(host *sshconfig.HostEntry, newID, cnameTarget string) {
	if strings.TrimSpace(newID) == "" {
		newID = host.Host
	}
	if !strings.EqualFold(newID, host.Host) {
		setDoctorHostPatterns(host, append([]string{newID}, hostPatterns(host)...))
		host.Host = newID
	}
	upsertDirective(host, "HostName", cnameTarget)
	host.HostName = cnameTarget
	host.Properties["hostname"] = cnameTarget
}

func addDoctorHostAlias(host *sshconfig.HostEntry, alias string) {
	if strings.TrimSpace(alias) == "" {
		return
	}
	setDoctorHostPatterns(host, append(hostPatterns(host), alias))
}

func setDoctorHostPatterns(host *sshconfig.HostEntry, patterns []string) {
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

func doctorDNSTarget(host *sshconfig.HostEntry) string {
	if host == nil {
		return ""
	}
	if host.HostName != "" && host.HostName != host.Host {
		return host.HostName
	}
	return host.Host
}

func doctorResolveTarget(target string) doctorDNSResult {
	if target == "" || strings.ContainsAny(target, "*?") {
		return doctorDNSResult{status: "skip", detail: "wildcard pattern"}
	}
	if net.ParseIP(target) != nil {
		return doctorDNSResult{status: "skip", detail: "IP address"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(ctx, target); err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return doctorDNSResult{status: "nxdomain"}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(err.Error(), "i/o timeout") {
			return doctorDNSResult{status: "timeout"}
		}
		return doctorDNSResult{status: "error", detail: err.Error()}
	}
	cname, err := net.LookupCNAME(target)
	if err == nil {
		cname = strings.TrimSuffix(cname, ".")
		if !strings.EqualFold(cname, target) {
			return doctorDNSResult{status: "cname", detail: cname}
		}
	}
	return doctorDNSResult{status: "ok"}
}

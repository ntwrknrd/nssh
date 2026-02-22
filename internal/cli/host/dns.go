package host

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

// dnsResult holds the DNS resolution result for a single host.
type dnsResult struct {
	host   *sshconfig.HostEntry
	target string // what we resolved (HostName or Host)
	status string // "ok", "nxdomain", "cname", "timeout", "error", "skip"
	detail string // CNAME target, error message, or skip reason
}

// resolveAllHosts resolves DNS for all hosts concurrently.
// Returns results in the same order as input.
func resolveAllHosts(hosts []*sshconfig.HostEntry) []dnsResult {
	results := make([]dnsResult, len(hosts))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 20) // 20-worker pool

	for i, h := range hosts {
		results[i].host = h

		// Determine what to resolve
		target := h.Host
		if h.HostName != "" && h.HostName != h.Host {
			target = h.HostName
		}
		results[i].target = target

		// Skip wildcards
		if strings.ContainsAny(target, "*?") {
			results[i].status = "skip"
			results[i].detail = "wildcard pattern"
			continue
		}

		// Skip IP addresses
		if net.ParseIP(target) != nil {
			results[i].status = "skip"
			results[i].detail = "IP address"
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, t string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx].status, results[idx].detail = resolveOne(t)
		}(i, target)
	}

	wg.Wait()
	return results
}

// resolveOne performs DNS lookup for a single target with retry on timeout.
// After retries exhaust, persistent timeouts are promoted to "timeout" status
// so they can be surfaced as prune candidates separately from real errors.
func resolveOne(target string) (string, string) {
	const maxAttempts = 3

	for attempt := range maxAttempts {
		status, detail := resolveOnce(target)
		if status != "error" {
			return status, detail
		}
		// Only retry on timeout errors
		if !strings.Contains(detail, "i/o timeout") {
			return status, detail
		}
		if attempt == maxAttempts-1 {
			return "timeout", ""
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	return "timeout", "" // should not happen
}

func resolveOnce(target string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := net.DefaultResolver.LookupHost(ctx, target)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return "nxdomain", ""
		}
		return "error", err.Error()
	}

	// Check for CNAME
	cname, err := net.LookupCNAME(target)
	if err == nil {
		cname = strings.TrimSuffix(cname, ".")
		if !strings.EqualFold(cname, target) {
			return "cname", cname
		}
	}

	return "ok", ""
}

// dnsSummary holds counts from DNS resolution.
type dnsSummary struct {
	ok       int
	nxdomain int
	cname    int
	timeout  int
	skip     int
	errCount int
}

// deduplicateHosts removes duplicate host entries (same Host name).
// Keeps the first occurrence of each.
func deduplicateHosts(hosts []*sshconfig.HostEntry) []*sshconfig.HostEntry {
	seen := make(map[string]bool)
	result := make([]*sshconfig.HostEntry, 0, len(hosts))
	for _, h := range hosts {
		key := strings.ToLower(h.Host)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, h)
	}
	return result
}

// findDuplicatesInFile returns host entries that share a Host name
// within the same source file. For each set of N duplicates, returns
// the last N-1 entries (keeping the first as the canonical one).
func findDuplicatesInFile(hosts []*sshconfig.HostEntry) []*sshconfig.HostEntry {
	// key: lowercase(host) + "\x00" + sourceFile
	type entry struct {
		count int
		hosts []*sshconfig.HostEntry
	}
	seen := make(map[string]*entry)

	for _, h := range hosts {
		key := strings.ToLower(h.Host) + "\x00" + h.SourceFile
		e, ok := seen[key]
		if !ok {
			e = &entry{}
			seen[key] = e
		}
		e.count++
		e.hosts = append(e.hosts, h)
	}

	var dupes []*sshconfig.HostEntry
	for _, e := range seen {
		if e.count > 1 {
			// Skip the first (canonical), return the rest as duplicates
			dupes = append(dupes, e.hosts[1:]...)
		}
	}
	return dupes
}

// removeHostByLines removes exactly one host entry from the list by matching
// its raw Lines slice. This allows removing a specific duplicate without
// removing all entries that share the same Host name.
func removeHostByLines(hosts []*sshconfig.HostEntry, lines []string) []*sshconfig.HostEntry {
	removed := false
	result := make([]*sshconfig.HostEntry, 0, len(hosts))
	for _, h := range hosts {
		if !removed && linesEqual(h.Lines, lines) {
			removed = true
			continue
		}
		result = append(result, h)
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

// summarizeDNS counts results by status.
func summarizeDNS(results []dnsResult) dnsSummary {
	var s dnsSummary
	for _, r := range results {
		switch r.status {
		case "ok":
			s.ok++
		case "nxdomain":
			s.nxdomain++
		case "cname":
			s.cname++
		case "timeout":
			s.timeout++
		case "skip":
			s.skip++
		case "error":
			s.errCount++
		}
	}
	return s
}

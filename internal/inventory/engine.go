package inventory

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/ntwrknrd/nssh/internal/config"
)

// Plan describes the changes a provider refresh would make.
type Plan struct {
	Adds      []*ProviderHost
	Updates   []*ProviderHost
	Removals  []*ProviderHost
	Unchanged []*ProviderHost
	Unmatched []Object
	Conflicts []GroupConflict
}

// GroupConflict is an object that matched more than one group selector.
type GroupConflict struct {
	Object Object
	Groups []string
}

// Reconcile assigns discovered objects to groups and diffs desired state
// against current provider state.
func Reconcile(objects []Object, selectors []config.InventoryGroupSelector, providerName string, current *ProviderState, groups map[string]config.GroupConfig) *Plan {
	plan := &Plan{}
	desired := make(map[string]*ProviderHost)
	for i := range objects {
		obj := &objects[i]
		matches := MatchGroupSelectors(obj, selectors)
		switch len(matches) {
		case 0:
			plan.Unmatched = append(plan.Unmatched, *obj)
			continue
		case 1:
			host := groupProviderHost(obj, matches[0].Group, groups)
			desired[host.ObjectID] = host
		default:
			if selector, ok := BestGroupSelectorMatch(obj, matches); ok {
				host := groupProviderHost(obj, selector.Group, groups)
				desired[host.ObjectID] = host
				continue
			}
			plan.Conflicts = append(plan.Conflicts, GroupConflict{Object: *obj, Groups: selectorGroups(matches)})
			continue
		}
	}

	var currentObjects map[string]*ProviderHost
	if current != nil {
		currentObjects = current.Objects
	}
	for id, next := range desired {
		if currentObjects == nil {
			plan.Adds = append(plan.Adds, next)
			continue
		}
		prev, exists := currentObjects[id]
		if exists {
			next.CompatFixes = slices.Clone(prev.CompatFixes)
		}
		switch {
		case !exists:
			plan.Adds = append(plan.Adds, next)
		case providerHostChanged(prev, next):
			plan.Updates = append(plan.Updates, next)
		default:
			plan.Unchanged = append(plan.Unchanged, next)
		}
	}
	for id, prev := range currentObjects {
		if _, ok := desired[id]; !ok {
			plan.Removals = append(plan.Removals, prev)
		}
	}
	return plan
}

func selectorGroups(selectors []config.InventoryGroupSelector) []string {
	groups := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		groups = append(groups, selector.Group)
	}
	return groups
}

func groupProviderHost(obj *Object, group string, groups map[string]config.GroupConfig) *ProviderHost {
	groupCfg := groupConfig(group, groups)
	qualified := qualifyShortObject(*obj, groupCfg.DomainSuffix)
	host := objectToProviderHost(&qualified, group)
	host.AuthMode = groupAuthMode(group, groups)
	return host
}

func groupConfig(group string, groups map[string]config.GroupConfig) config.GroupConfig {
	_, shortGroup, err := config.ParseInventoryGroupID(group)
	if err != nil {
		shortGroup = group
	}
	return groups[shortGroup]
}

// qualifyShortObject appends a group's single domain suffix to an object whose
// name has no domain and whose provider supplied no other address, so short
// provider names become resolvable FQDN hosts.
func qualifyShortObject(obj Object, suffixes []string) Object {
	if len(suffixes) != 1 {
		return obj
	}
	suffix := strings.TrimSpace(suffixes[0])
	name := strings.TrimSpace(obj.Name)
	if suffix == "" || name == "" || strings.Contains(name, ".") {
		return obj
	}
	if host := strings.TrimSpace(obj.HostName); host != "" && host != name {
		return obj
	}
	if !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	obj.Name = name + suffix
	obj.FQDN = obj.Name
	obj.HostName = obj.Name
	return obj
}

func groupAuthMode(group string, groups map[string]config.GroupConfig) string {
	if groups != nil {
		auth := groupConfig(group, groups).Auth
		auth.Normalize()
		if auth.Mode != "" {
			return auth.Mode
		}
		if auth.PasswordRef != "" {
			return config.AuthModePassword
		}
	}
	return config.AuthModeKey
}

func objectToProviderHost(obj *Object, group string) *ProviderHost {
	return &ProviderHost{
		ObjectID:  obj.ObjectID,
		Host:      obj.Name,
		Patterns:  providerHostPatterns(obj.Name, obj.HostName),
		Group:     group,
		HostName:  obj.HostName,
		Port:      obj.Port,
		ProxyJump: obj.ProxyJump,
	}
}

func providerHostPatterns(name, hostname string) []string {
	patterns := make([]string, 0, 3)
	add := func(pattern string) {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || slices.Contains(patterns, pattern) {
			return
		}
		patterns = append(patterns, pattern)
	}
	add(name)
	add(shortDNSName(name))
	add(shortDNSName(hostname))
	return patterns
}

func shortDNSName(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" || !strings.Contains(host, ".") || strings.Contains(host, ":") {
		return ""
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	short, _, ok := strings.Cut(host, ".")
	if !ok {
		return ""
	}
	return short
}

func providerHostChanged(old, new *ProviderHost) bool {
	if old.Host != new.Host ||
		old.Group != new.Group ||
		old.HostName != new.HostName ||
		old.Port != new.Port ||
		old.ProxyJump != new.ProxyJump ||
		old.AuthMode != new.AuthMode ||
		old.ProviderType != new.ProviderType {
		return true
	}
	if !slices.Equal(old.Patterns, new.Patterns) {
		return true
	}
	if !slices.Equal(old.CompatFixes, new.CompatFixes) {
		return true
	}
	return false
}

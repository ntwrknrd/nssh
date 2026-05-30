package inventory

import (
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
}

// Reconcile routes discovered objects into groups and diffs desired state
// against current provider state.
func Reconcile(objects []Object, routes []config.InventoryRouteConfig, providerName string, current *ProviderState, groups ...map[string]config.GroupConfig) *Plan {
	plan := &Plan{}
	desired := make(map[string]*ProviderHost)
	for i := range objects {
		obj := &objects[i]
		route := MatchRoute(obj, routes)
		if route == nil {
			plan.Unmatched = append(plan.Unmatched, *obj)
			continue
		}
		host := objectToProviderHost(obj, route.Group)
		host.Username = groupDefaultUser(route.Group, groups...)
		host.AuthMode = routeAuthMode(*route, groups...)
		desired[host.ObjectID] = host
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

func groupDefaultUser(group string, groups ...map[string]config.GroupConfig) string {
	if len(groups) == 0 || groups[0] == nil {
		return ""
	}
	return strings.TrimSpace(groups[0][group].DefaultUser)
}

func routeAuthMode(route config.InventoryRouteConfig, groups ...map[string]config.GroupConfig) string {
	if route.AuthMode != "" {
		return route.AuthMode
	}
	if len(groups) > 0 && groups[0] != nil && groups[0][route.Group].Auth.IsSet() {
		return config.AuthModePassword
	}
	return config.AuthModeKey
}

func objectToProviderHost(obj *Object, group string) *ProviderHost {
	return &ProviderHost{
		ObjectID:  obj.ObjectID,
		Host:      obj.Name,
		Patterns:  []string{obj.Name},
		Group:     group,
		HostName:  obj.HostName,
		Port:      obj.Port,
		ProxyJump: obj.ProxyJump,
	}
}

func providerHostChanged(old, new *ProviderHost) bool {
	if old.Host != new.Host ||
		old.Group != new.Group ||
		old.HostName != new.HostName ||
		old.Username != new.Username ||
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

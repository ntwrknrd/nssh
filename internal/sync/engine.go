package sync

import (
	"slices"

	"github.com/ntwrknrd/nssh/internal/config"
)

// SyncPlan describes the changes a sync run would make.
type SyncPlan struct {
	Adds      []*ManagedHost
	Updates   []*ManagedHost
	Removals  []*ManagedHost
	Unchanged []*ManagedHost
	Unmatched []InventoryObject
}

// Reconcile compares discovered objects against current state to produce a plan.
// It evaluates routes for each object, converts matched objects to ManagedHosts,
// and diffs against the existing state to determine adds/updates/removals.
func Reconcile(
	objects []InventoryObject,
	routes []config.SyncRouteConfig,
	sourceName string,
	current *SourceState,
) *SyncPlan {
	return ReconcileWithReservedTargets(objects, routes, sourceName, current, nil)
}

// ReconcileWithReservedTargets behaves like Reconcile but skips discovered
// objects whose resolved target already exists elsewhere in SSH config.
func ReconcileWithReservedTargets(
	objects []InventoryObject,
	routes []config.SyncRouteConfig,
	sourceName string,
	current *SourceState,
	reservedTargets map[string]struct{},
) *SyncPlan {
	plan := &SyncPlan{}

	// Build desired state from discovered objects
	desired := make(map[string]*ManagedHost)
	for i := range objects {
		obj := &objects[i]
		route := MatchRoute(obj, routes)
		if route == nil {
			plan.Unmatched = append(plan.Unmatched, *obj)
			continue
		}

		ctx, _ := ResolveDestination(route, sourceName)
		mh := objectToManagedHost(obj, ctx)
		if isReservedTarget(mh.HostName, reservedTargets) {
			continue
		}
		desired[mh.ObjectID] = mh
	}

	// Diff desired vs current
	var currentObjects map[string]*ManagedHost
	if current != nil {
		currentObjects = current.Objects
	}

	for id, dh := range desired {
		if currentObjects == nil {
			plan.Adds = append(plan.Adds, dh)
			continue
		}
		ch, exists := currentObjects[id]
		if exists {
			dh.CompatFixes = slices.Clone(ch.CompatFixes)
		}
		switch {
		case !exists:
			plan.Adds = append(plan.Adds, dh)
		case managedHostChanged(ch, dh):
			plan.Updates = append(plan.Updates, dh)
		default:
			plan.Unchanged = append(plan.Unchanged, dh)
		}
	}

	// Removals: in current but not in desired
	for id, ch := range currentObjects {
		if _, exists := desired[id]; !exists {
			plan.Removals = append(plan.Removals, ch)
		}
	}

	return plan
}

// objectToManagedHost converts an InventoryObject + route outcome into a ManagedHost.
func objectToManagedHost(obj *InventoryObject, context string) *ManagedHost {
	return &ManagedHost{
		ObjectID:        obj.ObjectID,
		Host:            obj.Name,
		Patterns:        []string{obj.Name},
		Context:         context,
		HostName:        obj.HostName,
		Username:        "",
		Port:            obj.Port,
		ProxyJump:       obj.ProxyJump,
		UsesPassword:    obj.UsesPassword,
		CredentialClass: obj.CredentialClass,
	}
}

// managedHostChanged returns true if any relevant field differs.
func managedHostChanged(old, new *ManagedHost) bool {
	if old.Host != new.Host {
		return true
	}
	if old.HostName != new.HostName {
		return true
	}
	if old.Username != new.Username {
		return true
	}
	if old.Port != new.Port {
		return true
	}
	if old.ProxyJump != new.ProxyJump {
		return true
	}
	if old.UsesPassword != new.UsesPassword {
		return true
	}
	if old.CredentialClass != new.CredentialClass {
		return true
	}
	if old.Context != new.Context {
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

func isReservedTarget(target string, reservedTargets map[string]struct{}) bool {
	if len(reservedTargets) == 0 {
		return false
	}
	_, ok := reservedTargets[target]
	return ok
}

package sync

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestReconcileAdds(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "all",
		Context: "lab",
		Match:   config.SyncRouteMatch{},
	}}

	objects := []InventoryObject{
		{ObjectID: "lab1/core01", Name: "clab-core01", HostName: "172.20.0.2"},
		{ObjectID: "lab1/core02", Name: "clab-core02", HostName: "172.20.0.3"},
	}

	plan := Reconcile(objects, routes, "test-lab", nil)

	if len(plan.Adds) != 2 {
		t.Errorf("adds = %d, want 2", len(plan.Adds))
	}
	if len(plan.Updates) != 0 {
		t.Errorf("updates = %d, want 0", len(plan.Updates))
	}
	if len(plan.Removals) != 0 {
		t.Errorf("removals = %d, want 0", len(plan.Removals))
	}
}

func TestReconcileUpdates(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "all",
		Context: "lab",
		Match:   config.SyncRouteMatch{},
	}}

	objects := []InventoryObject{
		{ObjectID: "lab1/core01", Name: "clab-core01", HostName: "172.20.0.99"}, // changed IP
	}

	current := &SourceState{
		Objects: map[string]*ManagedHost{
			"lab1/core01": {
				ObjectID:    "lab1/core01",
				Host:        "clab-core01",
				Patterns:    []string{"clab-core01"},
				HostName:    "172.20.0.2", // old IP
				Context:     "lab",
				IncludeFile: "conf.d/sync_test-lab",
			},
		},
	}

	plan := Reconcile(objects, routes, "test-lab", current)

	if len(plan.Adds) != 0 {
		t.Errorf("adds = %d, want 0", len(plan.Adds))
	}
	if len(plan.Updates) != 1 {
		t.Errorf("updates = %d, want 1", len(plan.Updates))
	}
	if len(plan.Removals) != 0 {
		t.Errorf("removals = %d, want 0", len(plan.Removals))
	}
}

func TestReconcileRemovals(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "all",
		Context: "lab",
		Match:   config.SyncRouteMatch{},
	}}

	// No objects discovered, but one exists in state
	current := &SourceState{
		Objects: map[string]*ManagedHost{
			"lab1/gone": {
				ObjectID: "lab1/gone",
				Host:     "clab-gone",
				HostName: "172.20.0.5",
			},
		},
	}

	plan := Reconcile(nil, routes, "test-lab", current)

	if len(plan.Removals) != 1 {
		t.Errorf("removals = %d, want 1", len(plan.Removals))
	}
}

func TestReconcileUnmatched(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "ceos-only",
		Context: "lab",
		Match:   config.SyncRouteMatch{"credential_class": {"ceos"}},
	}}

	objects := []InventoryObject{
		{ObjectID: "lab1/core01", Name: "clab-core01", HostName: "172.20.0.2", CredentialClass: "ceos"},
		{ObjectID: "lab1/spine01", Name: "clab-spine01", HostName: "172.20.0.3", CredentialClass: "vjunos"},
	}

	plan := Reconcile(objects, routes, "test-lab", nil)

	if len(plan.Adds) != 1 {
		t.Errorf("adds = %d, want 1", len(plan.Adds))
	}
	if len(plan.Unmatched) != 1 {
		t.Errorf("unmatched = %d, want 1", len(plan.Unmatched))
	}
	if plan.Unmatched[0].Name != "clab-spine01" {
		t.Errorf("unmatched[0].Name = %q", plan.Unmatched[0].Name)
	}
}

func TestReconcileUnchanged(t *testing.T) {
	routes := []config.SyncRouteConfig{{
		Name:    "all",
		Context: "lab",
		Match:   config.SyncRouteMatch{},
	}}

	objects := []InventoryObject{
		{ObjectID: "lab1/core01", Name: "clab-core01", HostName: "172.20.0.2"},
	}

	current := &SourceState{
		Objects: map[string]*ManagedHost{
			"lab1/core01": {
				ObjectID:    "lab1/core01",
				Host:        "clab-core01",
				Patterns:    []string{"clab-core01"},
				HostName:    "172.20.0.2",
				Context:     "lab",
				IncludeFile: "conf.d/sync_test-lab",
			},
		},
	}

	plan := Reconcile(objects, routes, "test-lab", current)

	if len(plan.Unchanged) != 1 {
		t.Errorf("unchanged = %d, want 1", len(plan.Unchanged))
	}
	if len(plan.Adds) != 0 {
		t.Errorf("adds = %d, want 0", len(plan.Adds))
	}
}

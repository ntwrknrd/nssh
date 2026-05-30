package inventory

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestReconcileRoutesObjectsToGroups(t *testing.T) {
	routes := []config.InventoryRouteConfig{{
		Group: "custcbb",
		Match: config.InventoryRouteMatch{
			"manufacturer": {"Juniper"},
			"status":       {"active"},
		},
	}}
	objects := []Object{{
		ObjectID: "device:1",
		Name:     "edge01",
		HostName: "edge01.custcbb.local",
		Attributes: map[string][]string{
			"manufacturer": {"Juniper"},
			"status":       {"active"},
		},
	}}

	plan := Reconcile(objects, routes, "netbox-prod", nil)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].Group != "custcbb" {
		t.Fatalf("group = %q", plan.Adds[0].Group)
	}
}

func TestReconcileAssignsRouteAuthMode(t *testing.T) {
	routes := []config.InventoryRouteConfig{{
		Group:    "servers",
		AuthMode: config.AuthModeKey,
		Match:    config.InventoryRouteMatch{"role": {"server"}},
	}}
	objects := []Object{{
		ObjectID:   "node:1",
		Name:       "app01",
		HostName:   "app01.example.com",
		Attributes: map[string][]string{"role": {"server"}},
	}}
	groups := map[string]config.GroupConfig{
		"servers": {Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/groups/servers"}},
	}

	plan := Reconcile(objects, routes, "netbox-prod", nil, groups)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].AuthMode != config.AuthModeKey {
		t.Fatalf("auth mode = %q, want %q", plan.Adds[0].AuthMode, config.AuthModeKey)
	}
}

func TestReconcileDefaultsAuthModeFromGroupAuth(t *testing.T) {
	routes := []config.InventoryRouteConfig{{
		Group: "network",
		Match: config.InventoryRouteMatch{"role": {"switch"}},
	}}
	objects := []Object{{
		ObjectID:   "device:1",
		Name:       "edge01",
		HostName:   "edge01.example.com",
		Attributes: map[string][]string{"role": {"switch"}},
	}}
	groups := map[string]config.GroupConfig{
		"network": {Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/groups/network"}},
	}

	plan := Reconcile(objects, routes, "netbox-prod", nil, groups)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].AuthMode != config.AuthModePassword {
		t.Fatalf("auth mode = %q, want %q", plan.Adds[0].AuthMode, config.AuthModePassword)
	}
}

func TestReconcileDefaultsAuthModeToKeyWithoutGroupAuth(t *testing.T) {
	routes := []config.InventoryRouteConfig{{
		Group: "servers",
		Match: config.InventoryRouteMatch{"role": {"server"}},
	}}
	objects := []Object{{
		ObjectID:   "node:1",
		Name:       "app01",
		HostName:   "app01.example.com",
		Attributes: map[string][]string{"role": {"server"}},
	}}
	groups := map[string]config.GroupConfig{"servers": {}}

	plan := Reconcile(objects, routes, "netbox-prod", nil, groups)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].AuthMode != config.AuthModeKey {
		t.Fatalf("auth mode = %q, want %q", plan.Adds[0].AuthMode, config.AuthModeKey)
	}
}

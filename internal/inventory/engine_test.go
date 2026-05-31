package inventory

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestReconcileSelectorsObjectsToGroups(t *testing.T) {
	selectors := []config.InventoryGroupSelector{{
		Group:    "netbox-prod/customer",
		Provider: "netbox-prod",
		Match: config.InventoryMatch{
			"manufacturer": {"Juniper"},
			"status":       {"active"},
		},
	}}
	objects := []Object{{
		ObjectID: "device:1",
		Name:     "edge01",
		HostName: "edge01.customer.local",
		Attributes: map[string][]string{
			"manufacturer": {"Juniper"},
			"status":       {"active"},
		},
	}}

	plan := Reconcile(objects, selectors, "netbox-prod", nil, nil)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].Group != "netbox-prod/customer" {
		t.Fatalf("group = %q", plan.Adds[0].Group)
	}
}

func TestReconcileDefaultsAuthModeFromGroupAuth(t *testing.T) {
	selectors := []config.InventoryGroupSelector{{
		Group:    "netbox-prod/network",
		Provider: "netbox-prod",
		Match:    config.InventoryMatch{"role": {"switch"}},
	}}
	objects := []Object{{
		ObjectID:   "device:1",
		Name:       "edge01",
		HostName:   "edge01.example.com",
		Attributes: map[string][]string{"role": {"switch"}},
	}}
	groups := map[string]config.GroupConfig{
		"network": {Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/network"}},
	}

	plan := Reconcile(objects, selectors, "netbox-prod", nil, groups)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].AuthMode != config.AuthModePassword {
		t.Fatalf("auth mode = %q, want %q", plan.Adds[0].AuthMode, config.AuthModePassword)
	}
}

func TestReconcileDefaultsAuthModeToKeyWithoutGroupAuth(t *testing.T) {
	selectors := []config.InventoryGroupSelector{{
		Group:    "netbox-prod/servers",
		Provider: "netbox-prod",
		Match:    config.InventoryMatch{"role": {"server"}},
	}}
	objects := []Object{{
		ObjectID:   "node:1",
		Name:       "app01",
		HostName:   "app01.example.com",
		Attributes: map[string][]string{"role": {"server"}},
	}}
	groups := map[string]config.GroupConfig{"servers": {}}

	plan := Reconcile(objects, selectors, "netbox-prod", nil, groups)
	if len(plan.Adds) != 1 {
		t.Fatalf("adds = %d, want 1", len(plan.Adds))
	}
	if plan.Adds[0].AuthMode != config.AuthModeKey {
		t.Fatalf("auth mode = %q, want %q", plan.Adds[0].AuthMode, config.AuthModeKey)
	}
}

func TestReconcileReportsConflictingGroupSelectors(t *testing.T) {
	selectors := []config.InventoryGroupSelector{
		{Group: "netbox-prod/customer", Provider: "netbox-prod", Match: config.InventoryMatch{"role": {"router"}}},
		{Group: "netbox-prod/network", Provider: "netbox-prod", Match: config.InventoryMatch{"role": {"router"}}},
	}
	objects := []Object{{
		ObjectID:   "device:1",
		Name:       "edge01",
		HostName:   "edge01.example.com",
		Attributes: map[string][]string{"role": {"router"}},
	}}

	plan := Reconcile(objects, selectors, "netbox-prod", nil, nil)
	if len(plan.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want one conflict", plan.Conflicts)
	}
	if len(plan.Adds) != 0 {
		t.Fatalf("adds = %+v, want no additions on conflict", plan.Adds)
	}
}

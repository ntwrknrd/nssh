package inv

import (
	"strings"
	"testing"
)

func TestInventoryTargetArgAllowsGroupModeWithPositionalName(t *testing.T) {
	target, err := inventoryTargetArg([]string{"homelab"}, true)
	if err != nil {
		t.Fatalf("inventoryTargetArg group mode: %v", err)
	}
	if target != "homelab" {
		t.Fatalf("target = %q, want homelab", target)
	}
}

func TestInventoryTargetArgRequiresModeSpecificName(t *testing.T) {
	_, err := inventoryTargetArg(nil, true)
	if err == nil || !strings.Contains(err.Error(), "group is required") {
		t.Fatalf("group mode error = %v, want group is required", err)
	}

	_, err = inventoryTargetArg(nil, false)
	if err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("host mode error = %v, want host is required", err)
	}
}

func TestValidateGroupNameRejectsFlagLikeNames(t *testing.T) {
	for _, name := range []string{"-h", "--help"} {
		err := validateLocalGroupID(name)
		if err == nil || (!strings.Contains(err.Error(), "provider-qualified") && !strings.Contains(err.Error(), "bare-key safe")) {
			t.Fatalf("validateGroupName(%q) = %v, want leading dash rejection", name, err)
		}
	}
}

func TestValidateGroupNameRejectsWhitespace(t *testing.T) {
	for _, name := range []string{"local/ lab", "local/lab ", "local/lab prod"} {
		err := validateLocalGroupID(name)
		if err == nil || (!strings.Contains(err.Error(), "provider-qualified") && !strings.Contains(err.Error(), "bare-key safe")) {
			t.Fatalf("validateGroupName(%q) = %v, want character set rejection", name, err)
		}
	}
}

func TestValidateGroupNameAllowsBareKeySafeNames(t *testing.T) {
	for _, name := range []string{"homelab", "customer", "lab-1", "lab_1"} {
		if err := validateLocalGroupID("local/" + name); err != nil {
			t.Fatalf("validateGroupName(%q): %v", name, err)
		}
	}
}

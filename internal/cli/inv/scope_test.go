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
		err := validateGroupName(name)
		if err == nil || !strings.Contains(err.Error(), "cannot start with '-'") {
			t.Fatalf("validateGroupName(%q) = %v, want leading dash rejection", name, err)
		}
	}
}

func TestValidateGroupNameRejectsWhitespace(t *testing.T) {
	for _, name := range []string{" lab", "lab ", "lab prod"} {
		err := validateGroupName(name)
		if err == nil || !strings.Contains(err.Error(), "letters, numbers, underscores, and dashes") {
			t.Fatalf("validateGroupName(%q) = %v, want character set rejection", name, err)
		}
	}
}

func TestValidateGroupNameAllowsBareKeySafeNames(t *testing.T) {
	for _, name := range []string{"homelab", "custcbb", "lab-1", "lab_1"} {
		if err := validateGroupName(name); err != nil {
			t.Fatalf("validateGroupName(%q): %v", name, err)
		}
	}
}

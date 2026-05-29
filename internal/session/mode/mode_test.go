package mode

import "testing"

func TestOnlySoftwareModeIsValid(t *testing.T) {
	if !Software.Valid() {
		t.Fatal("software mode should remain valid")
	}

	for _, m := range []Mode{"piv", "fido2", "secureenclave", "hardware", ""} {
		if m.Valid() {
			t.Fatalf("mode %q should not be valid after hardware-key support removal", m)
		}
	}
}

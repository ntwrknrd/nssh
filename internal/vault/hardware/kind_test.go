package hardware

import "testing"

func TestKind_Valid(t *testing.T) {
	tests := []struct {
		kind  Kind
		valid bool
	}{
		{PIV, true},
		{FIDO2, true},
		{SecureEnclave, true},
		{"", false},
		{"invalid", false},
		{"PIV", false}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := tt.kind.Valid(); got != tt.valid {
				t.Errorf("Kind(%q).Valid() = %v, want %v", tt.kind, got, tt.valid)
			}
		})
	}
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{PIV, "piv"},
		{FIDO2, "fido2"},
		{SecureEnclave, "secureenclave"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

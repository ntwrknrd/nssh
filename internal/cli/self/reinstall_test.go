package self

import "testing"

func TestNormalizeRelease(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"v0.3.0", "v0.3.0"},
		{"0.3.0", "v0.3.0"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := normalizeRelease(tt.in)
			if err != nil {
				t.Fatalf("normalizeRelease(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRelease(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeReleaseRejectsShellUnsafeInput(t *testing.T) {
	for _, in := range []string{"v0.3.0;echo nope", "v0.3.0$(echo nope)", "../v0.3.0"} {
		if got, err := normalizeRelease(in); err == nil {
			t.Fatalf("normalizeRelease(%q) = %q, want error", in, got)
		}
	}
}

func TestInstallShellCommandTargetsRelease(t *testing.T) {
	got, err := installShellCommand("0.3.0")
	if err != nil {
		t.Fatalf("installShellCommand error = %v", err)
	}

	want := "curl -fsSL https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh | sh -s -- --release v0.3.0"
	if got != want {
		t.Fatalf("installShellCommand = %q, want %q", got, want)
	}
}

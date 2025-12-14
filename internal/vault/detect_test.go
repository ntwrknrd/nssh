package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ntwrknrd/nssh/internal/session/mode"
)

func TestDetectSecurityMode(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(dir string)
		wantMode    string
		wantErr     error
		wantErrText string
	}{
		{
			name:     "software mode - age.key.enc exists",
			setup:    func(dir string) { createFile(t, dir, "age.key.enc") },
			wantMode: string(mode.Software),
		},
		{
			name:     "hardware mode - piv.json exists",
			setup:    func(dir string) { createFile(t, dir, "piv.json") },
			wantMode: string(mode.PIV),
		},
		{
			name: "ambiguous - both exist",
			setup: func(dir string) {
				createFile(t, dir, "age.key.enc")
				createFile(t, dir, "piv.json")
			},
			wantErr: ErrAmbiguousMode,
		},
		{
			name:    "not initialized - neither exists",
			setup:   func(dir string) {},
			wantErr: ErrNotInitialized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			mode, err := DetectSecurityMode(dir)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("DetectSecurityMode() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("DetectSecurityMode() unexpected error = %v", err)
				return
			}
			if mode != tt.wantMode {
				t.Errorf("DetectSecurityMode() = %q, want %q", mode, tt.wantMode)
			}
		})
	}
}

func TestHasMultipleKeystores(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
		want  bool
	}{
		{
			name:  "neither exists",
			setup: func(dir string) {},
			want:  false,
		},
		{
			name:  "only software",
			setup: func(dir string) { createFile(t, dir, "age.key.enc") },
			want:  false,
		},
		{
			name:  "only hardware",
			setup: func(dir string) { createFile(t, dir, "piv.json") },
			want:  false,
		},
		{
			name: "both exist",
			setup: func(dir string) {
				createFile(t, dir, "age.key.enc")
				createFile(t, dir, "piv.json")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			got := HasMultipleKeystores(dir)
			if got != tt.want {
				t.Errorf("HasMultipleKeystores() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInitialized(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
		want  bool
	}{
		{
			name:  "neither exists",
			setup: func(dir string) {},
			want:  false,
		},
		{
			name:  "software initialized",
			setup: func(dir string) { createFile(t, dir, "age.key.enc") },
			want:  true,
		},
		{
			name:  "hardware initialized",
			setup: func(dir string) { createFile(t, dir, "piv.json") },
			want:  true,
		},
		{
			name: "both initialized",
			setup: func(dir string) {
				createFile(t, dir, "age.key.enc")
				createFile(t, dir, "piv.json")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			got := IsInitialized(dir)
			if got != tt.want {
				t.Errorf("IsInitialized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func createFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create %s: %v", name, err)
	}
}

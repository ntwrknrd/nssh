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
			name:    "piv marker alone is not initialized",
			setup:   func(dir string) { createFile(t, dir, "piv.json") },
			wantErr: ErrNotInitialized,
		},
		{
			name: "software wins even with leftover piv marker",
			setup: func(dir string) {
				createFile(t, dir, "age.key.enc")
				createFile(t, dir, "piv.json")
			},
			wantMode: string(mode.Software),
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
			name:  "legacy piv marker is not initialized",
			setup: func(dir string) { createFile(t, dir, "piv.json") },
			want:  false,
		},
		{
			name: "software initialized with leftover piv marker",
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

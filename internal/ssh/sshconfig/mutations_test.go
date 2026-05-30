package sshconfig

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

func TestApplyCompatFixes(t *testing.T) {
	tests := []struct {
		name        string
		hostLines   []string
		compatTypes []compat.CompatType
		wantLines   []string
		wantProps   map[string]string
	}{
		{
			name: "add kex fix",
			hostLines: []string{
				"Host testhost\n",
				"  HostName test.example.com\n",
				"  User admin\n",
				"\n",
			},
			compatTypes: []compat.CompatType{compat.CompatKex},
			wantLines: []string{
				"Host testhost\n",
				"  HostName test.example.com\n",
				"  KexAlgorithms +diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256\n",
				"  User admin\n",
				"\n",
			},
			wantProps: map[string]string{
				"kexalgorithms": "+diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256",
			},
		},
		{
			name: "add multiple fixes",
			hostLines: []string{
				"Host testhost\n",
				"  HostName test.example.com\n",
				"  Port 22\n",
				"\n",
			},
			compatTypes: []compat.CompatType{compat.CompatKex, compat.CompatCiphers},
			wantLines: []string{
				"Host testhost\n",
				"  HostName test.example.com\n",
				"  Port 22\n",
				"  KexAlgorithms +diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256\n",
				"  Ciphers +aes128-cbc,3des-cbc,aes192-cbc,aes256-cbc\n",
				"\n",
			},
			wantProps: map[string]string{
				"kexalgorithms": "+diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256",
				"ciphers":       "+aes128-cbc,3des-cbc,aes192-cbc,aes256-cbc",
			},
		},
		{
			name: "replace existing kex",
			hostLines: []string{
				"Host testhost\n",
				"  HostName test.example.com\n",
				"  KexAlgorithms +old-algo\n",
				"  User admin\n",
				"\n",
			},
			compatTypes: []compat.CompatType{compat.CompatKex},
			wantLines: []string{
				"Host testhost\n",
				"  HostName test.example.com\n",
				"  KexAlgorithms +diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256\n",
				"  User admin\n",
				"\n",
			},
			wantProps: map[string]string{
				"kexalgorithms": "+diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256",
			},
		},
		{
			name:        "empty compat types",
			hostLines:   []string{"Host testhost\n", "  HostName test.example.com\n"},
			compatTypes: []compat.CompatType{},
			wantLines:   []string{"Host testhost\n", "  HostName test.example.com\n"},
			wantProps:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := &HostEntry{
				Host:       "testhost",
				Lines:      tt.hostLines,
				Properties: make(map[string]string),
			}

			err := ApplyCompatFixes(host, tt.compatTypes)
			if err != nil {
				t.Fatalf("ApplyCompatFixes() error = %v", err)
			}

			if len(host.Lines) != len(tt.wantLines) {
				t.Errorf("Lines count = %d, want %d\nGot: %v\nWant: %v",
					len(host.Lines), len(tt.wantLines), host.Lines, tt.wantLines)
				return
			}

			for i, line := range host.Lines {
				if line != tt.wantLines[i] {
					t.Errorf("Lines[%d] = %q, want %q", i, line, tt.wantLines[i])
				}
			}

			for key, wantVal := range tt.wantProps {
				if gotVal := host.Properties[key]; gotVal != wantVal {
					t.Errorf("Properties[%q] = %q, want %q", key, gotVal, wantVal)
				}
			}
		})
	}
}

func TestInsertAt(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		idx    int
		insert []string
		want   []string
	}{
		{
			name:   "insert at beginning",
			lines:  []string{"a", "b", "c"},
			idx:    0,
			insert: []string{"x"},
			want:   []string{"x", "a", "b", "c"},
		},
		{
			name:   "insert in middle",
			lines:  []string{"a", "b", "c"},
			idx:    1,
			insert: []string{"x", "y"},
			want:   []string{"a", "x", "y", "b", "c"},
		},
		{
			name:   "insert at end",
			lines:  []string{"a", "b", "c"},
			idx:    3,
			insert: []string{"x"},
			want:   []string{"a", "b", "c", "x"},
		},
		{
			name:   "insert past end",
			lines:  []string{"a", "b"},
			idx:    10,
			insert: []string{"x"},
			want:   []string{"a", "b", "x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := insertAt(tt.lines, tt.idx, tt.insert)
			if len(got) != len(tt.want) {
				t.Errorf("insertAt() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("insertAt()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFindCompatInsertionPoint(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{
			name:  "after hostname",
			lines: []string{"Host test\n", "  HostName example.com\n", "  User admin\n"},
			want:  2,
		},
		{
			name:  "after port",
			lines: []string{"Host test\n", "  HostName example.com\n", "  Port 2222\n", "  User admin\n"},
			want:  3,
		},
		{
			name:  "default after host line",
			lines: []string{"Host test\n", "  User admin\n"},
			want:  1,
		},
		{
			name:  "stop at blank line",
			lines: []string{"Host test\n", "  HostName example.com\n", "\n", "  User admin\n"},
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findCompatInsertionPoint(tt.lines)
			if got != tt.want {
				t.Errorf("findCompatInsertionPoint() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestApplyCompatFixes_DirectiveInLines verifies the directive appears in lines
func TestApplyCompatFixes_DirectiveInLines(t *testing.T) {
	host := &HostEntry{
		Host: "testhost",
		Lines: []string{
			"Host testhost\n",
			"  HostName test.example.com\n",
			"\n",
		},
		Properties: make(map[string]string),
	}

	err := ApplyCompatFixes(host, []compat.CompatType{compat.CompatHostKey})
	if err != nil {
		t.Fatalf("ApplyCompatFixes() error = %v", err)
	}

	// Check that HostKeyAlgorithms appears in the lines
	found := false
	for _, line := range host.Lines {
		if strings.Contains(line, "HostKeyAlgorithms") {
			found = true
			if !strings.Contains(line, "ssh-rsa") {
				t.Error("HostKeyAlgorithms line missing ssh-rsa")
			}
			break
		}
	}
	if !found {
		t.Error("HostKeyAlgorithms not found in lines")
	}
}

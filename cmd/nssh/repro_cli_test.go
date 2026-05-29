package main

import (
	"reflect"
	"testing"
)

func TestPreprocessArgs_SSHFlagsAfterHostname(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "port flag before hostname",
			args:     []string{"-p", "2222", "somehost"},
			expected: []string{"smart-connect", "somehost", "-p", "2222"},
		},
		{
			name:     "verbose and port flags",
			args:     []string{"-v", "-p", "2222", "somehost"},
			expected: []string{"-v", "smart-connect", "somehost", "-p", "2222"},
		},
		{
			name:     "port flag after hostname (already correct)",
			args:     []string{"somehost", "-p", "2222"},
			expected: []string{"smart-connect", "somehost", "-p", "2222"},
		},
		{
			name:     "multiple SSH flags",
			args:     []string{"-p", "2222", "-l", "admin", "somehost"},
			expected: []string{"smart-connect", "somehost", "-p", "2222", "-l", "admin"},
		},
		{
			name:     "boolean SSH flags",
			args:     []string{"-4", "-A", "somehost"},
			expected: []string{"smart-connect", "somehost", "-4", "-A"},
		},
		{
			name:     "mixed global and SSH flags",
			args:     []string{"-v", "-4", "-p", "2222", "somehost", "-l", "root"},
			expected: []string{"-v", "smart-connect", "somehost", "-4", "-p", "2222", "-l", "root"},
		},
		{
			name:     "known subcommand passes through unchanged",
			args:     []string{"inv", "list"},
			expected: []string{"inv", "list"},
		},
		{
			name:     "global flag with subcommand passes through",
			args:     []string{"-v", "inv", "list"},
			expected: []string{"-v", "inv", "list"},
		},
		{
			name:     "hostname only",
			args:     []string{"somehost"},
			expected: []string{"smart-connect", "somehost"},
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
		{
			name:     "global flag only",
			args:     []string{"-v", "--help"},
			expected: []string{"-v", "--help"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preprocessArgs(tt.args)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("preprocessArgs(%v) = %v, want %v", tt.args, result, tt.expected)
			}
		})
	}
}

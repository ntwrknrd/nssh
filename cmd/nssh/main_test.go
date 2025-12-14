package main

import (
	"reflect"
	"testing"
)

func TestPreprocessArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		out  []string
	}{
		{
			name: "simple host routes through smart-connect",
			in:   []string{"router1"},
			out:  []string{"smart-connect", "router1"},
		},
		{
			name: "known subcommand passes through",
			in:   []string{"connect", "router1"},
			out:  []string{"connect", "router1"},
		},
		{
			name: "verbose flag before host",
			in:   []string{"-v", "router1"},
			out:  []string{"-v", "smart-connect", "router1"},
		},
		{
			// SSH flags are moved AFTER hostname to avoid Cobra parsing them at root level
			name: "ssh flag with value before host (-o)",
			in:   []string{"-o", "StrictHostKeyChecking=no", "router1"},
			out:  []string{"smart-connect", "router1", "-o", "StrictHostKeyChecking=no"},
		},
		{
			// SSH flags are moved AFTER hostname to avoid Cobra parsing them at root level
			name: "ssh flag with value before host (-p)",
			in:   []string{"-p", "2200", "router1"},
			out:  []string{"smart-connect", "router1", "-p", "2200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preprocessArgs(tt.in)
			if !reflect.DeepEqual(got, tt.out) {
				t.Fatalf("preprocessArgs(%v) = %v, want %v", tt.in, got, tt.out)
			}
		})
	}
}

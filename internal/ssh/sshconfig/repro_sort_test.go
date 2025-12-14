package sshconfig

import (
	"testing"
)

func TestSortHosts_Wildcard(t *testing.T) {
	hosts := []*HostEntry{
		{Host: "*"},
		{Host: "alpha"},
		{Host: "beta"},
	}

	SortHosts(hosts)

	// Check order. We expect * to be last.
	if hosts[len(hosts)-1].Host != "*" {
		t.Errorf("Expected last host to be *, got %s", hosts[len(hosts)-1].Host)
	}

	// Print actual order for debugging
	for i, h := range hosts {
		t.Logf("[%d] %s", i, h.Host)
	}
}

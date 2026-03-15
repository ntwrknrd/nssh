package providers

import (
	"testing"
)

const testClabJSON = `{
  "containers": [
    {
      "name": "clab-dfz-core01",
      "container_id": "abc123",
      "image": "ceos:latest",
      "kind": "ceos",
      "state": "running",
      "ipv4_address": "172.20.20.2/24",
      "ipv6_address": "2001:db8::2/64",
      "lab_name": "dfz",
      "labPath": "/home/user/labs/dfz.clab.yml",
      "group": "",
      "shortname": "core01",
      "owner": "user"
    },
    {
      "name": "clab-dfz-core02",
      "container_id": "def456",
      "image": "ceos:latest",
      "kind": "ceos",
      "state": "running",
      "ipv4_address": "172.20.20.3/24",
      "ipv6_address": "",
      "lab_name": "dfz",
      "labPath": "/home/user/labs/dfz.clab.yml",
      "group": "",
      "shortname": "core02",
      "owner": "user"
    },
    {
      "name": "clab-dfz-spine01",
      "container_id": "ghi789",
      "image": "vjunos:latest",
      "kind": "vjunos",
      "state": "running",
      "ipv4_address": "172.20.20.4/24",
      "ipv6_address": "",
      "lab_name": "dfz",
      "labPath": "/home/user/labs/dfz.clab.yml",
      "group": "",
      "shortname": "spine01",
      "owner": "user"
    },
    {
      "name": "clab-dfz-noaddr",
      "container_id": "jkl012",
      "image": "ceos:latest",
      "kind": "ceos",
      "state": "stopped",
      "ipv4_address": "N/A",
      "ipv6_address": "",
      "lab_name": "dfz",
      "labPath": "/home/user/labs/dfz.clab.yml",
      "group": "",
      "shortname": "noaddr",
      "owner": "user"
    }
  ]
}`

func TestParseContainerlabJSON(t *testing.T) {
	objects, err := ParseContainerlabJSON([]byte(testClabJSON), "test-lab", "nre-netlab01")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Should skip noaddr (no usable IP)
	if len(objects) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objects))
	}

	// Check first object
	obj := objects[0]
	if obj.Provider != "containerlab" {
		t.Errorf("provider = %q", obj.Provider)
	}
	if obj.Source != "test-lab" {
		t.Errorf("source = %q", obj.Source)
	}
	if obj.ObjectID != "dfz/core01" {
		t.Errorf("object_id = %q", obj.ObjectID)
	}
	if obj.Name != "clab-dfz-core01" {
		t.Errorf("name = %q", obj.Name)
	}
	if obj.HostName != "172.20.20.2" {
		t.Errorf("hostname = %q (should strip CIDR)", obj.HostName)
	}
	if obj.ProxyJump != "nre-netlab01" {
		t.Errorf("proxy_jump = %q", obj.ProxyJump)
	}
	if !obj.UsesPassword {
		t.Error("uses_password should be true")
	}
	if obj.CredentialClass != "ceos" {
		t.Errorf("credential_class = %q", obj.CredentialClass)
	}

	// Check attributes
	if obj.Attributes["kind"][0] != "ceos" {
		t.Errorf("attr kind = %v", obj.Attributes["kind"])
	}
	if obj.Attributes["lab"][0] != "dfz" {
		t.Errorf("attr lab = %v", obj.Attributes["lab"])
	}
	if obj.Attributes["state"][0] != "running" {
		t.Errorf("attr state = %v", obj.Attributes["state"])
	}

	// Check vjunos object
	spine := objects[2]
	if spine.CredentialClass != "vjunos" {
		t.Errorf("spine credential_class = %q", spine.CredentialClass)
	}
}

func TestParseContainerlabJSONEmpty(t *testing.T) {
	objects, err := ParseContainerlabJSON([]byte(`{"containers":[]}`), "empty", "jump")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(objects) != 0 {
		t.Errorf("expected 0 objects, got %d", len(objects))
	}
}

func TestStripCIDR(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"172.20.20.2/24", "172.20.20.2"},
		{"2001:db8::1/64", "2001:db8::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"N/A", ""},
		{"", ""},
		{"  172.20.20.2/24  ", "172.20.20.2"},
	}

	for _, tt := range tests {
		got := stripCIDR(tt.input)
		if got != tt.want {
			t.Errorf("stripCIDR(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

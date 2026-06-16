package self

import (
	"strings"
	"testing"
)

func TestImportSSHConfigMapsApprovedDirectives(t *testing.T) {
	input := `
Host *
  IdentityAgent ~/agent.sock
  ServerAliveInterval 240

Host edge01
  HostName edge01.example.com
  User netops
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  CertificateFile ~/.ssh/id_ed25519-cert.pub
  ProxyJump bastion
  ForwardAgent no
  LocalForward 127.0.0.1:15432 db:5432
`
	out, warnings, err := importSSHConfigText("local", strings.NewReader(input))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	for _, want := range []string{
		"identity_agent:",
		"path: ~/agent.sock",
		"edge01:",
		"group: imported",
		"hostname: edge01.example.com",
		"username: netops",
		"port: 2222",
		"proxy_jump: bastion",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("import output missing %q:\n%s", want, out)
		}
	}
}

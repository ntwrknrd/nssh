//go:build unix

package connector

import (
	"slices"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"gopkg.in/yaml.v3"
)

func TestRenderSSHOptionsIncludesFNoneAndIdentityAgent(t *testing.T) {
	opts := config.SSHHostConfig{
		Options: config.SSHOptions{
			"Compression":    config.NewSSHOptionBool(true),
			"IdentitiesOnly": config.NewSSHOptionBool(true),
			"IdentityAgent":  config.NewSSHOptionString("~/agent.sock"),
			"IdentityFile":   config.NewSSHOptionItems("~/.ssh/id_ed25519.pub"),
		},
	}
	args := RenderSSHOptions(opts, 0)
	for _, want := range []string{"-F", "none", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=~/agent.sock", "-o", "IdentityFile=~/.ssh/id_ed25519.pub", "-o", "Compression=yes"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
}

func TestRenderSSHOptionsQuotesValuesWithSpaces(t *testing.T) {
	opts := config.SSHHostConfig{
		Options: config.SSHOptions{
			"IdentityAgent": config.NewSSHOptionString("/Users/cj/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"),
		},
	}
	args := RenderSSHOptions(opts, 0)
	want := `IdentityAgent="/Users/cj/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"`
	if !slices.Contains(args, want) {
		t.Fatalf("args = %#v, want %q", args, want)
	}
}

func TestRenderSSHOptionsAddsVerbosity(t *testing.T) {
	args := RenderSSHOptions(config.SSHHostConfig{}, 3)
	if !slices.Contains(args, "-vvv") {
		t.Fatalf("args = %#v, want -vvv", args)
	}
}

func TestRenderSSHOptionsUsesTypeAwareOptions(t *testing.T) {
	var cfg struct {
		SSH config.SSHHostConfig `yaml:"ssh"`
	}
	input := `
ssh:
  options:
    PubkeyAuthentication: false
    PreferredAuthentications:
      - keyboard-interactive
      - password
    ServerAliveInterval: 240s
    SetEnv:
      TERM: xterm-256color
      COLORTERM: truecolor
`
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	args := RenderSSHOptions(cfg.SSH, 0)
	for _, want := range []string{
		"PubkeyAuthentication=no",
		"PreferredAuthentications=keyboard-interactive,password",
		"ServerAliveInterval=240",
		"SetEnv=COLORTERM=truecolor TERM=xterm-256color",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
	if slices.Contains(args, `SetEnv="COLORTERM=truecolor TERM=xterm-256color"`) {
		t.Fatalf("args should not quote combined SetEnv map: %#v", args)
	}
	for _, repeated := range []string{"SetEnv=COLORTERM=truecolor", "SetEnv=TERM=xterm-256color"} {
		if slices.Contains(args, repeated) {
			t.Fatalf("args should not render repeated SetEnv %q: %#v", repeated, args)
		}
	}
}

func TestRenderSSHOptionsCompatibilityFloorUsesPolicyList(t *testing.T) {
	args := RenderSSHOptions(config.SSHHostConfig{
		Compatibility: config.SSHCompatibility{
			Kex: "diffie-hellman-group1-sha1",
			MAC: "hmac-sha1",
		},
	}, 0)

	for _, want := range []string{
		"KexAlgorithms=+diffie-hellman-group-exchange-sha256,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group1-sha1",
		"MACs=+hmac-sha2-512,hmac-sha2-256,hmac-sha1",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
}

func TestRenderSSHOptionsCompatibilityFloorExtendsExplicitAlgorithmBaseline(t *testing.T) {
	args := RenderSSHOptions(config.SSHHostConfig{
		Compatibility: config.SSHCompatibility{Kex: "diffie-hellman-group1-sha1"},
		Options: config.SSHOptions{
			"KexAlgorithms": config.NewSSHOptionItems(
				"sntrup761x25519-sha512@openssh.com",
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
			),
		},
	}, 0)

	want := "KexAlgorithms=sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org,diffie-hellman-group-exchange-sha256,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group1-sha1"
	if !slices.Contains(args, want) {
		t.Fatalf("args missing merged KexAlgorithms baseline and compatibility floor: %#v", args)
	}
	for _, arg := range args {
		if arg == "KexAlgorithms=sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org" ||
			arg == "KexAlgorithms=+diffie-hellman-group-exchange-sha256,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group1-sha1" {
			t.Fatalf("args should render one merged KexAlgorithms directive: %#v", args)
		}
	}
}

//go:build unix

package connector

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"gopkg.in/yaml.v3"
)

func TestComposeSSHOptionsOrdersRuntimeBeforeResolvedAndDefaults(t *testing.T) {
	args := ComposeSSHOptions(SSHOptionPlan{
		Enforced: []string{"-o", "StrictHostKeyChecking=yes"},
		Runtime:  []string{"-o", "ConnectTimeout=60", "-F", "/tmp/ignored"},
		Resolved: config.SSHHostConfig{Options: config.SSHOptions{
			"ConnectTimeout":        config.NewSSHOptionString("30"),
			"StrictHostKeyChecking": config.NewSSHOptionString("no"),
		}},
	})

	want := []string{
		"-o", "StrictHostKeyChecking=yes",
		"-F", "none",
		"-o", "ConnectTimeout=60",
		"-o", "ConnectTimeout=30",
		"-o", "StrictHostKeyChecking=no",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("ComposeSSHOptions() = %#v, want %#v", args, want)
	}
	if got := EffectiveSSHOption(args, "ConnectTimeout"); got != "60" {
		t.Fatalf("effective ConnectTimeout = %q, want 60", got)
	}
	if got := EffectiveSSHOption(args, "StrictHostKeyChecking"); got != "yes" {
		t.Fatalf("effective StrictHostKeyChecking = %q, want yes", got)
	}
}

func TestEffectiveSSHOptionUsesFirstValueAndShortAliases(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		want  string
		value string
	}{
		{name: "split equals", args: []string{"-o", "ConnectTimeout=60", "-o", "ConnectTimeout=30"}, want: "ConnectTimeout", value: "60"},
		{name: "split space", args: []string{"-o", "ConnectTimeout 60"}, want: "ConnectTimeout", value: "60"},
		{name: "joined", args: []string{"-oConnectTimeout=60"}, want: "ConnectTimeout", value: "60"},
		{name: "short port", args: []string{"-p", "2222", "-o", "Port=2200"}, want: "Port", value: "2222"},
		{name: "joined short port", args: []string{"-p2222"}, want: "Port", value: "2222"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveSSHOption(tt.args, tt.want); got != tt.value {
				t.Fatalf("EffectiveSSHOption() = %q, want %q", got, tt.value)
			}
		})
	}
}

func TestComposeSSHOptionsPreservesRuntimeFirstRepeatedOptions(t *testing.T) {
	args := ComposeSSHOptions(SSHOptionPlan{
		Runtime: []string{"-i", "/tmp/runtime-key", "-o", "Compression=no"},
		Resolved: config.SSHHostConfig{Options: config.SSHOptions{
			"Compression":  config.NewSSHOptionBool(true),
			"IdentityFile": config.NewSSHOptionItems("/tmp/config-key-1", "/tmp/config-key-2"),
		}},
	})
	want := []string{
		"-F", "none",
		"-i", "/tmp/runtime-key",
		"-o", "Compression=no",
		"-o", "Compression=yes",
		"-o", "IdentityFile=/tmp/config-key-1",
		"-o", "IdentityFile=/tmp/config-key-2",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("ComposeSSHOptions() = %#v, want %#v", args, want)
	}
	if got := EffectiveSSHOption(args, "Compression"); got != "no" {
		t.Fatalf("effective Compression = %q, want no", got)
	}
}

func TestComposeSSHOptionsMatchesOpenSSHEffectivePrecedence(t *testing.T) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh is unavailable")
	}
	args := ComposeSSHOptions(SSHOptionPlan{
		Runtime: []string{"-o", "ConnectTimeout=60"},
		Resolved: config.SSHHostConfig{Options: config.SSHOptions{
			"ConnectTimeout": config.NewSSHOptionString("30"),
		}},
	})
	args = append(args, "-G", "example.invalid")
	out, err := exec.Command(sshPath, args...).Output()
	if err != nil {
		t.Fatalf("ssh -G: %v", err)
	}
	if !strings.Contains(string(out), "connecttimeout 60\n") {
		t.Fatalf("ssh -G did not report runtime ConnectTimeout=60:\n%s", out)
	}
}

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

func TestRenderSSHOptionsDoesNotQuoteWholeProxyCommand(t *testing.T) {
	opts := config.SSHHostConfig{
		Options: config.SSHOptions{
			"ProxyCommand": config.NewSSHOptionString("ssh -F none -W %h:%p jump.example.com"),
		},
	}
	args := RenderSSHOptions(opts, 0)
	want := "ProxyCommand=ssh -F none -W %h:%p jump.example.com"
	if !slices.Contains(args, want) {
		t.Fatalf("args = %#v, want %q", args, want)
	}
	for _, arg := range args {
		if arg == `ProxyCommand="ssh -F none -W %h:%p jump.example.com"` {
			t.Fatalf("ProxyCommand should not quote the whole command: %#v", args)
		}
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

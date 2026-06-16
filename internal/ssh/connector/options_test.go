//go:build unix

package connector

import (
	"slices"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestRenderSSHOptionsIncludesFNoneAndIdentityAgent(t *testing.T) {
	v := true
	opts := config.SSHHostConfig{
		IdentitiesOnly: &v,
		IdentityAgent:  config.IdentityAgent{Path: "~/agent.sock"},
		IdentityFiles:  []string{"~/.ssh/id_ed25519.pub"},
		Options:        map[string]string{"Compression": "yes"},
	}
	args := RenderSSHOptions(opts, 0)
	for _, want := range []string{"-F", "none", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=~/agent.sock", "-i", "~/.ssh/id_ed25519.pub", "-o", "Compression=yes"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
}

func TestRenderSSHOptionsAddsVerbosity(t *testing.T) {
	args := RenderSSHOptions(config.SSHHostConfig{}, 3)
	if !slices.Contains(args, "-vvv") {
		t.Fatalf("args = %#v, want -vvv", args)
	}
}

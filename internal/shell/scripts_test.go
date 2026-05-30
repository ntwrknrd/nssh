package shell

import (
	"strings"
	"testing"
)

func TestNoArgIntegrationUsesHelpFlag(t *testing.T) {
	tests := map[string]string{
		"bash_zsh": BashZshIntegration,
		"fish":     FishIntegration,
	}

	for name, script := range tests {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(script, "--help") {
				t.Fatalf("integration script does not call --help for no-arg invocation")
			}
		})
	}
}

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
			if strings.Contains(script, "nssh_cmd help") ||
				strings.Contains(script, `"$nssh_cmd" help`) ||
				strings.Contains(script, `"${nssh_cmd}" help`) {
				t.Fatalf("integration script routes no-arg invocation through positional help")
			}
		})
	}
}

func TestFallbackSubcommandsDoNotReserveHostnames(t *testing.T) {
	tests := map[string]string{
		"bash_zsh": BashZshIntegration,
		"fish":     FishIntegration,
	}
	reservedHostnames := []string{"help", "version", "completion", "benchmark"}

	for name, script := range tests {
		t.Run(name, func(t *testing.T) {
			line := fallbackSubcommandLine(script)
			if line == "" {
				t.Fatal("fallback subcommand list not found")
			}

			fields := strings.Fields(strings.NewReplacer("(", " ", ")", " ").Replace(line))
			for _, reserved := range reservedHostnames {
				for _, field := range fields {
					if field == reserved {
						t.Fatalf("fallback subcommand list reserves hostname %q: %s", reserved, line)
					}
				}
			}
		})
	}
}

func fallbackSubcommandLine(script string) string {
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "subcommands") &&
			(strings.Contains(line, "set -l") || strings.Contains(line, "local -a")) {
			return line
		}
	}
	return ""
}

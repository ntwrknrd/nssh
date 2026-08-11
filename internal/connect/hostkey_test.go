//go:build unix

package connect

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestRenderNewHostKeyPromptTextIsPlainTerminalOutput(t *testing.T) {
	text := ui.StripANSI(renderNewHostKeyPromptText(connector.HostKeyPrompt{
		Host:        "edge01.example.net",
		KeyType:     "ssh-ed25519",
		Fingerprint: "SHA256:test",
	}))

	for _, want := range []string{
		"This is the first time connecting to this host.",
		"Host: edge01.example.net",
		"Fingerprint: ssh-ed25519 SHA256:test",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "New host key") {
		t.Fatalf("prompt text still contains removed heading:\n%s", text)
	}
	for _, unwanted := range []string{"─", "╭", "│", "╰"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("prompt text contains panel character %q:\n%s", unwanted, text)
		}
	}
}

func TestRenderChangedHostKeyPromptTextIsPlainTerminalOutput(t *testing.T) {
	text := ui.StripANSI(renderChangedHostKeyPromptText(connector.HostKeyPrompt{
		Host:        "edge01.example.net",
		KeyType:     "ssh-ed25519",
		Fingerprint: "SHA256:test",
		Changed:     true,
	}))

	for _, want := range []string{
		"The host key has changed.",
		"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!",
		"Host: edge01.example.net",
		"New fingerprint: ssh-ed25519 SHA256:test",
		"Otherwise, DO NOT CONTINUE.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt text missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"─", "╭", "│", "╰"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("prompt text contains panel character %q:\n%s", unwanted, text)
		}
	}
}

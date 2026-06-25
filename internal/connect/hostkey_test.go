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
		"New host key",
		"Host: edge01.example.net",
		"Fingerprint: ssh-ed25519 SHA256:test",
		"This is the first time connecting to this host.",
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

func TestRenderChangedHostKeyPromptTextIsPlainTerminalOutput(t *testing.T) {
	text := ui.StripANSI(renderChangedHostKeyPromptText(connector.HostKeyPrompt{
		Host:        "edge01.example.net",
		KeyType:     "ssh-ed25519",
		Fingerprint: "SHA256:test",
		Changed:     true,
	}))

	for _, want := range []string{
		"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!",
		"Host: edge01.example.net",
		"New fingerprint: ssh-ed25519 SHA256:test",
		"The host key has changed.",
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

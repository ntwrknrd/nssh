//go:build unix

package connect

import (
	"fmt"
	"strings"

	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func newHostKeyPromptFunc() connector.HostKeyPromptFunc {
	return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
		if prompt.Changed {
			return promptChangedHostKey(prompt)
		}
		return promptNewHostKey(prompt)
	}
}

func promptNewHostKey(prompt connector.HostKeyPrompt) connector.HostKeyAction {
	fmt.Print(renderNewHostKeyPromptText(prompt))

	options := []string{
		"Reject (disconnect)",
		"Accept once (this session only)",
		"Accept always (add to known_hosts)",
	}
	idx, err := ui.SelectIndex("Host key verification", options, prompt.Stdin)
	if err != nil || idx < 0 {
		return connector.HostKeyReject
	}
	switch idx {
	case 0:
		return connector.HostKeyReject
	case 1:
		return connector.HostKeyAcceptOnce
	default:
		return connector.HostKeyAcceptAlways
	}
}

func promptChangedHostKey(prompt connector.HostKeyPrompt) connector.HostKeyAction {
	fmt.Print(renderChangedHostKeyPromptText(prompt))

	options := []string{
		"Reject - possible attack! (recommended)",
		"Accept anyway (dangerous)",
	}
	idx, err := ui.SelectIndex("Host key verification", options, prompt.Stdin)
	if err != nil || idx < 0 {
		return connector.HostKeyReject
	}
	if idx == 0 {
		return connector.HostKeyReject
	}
	return connector.HostKeyAcceptAlways
}

func renderNewHostKeyPromptText(prompt connector.HostKeyPrompt) string {
	var sb strings.Builder
	fp := hostKeyFingerprint(prompt)
	sb.WriteString("\n")
	sb.WriteString(ui.Gray("This is the first time connecting to this host."))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Host: %s\n", ui.Cyan(prompt.Host)))
	sb.WriteString(fmt.Sprintf("Fingerprint: %s\n", ui.Cyan(fp)))
	sb.WriteString("\n\n")
	return sb.String()
}

func renderChangedHostKeyPromptText(prompt connector.HostKeyPrompt) string {
	var sb strings.Builder
	fp := hostKeyFingerprint(prompt)
	sb.WriteString("\n")
	sb.WriteString(ui.Red("The host key has changed."))
	sb.WriteString("\n\n")
	sb.WriteString(ui.Red("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!"))
	sb.WriteString("\n")
	sb.WriteString(ui.Yellow("IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!"))
	sb.WriteString("\n")
	sb.WriteString(ui.Yellow("Someone could be eavesdropping on you right now (man-in-the-middle attack)!"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Host: %s\n", ui.Cyan(prompt.Host)))
	sb.WriteString(fmt.Sprintf("New fingerprint: %s\n", ui.Yellow(fp)))
	sb.WriteString("\n")
	sb.WriteString("If this is expected (server reinstall, key rotation), you may proceed.\n")
	sb.WriteString("Otherwise, DO NOT CONTINUE.\n\n")
	return sb.String()
}

func hostKeyFingerprint(prompt connector.HostKeyPrompt) string {
	if prompt.KeyType != "" && prompt.Fingerprint != "" {
		return fmt.Sprintf("%s %s", prompt.KeyType, prompt.Fingerprint)
	}
	if prompt.Fingerprint != "" {
		return prompt.Fingerprint
	}
	return "(fingerprint not available)"
}

//go:build unix

package connect

import (
	"fmt"

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
	fp := prompt.Fingerprint
	if prompt.KeyType != "" && prompt.Fingerprint != "" {
		fp = fmt.Sprintf("%s %s", prompt.KeyType, prompt.Fingerprint)
	}
	if fp == "" {
		fp = "(fingerprint not available)"
	}

	fmt.Println()
	panel := ui.NewPanel("New Host").
		Row("Host:", prompt.Host).
		Row("Fingerprint:", fp).
		WithFooter("")
	panel.Print()
	fmt.Println()
	fmt.Println(ui.Gray("This is the first time connecting to this host."))
	fmt.Println()

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
	fp := ""
	if prompt.KeyType != "" && prompt.Fingerprint != "" {
		fp = fmt.Sprintf("%s %s", prompt.KeyType, prompt.Fingerprint)
	}

	fmt.Println()
	fmt.Println(ui.Red("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"))
	fmt.Println(ui.Red("@    WARNING: REMOTE HOST IDENTIFICATION HAS    @"))
	fmt.Println(ui.Red("@              CHANGED!                         @"))
	fmt.Println(ui.Red("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"))
	fmt.Println()
	fmt.Println(ui.Yellow("IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!"))
	fmt.Println(ui.Yellow("Someone could be eavesdropping on you right now (man-in-the-middle attack)!"))
	fmt.Println()

	panel := ui.NewPanel("").
		Row("Host:", prompt.Host).
		Row("New fingerprint:", fp).
		WithWarning()
	panel.Print()

	fmt.Println()
	fmt.Println("The host key has changed. If this is expected (server reinstall,")
	fmt.Println("key rotation), you may proceed. Otherwise, DO NOT CONTINUE.")
	fmt.Println()

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

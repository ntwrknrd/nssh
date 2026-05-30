package shell

import (
	_ "embed" // Required for go:embed directives
)

// BashZshIntegration contains the shell integration script for bash and zsh.
// This script provides:
// - Subcommand routing (detect inv, agent, etc. vs hostname)
// - Shell history integration (bash history -s, zsh print -s)
// - Atuin integration if available
// - Arrow output for connection commands (-> nssh hostname)
//
//go:embed scripts/nssh-shell-integration.sh
var BashZshIntegration string

// FishIntegration contains the shell integration script for fish.
// This script provides the same features as BashZshIntegration but
// using fish shell syntax.
//
//go:embed scripts/nssh-shell-integration.fish
var FishIntegration string

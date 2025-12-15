// Package shell provides shell integration and completion support.
//
// This package embeds shell integration scripts that provide enhanced nssh
// functionality when sourced in the user's shell configuration.
//
// # Shell Integration Features
//
// The integration scripts provide:
//   - Subcommand routing: detects nssh subcommands vs hostnames
//   - Shell history: adds successful connections to shell history
//   - Atuin integration: records commands in Atuin if available
//   - Visual feedback: arrow prefix for connection commands (-> nssh hostname)
//
// # Supported Shells
//
// Scripts are provided for:
//   - Bash and Zsh: [BashZshIntegration] (nssh-shell-integration.sh)
//   - Fish: [FishIntegration] (nssh-shell-integration.fish)
//
// # Installation
//
// Shell integration is installed by "nssh self init", which:
//  1. Copies the appropriate script to ~/.local/share/nssh/
//  2. Adds a source line to the shell's rc file (.bashrc, .zshrc, or config.fish)
//
// # Script Location
//
// The scripts are embedded via go:embed from the scripts/ subdirectory.
// At runtime they are written to $XDG_DATA_HOME/nssh/.
package shell

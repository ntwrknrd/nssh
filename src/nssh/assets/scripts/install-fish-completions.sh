#!/usr/bin/env bash
#
# Install fish shell completions for nssh tools
#
# This script installs autocompletion for the unified nssh CLI.
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSET_COMPLETIONS_DIR="$SCRIPT_DIR/../completions"
COMPLETION_DIR="$HOME/.config/fish/completions"

echo "Installing fish completions for nssh tools..."
echo

# Create completion directory if it doesn't exist
mkdir -p "$COMPLETION_DIR"
echo "✓ Completion directory: $COMPLETION_DIR"

# Install all completion files
comp_file="nssh.fish"
if [[ -f "$ASSET_COMPLETIONS_DIR/$comp_file" ]]; then
    cp "$ASSET_COMPLETIONS_DIR/$comp_file" "$COMPLETION_DIR/"
    echo "✓ Installed: $comp_file"
else
    echo "✗ Warning: assets/completions/$comp_file not found"
fi

echo
echo "Fish completions installed successfully!"
echo
echo "To activate completions:"
echo "  1. Restart your fish shell, or"
echo "  2. Run: source ~/.config/fish/config.fish"
echo
echo "Usage examples:"
echo "  nssh <TAB>                         - Complete top-level subcommands"

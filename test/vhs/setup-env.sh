#!/bin/bash
# setup-env.sh - Clean environment before recording demos
# Minimal setup - interactive init handled by VHS tapes
set -e

echo "==> Cleaning existing nssh state..."
rm -rf ~/.config/nssh ~/.local/share/nssh ~/.local/state/nssh ~/.ssh/conf.d 2>/dev/null || true
rm -f ~/.ssh/config 2>/dev/null || true

echo "==> Creating directories..."
mkdir -p ~/.ssh ~/.config ~/.local/share ~/.local/state
chmod 700 ~/.ssh

echo "==> Creating base SSH config..."
cat > ~/.ssh/config << 'EOF'
Host *
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
EOF
chmod 600 ~/.ssh/config

echo "==> Environment cleaned and ready for demo recording!"

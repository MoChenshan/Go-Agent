#!/usr/bin/env bash
# Install trpc-go-cmdline tool.
set -euo pipefail

echo "Installing trpc-go-cmdline..."
go install trpc.tech/trpc-go/trpc-go-cmdline/v2/trpc@latest

echo "Verifying installation..."
if command -v trpc &>/dev/null; then
    echo "trpc installed successfully."
    trpc version
else
    echo "Error: trpc command not found in PATH"
    echo "Please ensure \$GOPATH/bin or \$HOME/go/bin is in your PATH"
    exit 1
fi

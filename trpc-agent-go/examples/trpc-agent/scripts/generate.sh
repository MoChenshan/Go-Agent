#!/usr/bin/env bash
# Generate a tRPC Agent project using trpc agent command.
# All arguments are passed directly to trpc agent.
# Usage: bash scripts/generate.sh [trpc agent options...]
# Example:
#   bash scripts/generate.sh -o my-agent
#   bash scripts/generate.sh -o my-agent --server agui --opsys galileo
#   bash scripts/generate.sh --help
set -euo pipefail

# Show help if no arguments provided.
if [ $# -eq 0 ]; then
    echo "Usage: bash scripts/generate.sh [trpc agent options...]"
    echo ""
    echo "All arguments are passed directly to 'trpc agent'."
    echo ""
    echo "Examples:"
    echo "  bash scripts/generate.sh -o my-agent"
    echo "  bash scripts/generate.sh -o my-agent --server agui"
    echo "  bash scripts/generate.sh -o my-agent --agent graph"
    echo "  bash scripts/generate.sh -o my-agent --opsys galileo"
    echo "  bash scripts/generate.sh --help"
    echo ""
    echo "Run 'trpc help agent' for all available options."
    exit 0
fi

# Pass all arguments to trpc agent.
trpc agent "$@"

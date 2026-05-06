#!/bin/bash

# This script must be sourced so exported variables persist in the caller shell.
if ! (return 0 2>/dev/null); then
    echo "Please source this script: source ./load_env.sh"
    exit 1
fi

# Load .env file
if [ -f .env ]; then
    # Auto-export variables declared while sourcing .env.
    set -a
    # shellcheck disable=SC1091
    source ./.env
    set +a
    echo "Environment variables loaded from .env"
else
    echo "Error: .env file not found"
    exit 1
fi
#!/bin/bash
set -exo pipefail

# Check if npm is installed
if ! [ -x "$(command -v npm)" ]; then
    echo 'Error: npm is not installed.' >&2
    exit 1
else
    # Check if Prettier is installed
    if ! [ -x "$(command -v prettier)" ]; then
        echo 'Error: Prettier is not installed.' >&2
        echo 'Installing Prettier...'
        npm install -g prettier
    fi
fi

# Check if Prettier is installed
if ! [ -x "$(command -v prettier)" ]; then
    echo 'Error: Prettier is not installed.' >&2
    exit 1
fi

# Run Prettier on staged .json, .yaml, and .yml files
echo "Running Prettier on staged files..."

# List all staged files, filter for the desired extensions, and run Prettier
git diff --cached --name-only --diff-filter=d \
                                              | grep -E '\.(json|ya?ml)$' \
                            | xargs -I {} prettier --write {}

# Add the files back to staging area as Prettier may have modified them
git diff --name-only --diff-filter=d \
                                     | grep -E '\.(json|ya?ml)$' \
                            | xargs git add

echo "Prettier formatting completed."

exit 0

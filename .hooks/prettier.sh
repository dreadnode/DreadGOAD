#!/bin/bash
set -exo pipefail

if ! [ -x "$(command -v npm)" ]; then
    echo 'Error: npm is not installed.' >&2
    exit 1
else
    if ! [ -x "$(command -v prettier)" ]; then
        echo 'Error: Prettier is not installed.' >&2
        echo 'Installing Prettier...'
        npm install -g prettier
    fi
fi

if ! [ -x "$(command -v prettier)" ]; then
    echo 'Error: Prettier is not installed.' >&2
    exit 1
fi

echo "Running Prettier on staged files..."

git diff --cached --name-only --diff-filter=d \
                                              | grep -E '\.(json|ya?ml)$' \
                            | xargs -I {} prettier --write {}

git diff --name-only --diff-filter=d \
                                     | grep -E '\.(json|ya?ml)$' \
                            | xargs git add

echo "Prettier formatting completed."

exit 0

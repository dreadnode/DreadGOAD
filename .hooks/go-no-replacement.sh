#!/bin/bash

REPO_ROOT=$(git rev-parse --show-toplevel 2> /dev/null)
GO_MOD="${REPO_ROOT}/cli/go.mod"

if grep -q "^replace " "${GO_MOD}" 2> /dev/null; then
    echo "ERROR: Don't commit a replacement in go.mod!"
    exit 1
fi

#!/bin/bash
set -e

REPO_ROOT=$(git rev-parse --show-toplevel 2> /dev/null)
cd "${REPO_ROOT}/cli"

pkgs=$(go list ./...)

for pkg in $pkgs; do
    dir="$(basename "$pkg")/"
    if [[ "${dir}" != .*/ ]]; then
        go vet "${pkg}"
    fi
done

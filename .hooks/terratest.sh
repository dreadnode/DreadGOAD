#!/bin/bash
set -euo pipefail

RETURN_CODE=0
TIMESTAMP=$(date +"%Y%m%d%H%M%S")
REPO_ROOT=$(git rev-parse --show-toplevel 2> /dev/null) || exit 1

usage() {
    echo "Usage: $0 <module-name>"
    echo ""
    echo "Available modules:"
    echo "  net              - terraform-aws-net"
    echo "  instance-factory - terraform-aws-instance-factory"
    echo "  all              - Run tests for all modules"
    exit 1
}

run_test() {
    local module_dir="$1"
    local module_name="$2"
    local logfile="/tmp/dreadgoad-terratest-${module_name}-${TIMESTAMP}.log"

    pushd "${module_dir}/test" > /dev/null || exit 1
    echo "=== Running terratest for ${module_name} ===" | tee -a "$logfile"
    echo "Logging output to ${logfile}" | tee -a "$logfile"
    echo "Run: tail -f ${logfile}" | tee -a "$logfile"
    echo "Running tests..." | tee -a "$logfile"

    if go test -v -timeout 60m -failfast ./... 2>&1 | tee -a "$logfile"; then
        echo "=== ${module_name} tests PASSED ===" | tee -a "$logfile"
    else
        echo "=== ${module_name} tests FAILED ===" | tee -a "$logfile"
        RETURN_CODE=1
    fi

    popd > /dev/null || exit 1
}

if [ $# -lt 1 ]; then
    usage
fi

MODULE="$1"

case "${MODULE}" in
    net)
        run_test "${REPO_ROOT}/modules/terraform-aws-net" "terraform-aws-net"
        ;;
    instance-factory)
        run_test "${REPO_ROOT}/modules/terraform-aws-instance-factory" "terraform-aws-instance-factory"
        ;;
    all)
        run_test "${REPO_ROOT}/modules/terraform-aws-net" "terraform-aws-net"
        run_test "${REPO_ROOT}/modules/terraform-aws-instance-factory" "terraform-aws-instance-factory"
        ;;
    *)
        echo "Error: Unknown module '${MODULE}'"
        usage
        ;;
esac

if [ $RETURN_CODE -ne 0 ]; then
    echo "Tests failed. Check log files for details."
    exit 1
fi

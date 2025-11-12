#!/bin/bash
# Runs a single Ansible playbook with retry logic and error-specific handling
# Required env vars: PLAYBOOK, ENV, LOG_FILE, MAX_RETRIES, RETRY_DELAY, VERBOSE_FLAG

set -euo pipefail

# Helper functions
check_ansible_success() {
    local log_file=$1

    # Primary check: Look at PLAY RECAP for actual failures
    # This is the most reliable indicator of success/failure
    if grep -A 100 "PLAY RECAP" "$log_file" | grep -E "failed=[1-9][0-9]*|unreachable=[1-9][0-9]*" >/dev/null 2>&1; then
        return 1
    fi

    # Secondary check: Look for fatal errors that weren't ignored
    # Extract context around fatal/FAILED lines and check if they're followed by "...ignoring"
    if grep -E "^fatal:" "$log_file" >/dev/null 2>&1; then
        # Check if ANY fatal error is NOT followed by "...ignoring" within 10 lines
        local fatal_lines=$(grep -n "^fatal:" "$log_file" | cut -d: -f1)
        for line_num in $fatal_lines; do
            # Get 10 lines after the fatal error
            local context=$(sed -n "${line_num},$((line_num + 10))p" "$log_file")
            # If this context doesn't contain "...ignoring", it's a real failure
            if ! echo "$context" | grep -q "...ignoring"; then
                return 1
            fi
        done
    fi

    # Check for retry file creation (indicates Ansible wants us to retry)
    if grep -q "to retry, use:" "$log_file" >/dev/null 2>&1; then
        return 1
    fi

    return 0
}

run_ansible_command() {
    local temp_log=$1
    shift
    "$@" 2>&1 | tee "$temp_log" | tee -a "${LOG_FILE}"
    return "${PIPESTATUS[0]}"
}

log_message() {
    echo "[$(date +%Y-%m-%d\ %H:%M:%S)] $*" | tee -a "${LOG_FILE}"
}

detect_error_type() {
    local temp_log=$1

    if grep -q "FAILED! => .* setup" "$temp_log" || grep -q "Invalid control character" "$temp_log"; then
        echo "fact_gathering"
    elif grep -q "No MSFT_NetAdapter objects found with property 'Name' equal to 'Ethernet3'" "$temp_log"; then
        echo "network_adapter"
    elif grep -E "(403.*Forbidden|failed to transfer.*\.ps1|Invoke-WebRequest.*403.*Forbidden)" "$temp_log" >/dev/null 2>&1; then
        echo "ssm_transfer_error"
    elif grep -qE "(TargetNotConnected|is not connected)" "$temp_log"; then
        echo "ssm_reconnection_needed"
    elif grep -qE "(Timed out waiting for last boot time|timeout waiting for system to reboot)" "$temp_log"; then
        echo "ssm_reconnection_needed"
    elif grep -q "failed to transfer file" "$temp_log"; then
        echo "connection_error"
    elif grep -q "Windows PowerShell is in NonInteractive mode" "$temp_log"; then
        echo "powershell_interactive"
    else
        echo "unknown"
    fi
}

extract_failed_hosts() {
    local temp_log=$1
    grep -E "^[a-zA-Z0-9_-]+\s+:.*failed=[1-9]" "$temp_log" | awk '{print $1}' | tr '\n' ',' | sed 's/,$//'
}

retry_with_error_specific_settings() {
    local playbook=$1
    local temp_log=$2
    local error_type=$3
    local failed_hosts=$4
    
    local limit_args=()
    [[ -n "$failed_hosts" ]] && limit_args=(--limit "$failed_hosts")
    
    case "$error_type" in
        fact_gathering)
            log_message "Retrying with modified fact gathering settings..."
            ANSIBLE_GATHERING=explicit run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" --forks=1 \
                -e "ansible_facts_gathering_timeout=60" \
                -e "gather_timeout=60" \
                "ansible/$playbook"
            ;;
        network_adapter)
            log_message "Retrying with network adapter fix..."
            run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" \
                -e "skip_network_adapter_config=true" \
                -e "bypass_ethernet3_check=true" \
                "ansible/$playbook"
            ;;
        ssm_transfer_error)
            log_message "Retrying with SSM/S3 transfer workarounds..."

            if [[ -n "$failed_hosts" ]]; then
                log_message "Attempting to restart SSM agent on failed hosts..."
                ansible "$failed_hosts" -i "${ENV}-inventory" \
                    -m win_service -a "name=AmazonSSMAgent state=restarted" || true
                sleep 30
            fi

            log_message "Waiting 150 seconds for Windows networking/DNS/S3 access to fully initialize after reboot..."
            sleep 150

            ANSIBLE_TIMEOUT=300 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" --forks=1 \
                -e "ansible_aws_ssm_retries=10" \
                -e "ansible_aws_ssm_retry_delay=30" \
                -e "ansible_connection_timeout=300" \
                -e "ansible_command_timeout=300" \
                -e "ansible_aws_ssm_timeout=300" \
                "ansible/$playbook"
            ;;
        connection_error)
            log_message "Retrying with increased connection timeout..."
            ANSIBLE_TIMEOUT=180 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" \
                -e "ansible_connection_timeout=180" \
                -e "ansible_timeout=180" \
                "ansible/$playbook"
            ;;
        powershell_interactive)
            log_message "Retrying with PowerShell interactive mode fix..."
            run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" \
                -e "ansible_shell_type=powershell" \
                -e "force_ps_module=true" \
                -e "ansible_ps_version=5.1" \
                "ansible/$playbook"
            ;;
        ssm_reconnection_needed)
            log_message "TargetNotConnected detected - waiting for SSM reconnection after reboot..."
            log_message "Waiting 120 seconds for Windows systems to reboot and SSM agent to reconnect..."
            sleep 120

            if [[ -n "$failed_hosts" ]]; then
                log_message "Testing connectivity to failed hosts: $failed_hosts"
                # Test connectivity with a simple ping
                for host in $(echo "$failed_hosts" | tr ',' ' '); do
                    log_message "Testing $host..."
                    if ansible "$host" -i "${ENV}-inventory" -m ansible.windows.win_ping -o 2>&1 | tee -a "${LOG_FILE}" | grep -q "SUCCESS"; then
                        log_message "$host is now reachable"
                    else
                        log_message "$host is still not reachable - will retry anyway"
                    fi
                done
            fi

            log_message "Retrying playbook with increased connection timeout..."
            ANSIBLE_TIMEOUT=180 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" --forks=1 \
                -e "ansible_connection_timeout=180" \
                -e "ansible_timeout=180" \
                -e "ansible_facts_gathering_timeout=60" \
                "ansible/$playbook"
            ;;
        *)
            log_message "Retrying with general robust settings..."
            ANSIBLE_SSH_RETRIES=5 ANSIBLE_TIMEOUT=120 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
                "${limit_args[@]}" --forks=1 \
                "ansible/$playbook"
            ;;
    esac
}

# Main execution
retry_count=0
success=false
temp_log="/tmp/ansible_temp_$(date +%s)_$RANDOM.log"

while [[ $retry_count -lt ${MAX_RETRIES} ]] && [[ "$success" = "false" ]]; do
    if [[ $retry_count -gt 0 ]]; then
        log_message "Retry attempt $retry_count for ansible/${PLAYBOOK}..."
        log_message "Waiting ${RETRY_DELAY} seconds before retrying..."
        sleep "${RETRY_DELAY}"
    fi
    
    log_message "Starting ansible/${PLAYBOOK}..."
    true > "$temp_log"

    ansible_exit_code=0
    run_ansible_command "$temp_log" \
        ansible-playbook ${VERBOSE_FLAG} -i "${ENV}-inventory" \
        -e "ansible_facts_gathering_timeout=60" \
        "ansible/${PLAYBOOK}" || ansible_exit_code=$?
    
    log_message "Ansible exit code: $ansible_exit_code"
    
    if [[ "$ansible_exit_code" -eq 0 ]] && check_ansible_success "$temp_log"; then
        success=true
        log_message "Completed ansible/${PLAYBOOK} successfully."
    else
        log_message "Playbook failed"
        
        error_type=$(detect_error_type "$temp_log")
        log_message "Detected error type: $error_type"
        
        failed_hosts=$(extract_failed_hosts "$temp_log")
        [[ -n "$failed_hosts" ]] && log_message "Failed hosts: $failed_hosts"
        
        log_message "Attempting error-specific recovery for: $error_type"
        
        retry_exit_code=0
        retry_with_error_specific_settings "${PLAYBOOK}" "$temp_log" "$error_type" "$failed_hosts" || retry_exit_code=$?
        
        log_message "Error-specific retry exit code: $retry_exit_code"
        
        if [[ "$retry_exit_code" -eq 0 ]] && check_ansible_success "$temp_log"; then
            success=true
            log_message "Completed ansible/${PLAYBOOK} successfully after error-specific retry."
        else
            retry_count=$((retry_count + 1))
            if [[ $retry_count -eq ${MAX_RETRIES} ]]; then
                log_message "ERROR: ansible/${PLAYBOOK} failed after ${MAX_RETRIES} attempts. Stopping execution."
                {
                    echo "==============================================="
                    echo "SSM Provisioning Script failed at $(date)"
                    echo "==============================================="
                } | tee -a "${LOG_FILE}"
                rm -f "$temp_log"
                exit 1
            fi
        fi
    fi
done

rm -f "$temp_log"
echo "-----------------------------------------------" | tee -a "${LOG_FILE}"

[[ "$success" = "true" ]] && exit 0 || exit 1
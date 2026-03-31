#!/bin/bash
# shellcheck disable=SC2016,SC2153
# Runs a single Ansible playbook with retry logic and error-specific handling
# Required env vars: PLAYBOOK, ENV, LOG_FILE, MAX_RETRIES, RETRY_DELAY, VERBOSE_FLAG
# Optional env vars: LIMIT (to limit execution to specific hosts)

set -euo pipefail

# Build limit args if LIMIT is provided
LIMIT_ARGS=()
[[ -n "${LIMIT:-}" ]] && LIMIT_ARGS=(--limit "${LIMIT}")

# Helper functions
check_ansible_success() {
    local log_file=$1

    # Primary check: Look at PLAY RECAP for actual failures
    # This is the most reliable indicator of success/failure
    if grep -A 100 "PLAY RECAP" "$log_file" | grep -E "failed=[1-9][0-9]*|unreachable=[1-9][0-9]*" > /dev/null 2>&1; then
        return 1
    fi

    # Secondary check: Look for fatal errors that weren't ignored
    # Extract context around fatal/FAILED lines and check if they're followed by "...ignoring"
    if grep -E "^fatal:" "$log_file" > /dev/null 2>&1; then
        # Check if ANY fatal error is NOT followed by "...ignoring" within 10 lines
        local fatal_lines
        fatal_lines=$(grep -n "^fatal:" "$log_file" | cut -d: -f1)
        for line_num in $fatal_lines; do
            # Get 10 lines after the fatal error
            local context
            context=$(sed -n "${line_num},$((line_num + 10))p" "$log_file")
            # If this context doesn't contain "...ignoring", it's a real failure
            if ! echo "$context" | grep -q "...ignoring"; then
                return 1
            fi
        done
    fi

    # Check for retry file creation (indicates Ansible wants us to retry)
    if grep -q "to retry, use:" "$log_file" > /dev/null 2>&1; then
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

# Clean up stale SSM sessions to prevent connection saturation
cleanup_stale_ssm_sessions() {
    log_message "Cleaning up stale SSM sessions..."

    local region
    region=$(grep 'ansible_aws_ssm_region=' "${ENV}-inventory" | head -1 | cut -d= -f2)
    region=${region:-us-west-2}

    # Get all instance IDs from inventory
    local instance_ids
    instance_ids=$(grep -oE 'ansible_host=i-[a-z0-9]+' "${ENV}-inventory" | cut -d= -f2 | sort -u | tr '\n' ' ')

    if [[ -z "$instance_ids" ]]; then
        log_message "No instances found in inventory"
        return 0
    fi

    local terminated=0
    local max_age_minutes=15  # Aggressive cleanup - 15 minutes
    local cutoff_time
    cutoff_time=$(date -u -v-${max_age_minutes}M +%Y-%m-%dT%H:%M:%S 2> /dev/null || date -u -d "${max_age_minutes} minutes ago" +%Y-%m-%dT%H:%M:%S)

    for instance_id in $instance_ids; do
        # Get active sessions for this instance
        local sessions
        sessions=$(aws ssm describe-sessions \
            --state Active \
            --filters "key=Target,value=${instance_id}" \
            --region "$region" \
            --query "Sessions[?StartDate<='${cutoff_time}'].SessionId" \
            --output text 2> /dev/null || echo "")

        for session_id in $sessions; do
            if [[ -n "$session_id" && "$session_id" != "None" ]]; then
                if aws ssm terminate-session --session-id "$session_id" --region "$region" > /dev/null 2>&1; then
                    terminated=$((terminated + 1))
                fi
            fi
        done
    done

    if [[ $terminated -gt 0 ]]; then
        log_message "Terminated $terminated stale SSM session(s)"
        sleep 5  # Brief pause after cleanup
    else
        log_message "No stale sessions found"
    fi
}

# Get instance ID from inventory for a hostname
get_instance_id() {
    local host=$1
    grep "^${host} " "${ENV}-inventory" | grep -oE 'ansible_host=i-[a-z0-9]+' | cut -d= -f2
}

# Quick fix: Re-enable local ssm-user account via SSM Run Command
# This handles the common case where ssm-user exists but got disabled after reboot
enable_ssm_user_local() {
    local host=$1
    local instance_id
    instance_id=$(get_instance_id "$host")

    if [[ -z "$instance_id" ]]; then
        log_message "ERROR: Could not find instance ID for $host"
        return 1
    fi

    local region
    region=$(grep 'ansible_aws_ssm_region=' "${ENV}-inventory" | head -1 | cut -d= -f2)
    region=${region:-us-west-2}

    log_message "Attempting to re-enable ssm-user on $host ($instance_id)..."

    # Simple command to enable local ssm-user
    local cmd_id
    cmd_id=$(aws ssm send-command \
        --instance-ids "$instance_id" \
        --document-name "AWS-RunPowerShellScript" \
        --parameters 'commands=["try { Enable-LocalUser -Name ssm-user -ErrorAction Stop; Write-Output \"ssm-user enabled\" } catch { Write-Output \"Failed: $_\"; exit 1 }"]' \
        --region "$region" \
        --timeout-seconds 60 \
        --query 'Command.CommandId' \
        --output text 2>&1)

    if [[ ! "$cmd_id" =~ ^[a-f0-9-]+$ ]]; then
        log_message "Failed to send enable command: $cmd_id"
        return 1
    fi

    # Wait briefly for completion
    sleep 5
    local status
    status=$(aws ssm get-command-invocation \
        --command-id "$cmd_id" \
        --instance-id "$instance_id" \
        --region "$region" \
        --query 'Status' \
        --output text 2> /dev/null || echo "Failed")

    if [[ "$status" == "Success" ]]; then
        log_message "Successfully re-enabled ssm-user on $host"
        return 0
    else
        log_message "Could not enable local ssm-user on $host (status=$status), will try domain account fix"
        return 1
    fi
}

# Fix SSM user on domain controllers using SSM Run Command (bypasses broken ssm-user)
# This is needed because after DC promotion, the local ssm-user account is destroyed
# and SSM Agent 2.3.612.0+ won't auto-create it on DCs - must create as domain account
fix_ssm_user_via_run_command() {
    local host=$1
    local instance_id
    instance_id=$(get_instance_id "$host")

    if [[ -z "$instance_id" ]]; then
        log_message "ERROR: Could not find instance ID for $host"
        return 1
    fi

    log_message "Fixing ssm-user on $host ($instance_id) via SSM Run Command..."

    # Get region from inventory
    local region
    region=$(grep 'ansible_aws_ssm_region=' "${ENV}-inventory" | head -1 | cut -d= -f2)
    region=${region:-us-west-2}

    # PowerShell script to create ssm-user as domain account
    # Waits for ADWS, creates user if needed, adds to Domain Admins
    local ps_script='
$ErrorActionPreference = "Continue"
$maxWait = 30
$attempt = 0

# Check if this is a domain controller
$cs = Get-WmiObject Win32_ComputerSystem
if ($cs.DomainRole -lt 4) {
    Write-Output "Not a DC (role=$($cs.DomainRole)), skipping domain ssm-user creation"
    exit 0
}

# Wait for ADWS to be running
Write-Output "Waiting for AD Web Services..."
while ($attempt -lt $maxWait) {
    $adws = Get-Service ADWS -ErrorAction SilentlyContinue
    if ($adws.Status -eq "Running") {
        Write-Output "ADWS is running"
        break
    }
    if ($adws.Status -eq "Stopped") {
        Start-Service ADWS -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 10
    $attempt++
}

# Verify AD is accessible
try {
    Get-ADDomain -ErrorAction Stop | Out-Null
    Write-Output "AD is accessible"
} catch {
    Write-Output "ERROR: AD not accessible: $_"
    exit 1
}

# Create or enable ssm-user
try {
    $user = Get-ADUser -Identity ssm-user -ErrorAction Stop
    Write-Output "ssm-user exists, ensuring enabled..."
    Enable-ADAccount -Identity ssm-user
    Set-ADUser -Identity ssm-user -PasswordNeverExpires $true
} catch {
    Write-Output "Creating ssm-user domain account..."
    $pwd = ConvertTo-SecureString "TempP@ss$(Get-Random)!" -AsPlainText -Force
    New-ADUser -Name ssm-user -AccountPassword $pwd -Enabled $true -PasswordNeverExpires $true
}

# Add to Domain Admins
try {
    Add-ADGroupMember -Identity "Domain Admins" -Members ssm-user -ErrorAction SilentlyContinue
    Write-Output "ssm-user added to Domain Admins"
} catch {
    Write-Output "ssm-user already in Domain Admins or error: $_"
}

# Restart SSM Agent
Restart-Service AmazonSSMAgent -Force
Write-Output "SSM Agent restarted - ssm-user fix complete"
'

    # Write script to temp file and build JSON properly
    local tmp_script="/tmp/ssm_fix_script_$$.ps1"
    local tmp_json="/tmp/ssm_fix_params_$$.json"
    echo "$ps_script" > "$tmp_script"

    # Use jq to properly escape the script into JSON
    jq -n --rawfile script "$tmp_script" '{"commands": [$script]}' > "$tmp_json"

    # Send command via SSM Run Command (doesn't need ssm-user to work)
    local cmd_id
    cmd_id=$(aws ssm send-command \
        --instance-ids "$instance_id" \
        --document-name "AWS-RunPowerShellScript" \
        --parameters "file://$tmp_json" \
        --region "$region" \
        --timeout-seconds 600 \
        --query 'Command.CommandId' \
        --output text 2>&1)

    rm -f "$tmp_script" "$tmp_json"

    if [[ ! "$cmd_id" =~ ^[a-f0-9-]+$ ]]; then
        log_message "ERROR: Failed to send SSM command: $cmd_id"
        return 1
    fi

    log_message "SSM command sent: $cmd_id, waiting for completion..."

    # Poll for completion
    local status="InProgress"
    local max_polls=60
    local poll=0
    while [[ "$status" == "InProgress" || "$status" == "Pending" ]] && [[ $poll -lt $max_polls ]]; do
        sleep 5
        status=$(aws ssm get-command-invocation \
            --command-id "$cmd_id" \
            --instance-id "$instance_id" \
            --region "$region" \
            --query 'Status' \
            --output text 2> /dev/null || echo "Pending")
        ((poll++))
    done

    # Get result
    local result
    result=$(aws ssm get-command-invocation \
        --command-id "$cmd_id" \
        --instance-id "$instance_id" \
        --region "$region" \
        --query '[Status,StandardOutputContent,StandardErrorContent]' \
        --output text 2>&1)

    log_message "SSM command result: $result"

    if [[ "$status" == "Success" ]]; then
        log_message "Successfully fixed ssm-user on $host"
        return 0
    else
        log_message "WARNING: SSM command did not succeed (status=$status)"
        return 1
    fi
}

detect_error_type() {
    local temp_log=$1

    if grep -q "FAILED! => .* setup" "$temp_log" || grep -q "Invalid control character" "$temp_log" || grep -q "modules failed to execute: ansible.legacy.setup" "$temp_log" || grep -q "Module result deserialization failed" "$temp_log"; then
        echo "fact_gathering"
    elif grep -q "No MSFT_NetAdapter objects found with property 'Name' equal to 'Ethernet3'" "$temp_log"; then
        echo "network_adapter"
    elif grep -q "failed to transfer file" "$temp_log"; then
        echo "ssm_transfer_error"
    elif grep -qE "(TargetNotConnected|is not connected)" "$temp_log"; then
        echo "ssm_reconnection_needed"
    elif grep -qE "(Timed out waiting for last boot time|timeout waiting for system to reboot)" "$temp_log"; then
        echo "ssm_reconnection_needed"
    elif grep -q "Windows PowerShell is in NonInteractive mode" "$temp_log"; then
        echo "powershell_interactive"
    elif grep -qE "(ssm-user.*disabled|SSM.*account.*issue|Windows Local SAM)" "$temp_log"; then
        echo "ssm_user_account_issue"
    elif grep -qE "rc: 1603|rc: 3010" "$temp_log"; then
        echo "msi_installer_error"
    else
        # Extract actual error for unclassified failures
        local fatal_error
        fatal_error=$(grep -A5 "^fatal:" "$temp_log" | grep -E "msg:|rc:|stderr:" | head -3 | tr '\n' ' ' | sed 's/^[[:space:]]*//')
        if [[ -n "$fatal_error" ]]; then
            echo "unclassified: $fatal_error"
        else
            echo "unclassified: $(grep -E 'FAILED|fatal' "$temp_log" | tail -1 | cut -c1-120)"
        fi
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

    # Build limit args for retry - combine user's LIMIT with failed_hosts
    local limit_args=()
    if [[ -n "${LIMIT:-}" ]] && [[ -n "$failed_hosts" ]]; then
        # If user specified LIMIT and there are failed hosts, use both
        # Ansible will intersect them automatically
        limit_args=(--limit "${LIMIT}" --limit "$failed_hosts")
    elif [[ -n "${LIMIT:-}" ]]; then
        # Only user's LIMIT
        limit_args=(--limit "${LIMIT}")
    elif [[ -n "$failed_hosts" ]]; then
        # Only failed hosts
        limit_args=(--limit "$failed_hosts")
    fi

    case "$error_type" in
        fact_gathering)
            log_message "Retrying with modified fact gathering settings..."
            ANSIBLE_GATHERING=explicit run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} --forks=1 \
                -e "ansible_facts_gathering_timeout=60" \
                -e "gather_timeout=60" \
                "playbooks/$playbook"
            ;;
        network_adapter)
            log_message "Retrying with network adapter fix..."
            run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} \
                -e "skip_network_adapter_config=true" \
                -e "bypass_ethernet3_check=true" \
                "playbooks/$playbook"
            ;;
        ssm_transfer_error)
            log_message "SSM transfer error detected - likely ssm-user account issue on DC..."

            # Clean up stale sessions first
            cleanup_stale_ssm_sessions

            if [[ -n "$failed_hosts" ]]; then
                log_message "Fixing ssm-user via SSM Run Command (bypasses broken Session Manager)..."

                # Use SSM Run Command to fix ssm-user (doesn't require working ssm-user)
                for host in $(echo "$failed_hosts" | tr ',' ' '); do
                    fix_ssm_user_via_run_command "$host" || true
                done

                log_message "Waiting 30 seconds for SSM Agent to stabilize..."
                sleep 30
            fi

            ANSIBLE_TIMEOUT=300 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} --forks=1 \
                -e "ansible_aws_ssm_retries=10" \
                -e "ansible_aws_ssm_retry_delay=30" \
                -e "ansible_connection_timeout=300" \
                -e "ansible_command_timeout=300" \
                -e "ansible_aws_ssm_timeout=300" \
                "playbooks/$playbook"
            ;;
        connection_error)
            log_message "Retrying with increased connection timeout..."
            ANSIBLE_TIMEOUT=180 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} \
                -e "ansible_connection_timeout=180" \
                -e "ansible_timeout=180" \
                "playbooks/$playbook"
            ;;
        powershell_interactive)
            log_message "Retrying with PowerShell interactive mode fix..."
            run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} \
                -e "ansible_shell_type=powershell" \
                -e "force_ps_module=true" \
                -e "ansible_ps_version=5.1" \
                "playbooks/$playbook"
            ;;
        ssm_reconnection_needed)
            log_message "TargetNotConnected detected - waiting for SSM reconnection after reboot..."

            # Clean up stale sessions first
            cleanup_stale_ssm_sessions

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

                # Re-enable ssm-user after reconnection (often disabled after DC promotion/reboot)
                log_message "Re-enabling ssm-user on failed hosts (may be disabled after DC reboot)..."
                for host in $(echo "$failed_hosts" | tr ',' ' '); do
                    enable_ssm_user_local "$host" || fix_ssm_user_via_run_command "$host" || true
                done
                sleep 10
            fi

            log_message "Retrying playbook with increased connection timeout..."
            ANSIBLE_TIMEOUT=180 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} --forks=1 \
                -e "ansible_connection_timeout=180" \
                -e "ansible_timeout=180" \
                -e "ansible_facts_gathering_timeout=60" \
                "playbooks/$playbook"
            ;;
        ssm_user_account_issue)
            log_message "SSM user account issue detected (likely after DC promotion)..."
            log_message "Local ssm-user destroyed when server promoted to DC - creating as domain account"

            if [[ -n "$failed_hosts" ]]; then
                # Use SSM Run Command to fix ssm-user (doesn't require working ssm-user)
                for host in $(echo "$failed_hosts" | tr ',' ' '); do
                    fix_ssm_user_via_run_command "$host" || true
                done

                log_message "Waiting 30 seconds for SSM Agent to stabilize..."
                sleep 30
            fi

            log_message "Retrying playbook with robust SSM settings..."
            ANSIBLE_TIMEOUT=180 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} --forks=1 \
                -e "ansible_connection_timeout=180" \
                -e "ansible_timeout=180" \
                -e "ansible_aws_ssm_timeout=300" \
                "playbooks/$playbook"
            ;;
        msi_installer_error)
            log_message "MSI installer error (rc 1603/3010) - usually requires reboot..."
            if [[ -n "$failed_hosts" ]]; then
                log_message "Rebooting failed hosts before retry: $failed_hosts"
                for host in $(echo "$failed_hosts" | tr ',' ' '); do
                    ansible "$host" -i "${ENV}-inventory" -m ansible.windows.win_reboot \
                        -a "reboot_timeout=600 post_reboot_delay=60" 2>&1 | tee -a "${LOG_FILE}" || true
                done
                log_message "Waiting 30 seconds after reboot..."
                sleep 30
            fi
            run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} --forks=1 \
                "playbooks/$playbook"
            ;;
        unclassified:* | *)
            log_message "Retrying with general robust settings..."
            ANSIBLE_SSH_RETRIES=5 ANSIBLE_TIMEOUT=120 run_ansible_command "$temp_log" \
                ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
                ${limit_args[@]+"${limit_args[@]}"} --forks=1 \
                "playbooks/$playbook"
            ;;
    esac
}

# Main execution
retry_count=0
success=false
temp_log="/tmp/ansible_temp_$(date +%s)_$RANDOM.log"

while [[ $retry_count -lt ${MAX_RETRIES} ]] && [[ "$success" = "false" ]]; do
    if [[ $retry_count -gt 0 ]]; then
        log_message "Retry attempt $retry_count for playbooks/${PLAYBOOK}..."
        log_message "Waiting ${RETRY_DELAY} seconds before retrying..."
        sleep "${RETRY_DELAY}"
    fi

    log_message "Starting playbooks/${PLAYBOOK}..."
    true > "$temp_log"

    ansible_exit_code=0
    # Kill playbook if no output for 20 minutes (not total runtime)
    # Needs to be longer than win_reboot (900s timeout + 120s post_reboot_delay = 1020s)
    # and async_status polling (60 retries × 5s = 300s per host)
    IDLE_TIMEOUT=${IDLE_TIMEOUT:-1200}

    # Kill a process and all its descendants
    kill_tree() {
        local pid=$1
        local sig=${2:-TERM}
        # Kill children first (depth-first)
        for child in $(pgrep -P "$pid" 2> /dev/null); do
            kill_tree "$child" "$sig"
        done
        kill -"$sig" "$pid" 2> /dev/null
    }

    # Run ansible-playbook with a FIFO to detect idle/hung state
    mkfifo /tmp/ansible_pipe_$$ 2> /dev/null || true
    {
        ansible-playbook ${VERBOSE_FLAG:+"${VERBOSE_FLAG}"} -i "${ENV}-inventory" \
            ${LIMIT_ARGS[@]+"${LIMIT_ARGS[@]}"} \
            -e "ansible_facts_gathering_timeout=60" \
            "playbooks/${PLAYBOOK}" 2>&1 | tee "$temp_log" | tee -a "${LOG_FILE}"
        echo $? > /tmp/ansible_exit_$$
    } &

    ansible_pid=$!

    # Clean up on interrupt (Ctrl+C, TERM, etc.)
    trap 'log_message "Interrupted, killing ansible process tree..."; kill_tree $ansible_pid TERM; sleep 1; kill_tree $ansible_pid KILL; exit 130' INT TERM

    # Monitor for idle timeout
    last_output_time=$(date +%s)
    last_size=0
    while kill -0 $ansible_pid 2> /dev/null; do
        sleep 5

        # Get current log file size, default to 0 if file doesn't exist yet
        if [[ -f "$temp_log" ]]; then
            current_size=$(wc -c < "$temp_log" 2> /dev/null || echo 0)
        else
            current_size=0
        fi
        current_time=$(date +%s)

        if [[ "$current_size" -gt "$last_size" ]]; then
            # Output is progressing
            last_output_time=$current_time
            last_size=$current_size
        else
            # No new output
            idle_time=$((current_time - last_output_time))
            if [[ $idle_time -gt $IDLE_TIMEOUT ]]; then
                log_message "ERROR: No output for ${IDLE_TIMEOUT} seconds, killing playbook (PID: $ansible_pid) and children"
                kill_tree $ansible_pid TERM
                sleep 2
                kill_tree $ansible_pid KILL
                ansible_exit_code=124  # Use same exit code as timeout command
                break
            fi
        fi
    done

    wait $ansible_pid 2> /dev/null || ansible_exit_code=$(cat /tmp/ansible_exit_$$ 2> /dev/null || echo 1)
    trap - INT TERM  # Reset trap
    rm -f /tmp/ansible_exit_$$ /tmp/ansible_pipe_$$ 2> /dev/null

    log_message "Ansible exit code: $ansible_exit_code"

    # Check if playbook timed out (timeout command returns 124)
    if [[ "$ansible_exit_code" -eq 124 ]]; then
        log_message "ERROR: Playbook timed out (idle timeout reached)"
        log_message "This usually indicates hung async tasks or SSM connection issues"

        # Clean up stale sessions before retry
        cleanup_stale_ssm_sessions

        retry_count=$((retry_count + 1))
        if [[ $retry_count -lt ${MAX_RETRIES} ]]; then
            log_message "Will retry playbook..."
            continue
        else
            log_message "ERROR: playbooks/${PLAYBOOK} timed out after ${MAX_RETRIES} attempts. Stopping execution."
            rm -f "$temp_log"
            exit 1
        fi
    fi

    if [[ "$ansible_exit_code" -eq 0 ]] && check_ansible_success "$temp_log"; then
        success=true
        log_message "Completed playbooks/${PLAYBOOK} successfully."
    else
        log_message "Playbook failed"

        error_type=$(detect_error_type "$temp_log")
        log_message "Detected error type: $error_type"

        failed_hosts=$(extract_failed_hosts "$temp_log")

        if [[ -n "$failed_hosts" ]]; then
            log_message "Attempting error-specific recovery for $failed_hosts: $error_type"
        else
            log_message "Attempting error-specific recovery: $error_type"
        fi

        retry_exit_code=0
        retry_with_error_specific_settings "${PLAYBOOK}" "$temp_log" "$error_type" "$failed_hosts" || retry_exit_code=$?

        log_message "Error-specific retry exit code: $retry_exit_code"

        if [[ "$retry_exit_code" -eq 0 ]] && check_ansible_success "$temp_log"; then
            success=true
            log_message "Completed playbooks/${PLAYBOOK} successfully after error-specific retry."
        else
            retry_count=$((retry_count + 1))
            if [[ $retry_count -eq ${MAX_RETRIES} ]]; then
                log_message "ERROR: playbooks/${PLAYBOOK} failed after ${MAX_RETRIES} attempts. Stopping execution."
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

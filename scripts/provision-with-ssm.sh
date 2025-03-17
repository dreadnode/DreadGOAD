#!/bin/bash
# Robust Ansible Provisioning Script with comprehensive error handling
# Handles various types of Ansible failures including fact gathering, network adapter and PowerShell issues

# Disable the script from exiting on error
set +e

ENV=${ENV:-staging}
# Set to "true" to enable verbose output for ansible-playbook, otherwise "false"
VERBOSE=${VERBOSE:-false}
# Maximum number of retry attempts for each playbook
MAX_RETRIES=${MAX_RETRIES:-3}
# Delay between retry attempts in seconds
RETRY_DELAY=${RETRY_DELAY:-30}

# Determine the ansible verbose flag
if [ "$VERBOSE" = "true" ]; then
  VERBOSE_FLAG="-vvv"
else
  VERBOSE_FLAG=""
fi

# Set up logging
LOG_FILE="ad_deployment_$(date +%Y%m%d_%H%M%S).log"
TEMP_LOG="/tmp/ansible_temp_$$.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "==============================================="
echo "AD Deployment Script started at $(date)"
echo "Environment: $ENV"
echo "Max Retries: $MAX_RETRIES"
echo "==============================================="

# Define playbooks for the default profile from playbooks.yml
PLAYBOOKS=(
  "build.yml" # ALL CHANGES VETTED
  "ad-servers.yml" # ALL CHANGES VETTED
  "ad-parent_domain.yml" 
  "ad-child_domain.yml" 
  "ad-members.yml" 
  "ad-trusts.yml" 
  "ad-data.yml" 
  "ad-gmsa.yml" 
  "laps.yml" 
  "ad-relations.yml" 
  "adcs.yml" 
  "ad-acl.yml" 
  # <------ known to be all good and working up until this point
  "servers.yml" 
  # <------ not super well tested, but appears to work
  "security.yml" 
  # <------ worked first try - super fucking sus
  "vulnerabilities.yml"
)

echo "Playbooks to be executed:"
for playbook in "${PLAYBOOKS[@]}"; do
  echo "  - ansible/$playbook"
done
echo "-----------------------------------------------"

# Function to run a playbook with comprehensive retry logic
run_playbook_with_retry() {
  local playbook=$1
  local retry_count=0
  local success=false
  local error_type=""
  
  while [ $retry_count -lt $MAX_RETRIES ] && [ "$success" = "false" ]; do
    if [ $retry_count -gt 0 ]; then
      echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Retry attempt $retry_count for ansible/$playbook..."
      echo "Waiting $RETRY_DELAY seconds before retrying..."
      sleep $RETRY_DELAY
    fi
    
    echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Starting ansible/$playbook..."
    
    # Clear temporary log for this run
    true > "$TEMP_LOG"
    
    # For all playbooks, use the inventory file and include the optional verbose flag
    # Run with standard configuration first
    set -o pipefail
    ANSIBLE_HOST_KEY_CHECKING=False ANSIBLE_RETRY_FILES_ENABLED=True \
    ANSIBLE_GATHER_TIMEOUT=60 \
    ansible-playbook $VERBOSE_FLAG -i $ENV-inventory \
    -e "ansible_facts_gathering_timeout=60" \
    "ansible/$playbook" 2>&1 | tee -a "$TEMP_LOG"
    result=$?
    set +o pipefail
    
    # Check if the playbook ran successfully
    if [ $result -eq 0 ]; then
      success=true
      echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Completed ansible/$playbook successfully."
    else
      # Analyze error type to determine specific retry strategy
      if grep -q "FAILED! => .* setup" "$TEMP_LOG" || grep -q "Invalid control character" "$TEMP_LOG"; then
        error_type="fact_gathering"
        echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Detected fact gathering issues."
      elif grep -q "No MSFT_NetAdapter objects found with property 'Name' equal to 'Ethernet3'" "$TEMP_LOG"; then
        error_type="network_adapter"
        echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Detected network adapter configuration issues."
      elif grep -q "failed to transfer file" "$TEMP_LOG" || grep -q "403" "$TEMP_LOG"; then
        error_type="connection_error"
        echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Detected connection/transfer errors."
      elif grep -q "Windows PowerShell is in NonInteractive mode" "$TEMP_LOG"; then
        error_type="powershell_interactive"
        echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Detected PowerShell interactive mode error."
      else
        error_type="unknown"
        echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Unknown error occurred."
      fi
      
      # Apply specific retry strategy based on error type
      set -o pipefail
      case "$error_type" in
        fact_gathering)
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Retrying with modified fact gathering settings..."
          ANSIBLE_GATHERING=explicit ANSIBLE_HOST_KEY_CHECKING=False \
          ansible-playbook $VERBOSE_FLAG -i $ENV-inventory \
          --forks=1 \
          -e "ansible_facts_gathering_timeout=60" \
          -e "gather_timeout=60" \
          "ansible/$playbook" 2>&1 | tee -a "$TEMP_LOG"
          ;;
        network_adapter)
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Retrying with network adapter fix..."
          ANSIBLE_HOST_KEY_CHECKING=False \
          ansible-playbook $VERBOSE_FLAG -i $ENV-inventory \
          -e "skip_network_adapter_config=true" \
          -e "bypass_ethernet3_check=true" \
          "ansible/$playbook" 2>&1 | tee -a "$TEMP_LOG"
          ;;
        connection_error)
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Retrying with increased connection timeout..."
          ANSIBLE_HOST_KEY_CHECKING=False ANSIBLE_TIMEOUT=180 \
          ansible-playbook $VERBOSE_FLAG -i $ENV-inventory \
          -e "ansible_connection_timeout=180" \
          -e "ansible_timeout=180" \
          "ansible/$playbook" 2>&1 | tee -a "$TEMP_LOG"
          ;;
        powershell_interactive)
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Retrying with PowerShell interactive mode fix..."
          ANSIBLE_HOST_KEY_CHECKING=False \
          ansible-playbook $VERBOSE_FLAG -i $ENV-inventory \
          -e "ansible_shell_type=powershell" \
          -e "force_ps_module=true" \
          -e "ansible_ps_version=5.1" \
          "ansible/$playbook" 2>&1 | tee -a "$TEMP_LOG"
          ;;
        *)
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Retrying with general robust settings..."
          ANSIBLE_HOST_KEY_CHECKING=False ANSIBLE_RETRY_FILES_ENABLED=True \
          ANSIBLE_SSH_RETRIES=5 ANSIBLE_TIMEOUT=120 \
          ansible-playbook $VERBOSE_FLAG -i $ENV-inventory \
          --forks=1 \
          "ansible/$playbook" 2>&1 | tee -a "$TEMP_LOG"
          ;;
      esac
      retry_result=$?
      set +o pipefail
      
      # Check if the specific retry was successful
      if [ $retry_result -eq 0 ]; then
        success=true
        echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Completed ansible/$playbook successfully after error-specific retry."
      else
        retry_count=$((retry_count + 1))
        if [ $retry_count -lt $MAX_RETRIES ]; then
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] WARNING: ansible/$playbook failed with exit code $retry_result. Retrying ($retry_count/$MAX_RETRIES)..."
        else
          echo "[$(date +%Y-%m-%d\ %H:%M:%S)] ERROR: ansible/$playbook failed with exit code $retry_result after $MAX_RETRIES attempts. Stopping execution."
          echo "==============================================="
          echo "SSM Provisioning Script failed at $(date)"
          echo "==============================================="
          return 1
        fi
      fi
    fi
  done
  
  if [ "$success" = "true" ]; then
    return 0
  else
    return 1
  fi
}

# Run each playbook with retry logic
for playbook in "${PLAYBOOKS[@]}"; do
  if ! run_playbook_with_retry "$playbook"; then
    echo "Playbook execution failed after all retry attempts. Exiting."
    exit 1
  fi
  
  echo "-----------------------------------------------"
done

echo "==============================================="
echo "All playbooks completed successfully at $(date)"
echo "Full log available at: $LOG_FILE"
echo "==============================================="

# Clean up temporary log
rm -f "$TEMP_LOG"
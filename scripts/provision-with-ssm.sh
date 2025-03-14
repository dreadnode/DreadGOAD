#!/bin/bash
set -e

ENV=staging
# Set to "true" to enable verbose output for ansible-playbook, otherwise "false"
VERBOSE=false

# Determine the ansible verbose flag
if [ "$VERBOSE" = "true" ]; then
  VERBOSE_FLAG="-vvv"
else
  VERBOSE_FLAG=""
fi

# Set up logging
LOG_FILE="ad_deployment_$(date +%Y%m%d_%H%M%S).log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "==============================================="
echo "AD Deployment Script started at $(date)"
echo "==============================================="

# Define playbooks for the default profile from playbooks.yml
PLAYBOOKS=(
  "build.yml" # ALL CHANGES VETTED
  "ad-servers.yml" # ALL CHANGES VETTED
  "ad-parent_domain.yml" 
  "ad-child_domain.yml" 
  "ad-members.yml" 
  # "ad-trusts.yml" 
  # "ad-data.yml" 
  # "ad-gmsa.yml" 
  # "laps.yml" 
  # "ad-relations.yml" 
  # "adcs.yml" 
  # "ad-acl.yml" 
  # <------ known to be all good and working up until this point
  # "servers.yml" 
  # <------ not super well tested, but appears to work
  # "security.yml" 
  # <------ worked first try - super fucking sus
  # "vulnerabilities.yml"
)

echo "Playbooks to be executed:"
for playbook in "${PLAYBOOKS[@]}"; do
  echo "  - ansible/$playbook"
done
echo "-----------------------------------------------"

# Run each playbook
for playbook in "${PLAYBOOKS[@]}"; do
  echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Starting ansible/$playbook..."
  
  # For all playbooks, use the inventory file and include the optional verbose flag
  ANSIBLE_HOST_KEY_CHECKING=False ansible-playbook $VERBOSE_FLAG -i $ENV-inventory ansible/$playbook
  
  # Check if the playbook ran successfully
  result=$?
  if [ $result -ne 0 ]; then
    echo "[$(date +%Y-%m-%d\ %H:%M:%S)] ERROR: ansible/$playbook failed with exit code $result. Stopping execution."
    echo "==============================================="
    echo "AD Deployment Script failed at $(date)"
    echo "==============================================="
    exit 1
  fi
  
  if [ "$playbook" != "wait5m.yml" ]; then
    echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Completed ansible/$playbook successfully."
  else
    echo "[$(date +%Y-%m-%d\ %H:%M:%S)] Completed waiting period."
  fi
  echo "-----------------------------------------------"
done

echo "==============================================="
echo "All playbooks completed successfully at $(date)"
echo "Full log available at: $LOG_FILE"
echo "==============================================="
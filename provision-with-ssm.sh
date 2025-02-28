#!/bin/bash

# Define playbooks for the default profile from playbooks.yml
PLAYBOOKS=(
  "build.yml"
  # "ad-servers.yml"
  # "ad-parent_domain.yml"
  # "ad-child_domain.yml"
  # "wait5m.yml"
  # "ad-members.yml"
  # "ad-trusts.yml"
  # "ad-data.yml"
  # "ad-gmsa.yml"
  # "laps.yml"
  # "ad-relations.yml"
  # "adcs.yml"
  # "ad-acl.yml"
  # "servers.yml"
  # "security.yml"
  # "vulnerabilities.yml"
)

# Run each playbook
for playbook in "${PLAYBOOKS[@]}"; do
  echo "Running ansible/$playbook..."
  
  if [ "$playbook" = "wait5m.yml" ]; then
    # For wait5m.yml, don't use the inventory file since it runs on localhost
    ANSIBLE_HOST_KEY_CHECKING=False ansible-playbook ansible/$playbook
  else
    # For all other playbooks, use the inventory file
    ANSIBLE_HOST_KEY_CHECKING=False ansible-playbook -i inventory \
      ansible/$playbook -e "data_path=~/dreadnode/DreadGOAD/ad/GOAD/data"
  fi
  
  # Check if the playbook ran successfully
  result=$?
  if [ $result -ne 0 ]; then
    echo "Error running ansible/$playbook. Stopping execution."
    exit 1
  fi
  
  if [ "$playbook" != "wait5m.yml" ]; then
    echo "Completed ansible/$playbook successfully."
  else
    echo "Completed waiting period."
  fi
done

echo "All playbooks completed successfully."
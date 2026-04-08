#!/bin/bash
# shellcheck disable=SC2001,SC2034
# Generate AWS instance ID to IP mapping for Ansible optimization
# This speeds up playbook execution by avoiding network detection on each run
set -euo pipefail

INVENTORY="${1:-dev-inventory}"
# Extract environment name from inventory (e.g., dev-inventory -> dev)
ENV_NAME=$(basename "${INVENTORY}" | sed 's/-inventory$//')
OUTPUT_FILE="${2:-/tmp/aws_instance_mapping_${ENV_NAME}.json}"

# Extract AWS region from inventory
AWS_REGION=$(grep -E "ansible_aws_ssm_region=" "${INVENTORY}" | head -1 | cut -d= -f2)

if [ -z "$AWS_REGION" ]; then
    echo "Error: Could not find ansible_aws_ssm_region in inventory"
    echo "Please ensure your inventory has: ansible_aws_ssm_region=us-west-2 (or your region)"
    exit 1
fi

echo "Generating instance-to-IP mapping from inventory: ${INVENTORY}"
echo "AWS Region: ${AWS_REGION}"

# Extract instance IDs from inventory
instance_ids=$(grep -E "ansible_host=i-[a-f0-9]+" "${INVENTORY}" \
                                                                 | grep -oE "i-[a-f0-9]+" | sort -u)

if [ -z "$instance_ids" ]; then
    echo "Error: No instance IDs found in inventory"
    exit 1
fi

echo "Found instances:"
echo "$instance_ids" | sed 's/^/  - /'

# Query AWS for instance private IPs
echo ""
echo "Querying AWS EC2 for private IP addresses..."

# Build JMESPath query for multiple instances
instance_list=$(echo "$instance_ids" | tr '\n' ',' | sed 's/,$//')

# Query AWS EC2 and build JSON mapping
# instance_ids needs word splitting for multiple IDs
# shellcheck disable=SC2086
mapping=$(aws ec2 describe-instances \
    --region "${AWS_REGION}" \
    --instance-ids $instance_ids \
    --query 'Reservations[].Instances[].[InstanceId, PrivateIpAddress]' \
    --output json \
                  | jq -r 'map({key: .[0], value: .[1]}) | from_entries | {instance_to_ip: .}')

if [ -z "$mapping" ] || [ "$mapping" = "null" ]; then
    echo "Error: Failed to retrieve instance information from AWS"
    exit 1
fi

# Write to output file
echo "$mapping" > "${OUTPUT_FILE}"

echo ""
echo "✓ Mapping generated successfully: ${OUTPUT_FILE}"
echo ""
echo "Contents:"
jq '.' "${OUTPUT_FILE}"

# Show summary
instance_count=$(echo "$mapping" | jq '.instance_to_ip | length')
echo ""
echo "Summary: Mapped ${instance_count} instances"

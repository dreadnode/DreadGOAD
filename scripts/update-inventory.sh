#!/bin/bash
# Synchronize Ansible inventory with AWS instance IDs
# Required env vars: ENV, INVENTORY, OUTPUT, BACKUP, BACKUP_FILE, JSON

set -euo pipefail

# Validate inventory file exists
if [ ! -f "${INVENTORY}" ]; then
    echo "Error: Inventory file not found: ${INVENTORY}"
    exit 1
fi

# Create backup if requested
if [ "${BACKUP}" = "true" ]; then
    cp "${INVENTORY}" "${BACKUP_FILE}"
    echo "Created backup: ${BACKUP_FILE}"
fi

# Set output file to inventory file if not specified
OUTPUT_FILE="${OUTPUT:-${INVENTORY}}"

# Update the env= field in the inventory file to match the ENV parameter
echo "Updating environment in inventory to: ${ENV}"
sed -i.tmp -E "s/^(\s*env=).*/\1${ENV}/" "${INVENTORY}"
rm -f "${INVENTORY}.tmp"

# Function to update inventory file
update_inventory() {
    local instances_data="$1"
    local inventory_file="$2"
    local output_file="$3"
    local temp_file
    temp_file=$(mktemp)
    local updates_count=0

    # Parse JSON and extract instance information
    if command -v jq &> /dev/null; then
        instance_pairs=$(echo "$instances_data" | jq -r '.[] |
            if type=="array" then .[0] else . end |
            select(.Name != null and .InstanceId != null) |
            select(.Name | contains("dreadgoad-")) |
            (.Name | split("dreadgoad-") | .[1] | ascii_downcase) + " " + .InstanceId')
    fi

    if [ -z "$instance_pairs" ]; then
        echo "No matching instances found in the AWS data"
        rm -f "$temp_file"
        return 1
    fi

    echo "0" > "$temp_file"

    while read -r server_name instance_id; do
        if [ -z "$server_name" ] || [ -z "$instance_id" ]; then
            continue
        fi

        server_line=$(grep -i "^${server_name}[[:space:]]" "$inventory_file" || true)

        if [ -n "$server_line" ]; then
            # Extract current instance ID (assumes ansible_host= format)
            current_id=$(echo "$server_line" | sed -E "s/^${server_name}[[:space:]]+ansible_host=([^[:space:]]+)(.*)/\1/")

            if [ "$current_id" != "$instance_id" ]; then
                sed -i.tmp -E "s/^(${server_name}[[:space:]]+ansible_host=)[^[:space:]]+(.*)$/\1${instance_id}\2/i" "$inventory_file"
                rm -f "${inventory_file}.tmp"
                echo "Updated ${server_name} with instance ID: ${instance_id}"

                current_count=$(cat "$temp_file")
                echo $((current_count + 1)) > "$temp_file"
            fi
        fi
    done <<< "$instance_pairs"

    updates_count=$(cat "$temp_file")

    if [ "$updates_count" -eq 0 ]; then
        echo "No instance ID updates were made. All instance IDs are already up to date."
    else
        echo "Successfully updated ${updates_count} instance IDs in ${inventory_file}"
    fi

    if [ "$output_file" != "$inventory_file" ]; then
        cp "$inventory_file" "$output_file"
        echo "Copied updated inventory to: ${output_file}"
    fi

    rm -f "$temp_file"
    return 0
}

if [ -n "${JSON}" ]; then
    if [ ! -f "${JSON}" ]; then
        echo "Error: JSON file not found: ${JSON}"
        exit 1
    fi
    instances_data=$(cat "${JSON}")
elif ! [ -t 0 ]; then
    # Use timeout to avoid blocking indefinitely on empty stdin
    instances_data=$(timeout 1 cat 2> /dev/null || true)
    if [ -z "$instances_data" ]; then
        echo "No piped input received. Fetching instance data from AWS..."
        if ! instances_data=$(aws ec2 describe-instances \
            --query "Reservations[].Instances[].{InstanceId:InstanceId, VPC:VpcId, Subnet:SubnetId, PublicIP:PublicIpAddress, PrivateIP:PrivateIpAddress, Name:Tags[?Key=='Name']|[0].Value}" \
            --filters Name=instance-state-name,Values=running \
            --region "${AWS_REGION:-us-west-2}" \
            --output json 2>&1); then
            echo "Failed to fetch instance data from AWS: $instances_data"
            exit 1
        fi
    fi
else
    echo "Fetching instance data from AWS..."
    if ! instances_data=$(aws ec2 describe-instances \
        --query "Reservations[].Instances[].{InstanceId:InstanceId, VPC:VpcId, Subnet:SubnetId, PublicIP:PublicIpAddress, PrivateIP:PrivateIpAddress, Name:Tags[?Key=='Name']|[0].Value}" \
        --filters Name=instance-state-name,Values=running \
        --region "${AWS_REGION:-us-west-2}" \
        --output json 2>&1); then
        echo "Failed to fetch instance data from AWS: $instances_data"
        exit 1
    fi
fi

if [ -z "$instances_data" ]; then
    echo "Error: No AWS instance data provided"
    exit 1
fi

update_inventory "$instances_data" "${INVENTORY}" "$OUTPUT_FILE"
exit $?

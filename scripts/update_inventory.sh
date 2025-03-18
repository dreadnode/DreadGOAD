#!/bin/bash

ENV=${ENV:-dev}

# Function to show usage information
function show_usage {
  echo "Usage: update_inventory.sh [options]"
  echo "Update AWS instance IDs in ${ENV} inventory file"
  echo ""
  echo "Options:"
  echo "  -i, --inventory FILE    Path to inventory file (default: ./${ENV}-inventory)"
  echo "  -o, --output FILE       Output file path (default: overwrite inventory file)"
  echo "  -b, --backup            Create a backup of inventory file before modifying"
  echo "  -j, --json FILE         JSON file containing AWS instance data"
  echo "  -h, --help              Show this help message and exit"
  echo ""
  echo "Example: list_running_instances | update_inventory.sh -i ./${ENV}-inventory -b"
}

# Default values
INVENTORY_FILE="./${ENV}-inventory"
OUTPUT_FILE=""
CREATE_BACKUP=false
JSON_FILE=""

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -i|--inventory)
      INVENTORY_FILE="$2"
      shift 2
      ;;
    -o|--output)
      OUTPUT_FILE="$2"
      shift 2
      ;;
    -b|--backup)
      CREATE_BACKUP=true
      shift
      ;;
    -j|--json)
      JSON_FILE="$2"
      shift 2
      ;;
    -h|--help)
      show_usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      show_usage
      exit 1
      ;;
  esac
done

# Check if inventory file exists
if [ ! -f "$INVENTORY_FILE" ]; then
  echo "Error: Inventory file not found: $INVENTORY_FILE"
  exit 1
fi

# Create backup if requested
if [ "$CREATE_BACKUP" = true ]; then
  BACKUP_FILE="${INVENTORY_FILE}.bak.$(date +%Y%m%d%H%M%S)"
  cp "$INVENTORY_FILE" "$BACKUP_FILE"
  echo "Created backup: $BACKUP_FILE"
fi

# Set output file to inventory file if not specified
OUTPUT_FILE="${OUTPUT_FILE:-$INVENTORY_FILE}"

# Function to update inventory file
function update_inventory {
  local instances_data="$1"
  local inventory_file="$2"
  local output_file="$3"
  local temp_file
  temp_file=$(mktemp)
  local updates_count=0
  
  # Parse JSON and extract instance information
  # Use jq if available, otherwise try with grep and awk
  if command -v jq &> /dev/null; then
    # Extract instance name and ID pairs with jq
    instance_pairs=$(echo "$instances_data" | jq -r '.[] | 
      if type=="array" then .[0] else . end | 
      select(.Name != null and .InstanceId != null) | 
      select(.Name | contains("dreadgoad-")) | 
      (.Name | split("dreadgoad-") | .[1] | ascii_downcase) + " " + .InstanceId')
  else
    echo "Warning: jq not found, using limited fallback parser"
    # Crude parsing with grep/sed/awk - less reliable
    instance_pairs=$(echo "$instances_data" | grep -o '"Name":\s*"[^"]*dreadgoad-[^"]*"' | 
      sed 's/"Name":\s*"[^"]*dreadgoad-\([^"]*\)"/\1/' | 
      tr '[:upper:]' '[:lower:]' | 
      while read -r name; do
        id=$(echo "$instances_data" | grep -o -B5 -A5 "dreadgoad-${name}" | 
             grep -o '"InstanceId":\s*"[^"]*"' | 
             sed 's/"InstanceId":\s*"\([^"]*\)"/\1/' | head -1)
        if [ -n "$id" ]; then
          echo "${name} ${id}"
        fi
      done)
  fi
  
  if [ -z "$instance_pairs" ]; then
    echo "No matching instances found in the AWS data"
    rm -f "$temp_file"
    return 1
  fi
  
  # Create a temporary file to track updates
  echo "0" > "$temp_file"
  
  # Process each instance pair (without pipe to avoid subshell)
  while read -r server_name instance_id; do
    if [ -z "$server_name" ] || [ -z "$instance_id" ]; then
      continue
    fi
    
    # Case insensitive search for server name at the start of the line
    server_line=$(grep -i "^${server_name}[[:space:]]" "$inventory_file")
    
    if [ -n "$server_line" ]; then
      # Extract current instance ID (assumes ansible_host= format)
      current_id=$(echo "$server_line" | sed -E "s/^${server_name}[[:space:]]+ansible_host=([^[:space:]]+)(.*)/\1/")
      
      if [ "$current_id" != "$instance_id" ]; then
        # Update the instance ID
        sed -i.tmp -E "s/^(${server_name}[[:space:]]+ansible_host=)[^[:space:]]+(.*)$/\1${instance_id}\2/i" "$inventory_file"
        rm -f "${inventory_file}.tmp"
        echo "Updated ${server_name} with instance ID: ${instance_id}"
        
        # Increment counter (avoid subshell issues)
        current_count=$(cat "$temp_file")
        echo $((current_count + 1)) > "$temp_file"
      fi
    fi
  done <<< "$instance_pairs"
  
  # Get final update count
  updates_count=$(cat "$temp_file")
  
  if [ "$updates_count" -eq 0 ]; then
    echo "No updates were made. Check server names in AWS output match inventory."
    rm -f "$temp_file"
    return 1
  fi
  
  # If output file is different from inventory, copy the updated file
  if [ "$output_file" != "$inventory_file" ]; then
    cp "$inventory_file" "$output_file"
  fi
  
  echo "Successfully updated ${updates_count} instance IDs in ${output_file}"
  rm -f "$temp_file"
  return 0
}

# Get instance data from JSON file or stdin
if [ -n "$JSON_FILE" ]; then
  if [ ! -f "$JSON_FILE" ]; then
    echo "Error: JSON file not found: ${JSON_FILE}"
    exit 1
  fi
  instances_data=$(cat "$JSON_FILE")
elif [ -t 0 ]; then
  # No piped input, try to run list_running_instances
  if command -v list_running_instances &> /dev/null; then
    echo "No piped input detected. Running list_running_instances..."
    instances_data=$(list_running_instances)
  else
    echo "Error: No piped input and list_running_instances function not found"
    echo "Please pipe AWS instance data to this script or ensure list_running_instances is available"
    exit 1
  fi
else
  # Read from stdin (piped input)
  instances_data=$(cat)
fi

# Check if we have instance data
if [ -z "$instances_data" ]; then
  echo "Error: No AWS instance data provided"
  exit 1
fi

# Update inventory
update_inventory "$instances_data" "$INVENTORY_FILE" "$OUTPUT_FILE"
exit $?
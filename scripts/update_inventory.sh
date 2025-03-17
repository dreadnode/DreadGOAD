#!/bin/bash

# Function to show usage information
function show_usage {
  echo "Usage: update_inventory.sh [options]"
  echo "Update AWS instance IDs in staging inventory file"
  echo ""
  echo "Options:"
  echo "  -i, --inventory FILE    Path to inventory file (default: ./staging-inventory)"
  echo "  -o, --output FILE       Output file path (default: overwrite inventory file)"
  echo "  -b, --backup            Create a backup of inventory file before modifying"
  echo "  -h, --help              Show this help message and exit"
  echo ""
  echo "Example: list_running_instances | update_inventory.sh -i ./staging-inventory -b"
}

# Default values
INVENTORY_FILE="./staging-inventory"
OUTPUT_FILE=""
CREATE_BACKUP=false

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

# Prepare output file argument
OUTPUT_ARG=""
if [ -n "$OUTPUT_FILE" ]; then
  OUTPUT_ARG="--output $OUTPUT_FILE"
fi

# Check if we have python3 installed
if ! command -v python3 &> /dev/null; then
  echo "Error: python3 is required but not found"
  exit 1
fi

# Save the update_inventory.py script to a temporary file
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/update_inventory.py"

# Check if stdin has data (piped input)
if [ -t 0 ]; then
  # No piped input, try to run list_running_instances
  if command -v list_running_instances &> /dev/null; then
    echo "No piped input detected. Running list_running_instances..."
    list_running_instances | python3 "$SCRIPT_PATH" --inventory "$INVENTORY_FILE" $OUTPUT_ARG
  else
    echo "Error: No piped input and list_running_instances function not found"
    echo "Please pipe AWS instance data to this script or ensure list_running_instances is available"
    exit 1
  fi
else
  # Process piped input
  python3 "$SCRIPT_PATH" --inventory "$INVENTORY_FILE" $OUTPUT_ARG
fi

exit $?
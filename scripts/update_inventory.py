#!/usr/bin/env python3
import json
import re
import sys
import argparse

def update_inventory(instances_data, inventory_path, output_path=None):
    """
    Update instance IDs in the inventory file based on AWS instance data.
    
    Parameters:
    instances_data (str): JSON string containing AWS instance information
    inventory_path (str): Path to the inventory file to update
    output_path (str, optional): Path to write the updated inventory. If None, overwrites input file.
    """
    # Read input inventory
    try:
        with open(inventory_path, 'r') as file:
            inventory_content = file.read()
    except FileNotFoundError:
        print(f"Error: Inventory file not found at {inventory_path}")
        return False
    
    # Parse instance data
    try:
        instances = json.loads(instances_data)
        
        # Create a dictionary mapping server names to instance IDs
        instance_map = {}
        for instance in instances:
            # Handle both nested and flat JSON structures
            if isinstance(instance, list):
                instance = instance[0]
            
            name = instance.get("Name")
            instance_id = instance.get("InstanceId")
            
            if not name or not instance_id:
                continue
                
            # Extract the server name (DC01, DC02, etc.) from the full name
            if "dreadgoad-" in name:
                server_name = name.split("dreadgoad-")[-1].lower()
                instance_map[server_name] = instance_id
        
        if not instance_map:
            print("No matching instances found in the AWS data")
            return False
            
        # Update inventory file
        updated_content = inventory_content
        updates_made = 0
        
        for server_name, instance_id in instance_map.items():
            # Pattern to match server configuration lines
            pattern = rf'({server_name} ansible_host=)([^ ]+)( .*)'
            
            # Replace the instance ID in the inventory
            new_content = re.sub(pattern, f'\\1{instance_id}\\3', updated_content, flags=re.IGNORECASE)
            
            if new_content != updated_content:
                updates_made += 1
                updated_content = new_content
                print(f"Updated {server_name} with instance ID: {instance_id}")
        
        if updates_made == 0:
            print("No updates were made. Check server names in AWS output match inventory.")
            return False
        
        # Determine where to write the updated content
        write_path = output_path if output_path else inventory_path
        
        # Write updated inventory back to file
        with open(write_path, 'w') as file:
            file.write(updated_content)
        
        print(f"Successfully updated {updates_made} instance IDs in {write_path}")
        return True
        
    except json.JSONDecodeError:
        print("Error: Invalid JSON format in AWS instance data")
        return False
    except Exception as e:
        print(f"Error: {str(e)}")
        return False

def main():
    parser = argparse.ArgumentParser(description='Update inventory file with AWS instance IDs')
    parser.add_argument('-i', '--inventory', required=True, help='Path to the inventory file to update')
    parser.add_argument('-o', '--output', help='Path for the output file (defaults to overwriting input file)')
    parser.add_argument('-j', '--json', help='JSON file containing AWS instance data (if not provided, reads from stdin)')
    
    args = parser.parse_args()
    
    # Read AWS instance data from file or stdin
    if args.json:
        try:
            with open(args.json, 'r') as f:
                instances_data = f.read()
        except:
            print(f"Error reading AWS data from {args.json}")
            return 1
    else:
        # Read from stdin (for piping)
        instances_data = sys.stdin.read()
    
    if not instances_data.strip():
        print("Error: No AWS instance data provided")
        return 1
    
    success = update_inventory(instances_data, args.inventory, args.output)
    return 0 if success else 1

if __name__ == "__main__":
    sys.exit(main())
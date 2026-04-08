<!-- DOCSIBLE START -->
# network_discovery

## Description

Discover network adapters, IPs, and AWS instance mappings on lab hosts

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `skip_ip_detection` | bool | `False` | No description |
| `skip_adapter_detection` | bool | `False` | No description |

## Tasks

### adapters.yml

- **Get adapter names** (ansible.windows.win_powershell)
- **Set adapter facts from adapter detection** (ansible.builtin.set_fact) - Conditional

### aws_mapping.yml

- **Load AWS instance_to_ip mapping from file for SSM connections** (ansible.builtin.include_vars) - Conditional
- **Display AWS instance to IP mappings** (ansible.builtin.debug) - Conditional
- **Set instance_to_ip mapping for all hosts** (ansible.builtin.set_fact) - Conditional
- **Set vpc_dns_resolver for all hosts** (ansible.builtin.set_fact) - Conditional
- **Set host_ipv4 from AWS instance mapping for SSM connections** (ansible.builtin.set_fact) - Conditional
- **Display host IP assignment from AWS mapping** (ansible.builtin.debug) - Conditional

### dc_facts.yml

- **Store IP as host fact for DCs** (ansible.builtin.set_fact) - Conditional

### fallbacks.yml

- **Set fallback network facts** (ansible.builtin.set_fact)
- **Show host network configuration** (ansible.builtin.debug)

### ip_detection.yml

- **Get all network information** (ansible.windows.win_powershell)
- **Set all network facts from full detection** (ansible.builtin.set_fact) - Conditional

### main.yml

- **Include AWS instance mapping tasks** (ansible.builtin.include_tasks)
- **Check if network facts are already cached** (ansible.builtin.set_fact)
- **Include adapter detection tasks** (ansible.builtin.include_tasks) - Conditional
- **Include IP detection tasks** (ansible.builtin.include_tasks) - Conditional
- **Include fallback facts** (ansible.builtin.include_tasks)
- **Include DC fact storage** (ansible.builtin.include_tasks)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - network_discovery
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: MIT

## Platforms

- Windows: all
<!-- DOCSIBLE END -->

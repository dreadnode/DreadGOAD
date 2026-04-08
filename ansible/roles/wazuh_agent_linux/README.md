<!-- DOCSIBLE START -->
# wazuh_agent_linux

## Description

Install Wazuh agent on Linux hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add Wazuh GPG key** (ansible.builtin.rpm_key) - Conditional
- **Add Wazuh APT key** (ansible.builtin.apt_key) - Conditional
- **Add Wazuh repository (Debian/Ubuntu)** (ansible.builtin.apt_repository) - Conditional
- **Add Wazuh repository (RHEL/CentOS)** (ansible.builtin.yum_repository) - Conditional
- **Install Wazuh agent** (ansible.builtin.package)
- **Configure Wazuh agent manager address** (ansible.builtin.lineinfile)
- **Enable and start Wazuh agent** (ansible.builtin.systemd)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - wazuh_agent_linux
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: MIT

## Platforms

<!-- DOCSIBLE END -->

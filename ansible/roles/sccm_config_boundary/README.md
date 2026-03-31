<!-- DOCSIBLE START -->
# sccm_config_boundary

## Description

Configure SCCM site boundaries and boundary groups

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create boundary** (dreadnode.goad.sccm_boundary)
- **Create boundary group** (dreadnode.goad.sccm_boundary_group)
- **Add boundary to boundary group** (dreadnode.goad.sccm_boundary_to_boundarygroup)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_config_boundary
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->

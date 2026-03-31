<!-- DOCSIBLE START -->
# elk

## Description

elk

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `elasticsearch_version` | str | `7.x` | No description |
| `es_cluster_name` | str | `elasticsearch` | No description |

## Tasks

### main.yml

- **Update cache** (ansible.builtin.apt)
- **Add required dependencies.** (ansible.builtin.apt)
- **Add Elasticsearch apt key.** (ansible.builtin.apt_key)
- **Add Elasticsearch repository.** (ansible.builtin.apt_repository)
- **Install logstash** (ansible.builtin.apt)
- **Install java** (ansible.builtin.apt)
- **Install elasticsearch** (ansible.builtin.apt)
- **Install kibana** (ansible.builtin.apt)
- **Copy kibana config** (ansible.builtin.copy)
- **Elasticsearch change start timeout to 3min** (ansible.builtin.lineinfile)
- **Copy elasticsearch config** (ansible.builtin.copy)
- **Enable logstash** (ansible.builtin.service)
- **Enable elasticsearch** (ansible.builtin.service)
- **Enable kibana** (ansible.builtin.service)
- **Start logstash** (ansible.builtin.service)
- **Start elasticsearch** (ansible.builtin.service)
- **Start kibana** (ansible.builtin.service)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - elk
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->

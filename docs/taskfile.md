# 🛠️ Taskfile for DreadGOAD

This repository contains a Taskfile for managing DreadGOAD provisioning logic
using Ansible and AWS Systems Manager (SSM). It provides a set of automated
tasks for deploying, configuring, and managing AD environments efficiently.

## 📋 Prerequisites

Before using these tasks, ensure that the following dependencies are installed
and properly configured:

- [AWS CLI](https://aws.amazon.com/cli/) installed and configured
- [jq](https://stedolan.github.io/jq/) installed (for JSON processing)
- [Task](https://taskfile.dev) installed (`brew install go-task/tap/go-task` or equivalent)

## 🎯 Available Tasks

### `default`

Displays a list of available tasks.

```bash
task
```

### `list-plays`

Lists all available Ansible playbooks that can be executed.

```bash
task list-plays
```

### `provision`

Runs the complete AD provisioning process using Ansible playbooks.

```bash
task provision ENV=prod VERBOSE=true
```

Optional variables:

- `ENV`: Environment to target (default: `dev`)
- `VERBOSE`: Enables verbose output (`true` or `false`, default: `false`)
- `MAX_RETRIES`: Maximum retry attempts for failed playbooks (default: `3`)
- `RETRY_DELAY`: Delay in seconds between retries (default: `30`)
- `PLAYS`: List of specific playbooks to execute (default: all playbooks)

Example usage:

```bash
task provision PLAYS="build.yml ad-servers.yml" ENV=prod
```

### `get-files`

Displays content of files related to a specific playbook.

```bash
task get-files PLAYBOOK=security
```

Required variable:

- `PLAYBOOK`: Name of the playbook whose files should be displayed.

### `update-inventory`

Synchronizes the Ansible inventory with AWS instance IDs.

```bash
task update-inventory ENV=prod BACKUP=true --force
```

Optional variables:

- `ENV`: Target environment (default: `dev`)
- `INVENTORY`: Path to inventory file (default: `./<env>-inventory`)
- `OUTPUT`: Output file path (if different from inventory file)
- `BACKUP`: Create a backup before modifying the inventory (default: `false`)
- `JSON`: Path to a JSON file with AWS instance data
- `--force`: Force update inventory without confirmation

## 🔧 Extending Tasks

You can extend these tasks by importing this Taskfile into your own Taskfile
and adding custom logic:

```yaml
version: "3"

includes:
  ad:
    taskfile: ./Taskfile.yaml
    optional: true

tasks:
  provision-extended:
    deps: [ad:provision]
    cmds:
      - echo "Additional post-provisioning steps..."
```

## 🔍 Important Notes

- Ensure that AWS CLI is properly configured (`aws configure`).
- Ansible inventory should be kept up to date with the `update-inventory` task.
- The `provision` task implements error handling and retries for robustness.
- Be cautious with deletion tasks, as they can remove AWS resources permanently.
- Use the `AWS_PROFILE` environment variable if working with multiple AWS accounts.
